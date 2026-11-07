"""Load the real shared library with a fake CPA host; never contact upstream."""
import base64
import ctypes as c
import json
import pathlib
import sys
import tempfile

class Buffer(c.Structure):
    _fields_ = [("ptr", c.c_void_p), ("len", c.c_size_t)]

HOST_CALL = c.CFUNCTYPE(c.c_int, c.c_void_p, c.c_char_p, c.c_void_p, c.c_size_t, c.POINTER(Buffer))
FREE = c.CFUNCTYPE(None, c.c_void_p, c.c_size_t)
CALL = c.CFUNCTYPE(c.c_int, c.c_char_p, c.c_void_p, c.c_size_t, c.POINTER(Buffer))
STOP = c.CFUNCTYPE(None)

class Host(c.Structure):
    _fields_ = [("version", c.c_uint32), ("ctx", c.c_void_p), ("call", HOST_CALL), ("free", FREE)]

class Plugin(c.Structure):
    _fields_ = [("version", c.c_uint32), ("call", CALL), ("free", FREE), ("stop", STOP)]

buffers = {}
@HOST_CALL
def host_call(ctx, method, raw, length, response):
    assert method == b"host.auth.list", method
    buf = c.create_string_buffer(b'{"ok":true,"result":{"files":[]}}')
    buffers[c.addressof(buf)] = buf
    response.contents.ptr = c.addressof(buf)
    response.contents.len = len(buf.value)
    return 0

@FREE
def free(ptr, length):
    buffers.pop(ptr, None)

lib = c.CDLL(str(pathlib.Path(sys.argv[1]).resolve()))
host, plugin = Host(1, None, host_call, free), Plugin()
lib.cliproxy_plugin_init.argtypes = [c.POINTER(Host), c.POINTER(Plugin)]
assert lib.cliproxy_plugin_init(c.byref(host), c.byref(plugin)) == 0
assert plugin.version == 1

def call(method, payload):
    raw = json.dumps(payload).encode()
    buf = c.create_string_buffer(raw)
    out = Buffer()
    result = plugin.call(method.encode(), buf, len(raw), c.byref(out))
    try:
        reply = json.loads(c.string_at(out.ptr, out.len))
    finally:
        plugin.free(out.ptr, out.len)
    assert result == 0 and reply["ok"], reply
    return reply["result"]

def manage(method, path, data=None):
    request = {"Method": method, "Path": "/cpa-codex-reset-manager" + path}
    if data is not None:
        request["Body"] = base64.b64encode(json.dumps(data).encode()).decode()
    response = call("management.handle", request)
    assert response["StatusCode"] == 200, response
    return json.loads(base64.b64decode(response["Body"]))

with tempfile.TemporaryDirectory() as tmp:
    config = {"config_yaml": base64.b64encode(("data_dir: " + tmp).encode()).decode()}
    try:
        registration = call("plugin.register", config)
        assert registration["metadata"]["Name"] == "Codex 额度管家"
        routes = call("management.register", {})
        assert len(routes["routes"]) == 4
        settings = manage("GET", "/status")["settings"]
        settings["accounts"] = {"account:test": {"monitor": True, "expiring": False, "exhausted": False}}
        manage("PUT", "/settings", settings)
        call("plugin.reconfigure", config)
        saved = manage("GET", "/status")["settings"]
        assert saved["accounts"] == settings["accounts"] and saved["revision"] == 1
        call("plugin.quiesce", {})
        call("plugin.register", config)
        assert manage("GET", "/status")["settings"] == saved
        print("ABI smoke passed: register, routes, save, reconfigure, quiesce, restart")
    finally:
        plugin.stop()
