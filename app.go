package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

//go:embed web/index.html
var dashboard []byte

type managementRequest struct {
	Method string `json:"Method"`
	Path   string `json:"Path"`
	Body   []byte `json:"Body"`
}
type managementResponse struct {
	StatusCode int         `json:"StatusCode"`
	Headers    http.Header `json:"Headers"`
	Body       []byte      `json:"Body"`
}
type service struct {
	lifecycle sync.Mutex
	mu        sync.Mutex
	store     *Store
	cancel    chan struct{}
	done      chan struct{}
	wake      chan struct{}
	health    string
	fault     bool
}

var global = &service{}

func (a *service) stop() {
	a.lifecycle.Lock()
	defer a.lifecycle.Unlock()
	a.stopWorker()
}
func (a *service) stopWorker() {
	a.mu.Lock()
	cancel, done := a.cancel, a.done
	a.cancel = nil
	a.mu.Unlock()
	if cancel != nil {
		close(cancel)
		<-done
	}
}
func (a *service) configure(raw []byte) error {
	a.lifecycle.Lock()
	defer a.lifecycle.Unlock()
	var req struct {
		ConfigYAML []byte `json:"config_yaml"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return err
	}
	cfg := struct {
		DataDir string `yaml:"data_dir"`
	}{"/CLIProxyAPI/data/" + pluginID}
	if err := yaml.Unmarshal(req.ConfigYAML, &cfg); err != nil {
		return err
	}
	a.mu.Lock()
	old := a.store
	a.mu.Unlock()
	if old != nil && old.dir != cfg.DataDir {
		return errors.New("data_dir cannot change during reload; stop host and migrate files first")
	}
	a.stopWorker()
	s, err := openStore(cfg.DataDir)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.store = s
	a.fault = false
	a.cancel = make(chan struct{})
	a.done = make(chan struct{})
	a.wake = make(chan struct{}, 1)
	stop, done, wake := a.cancel, a.done, a.wake
	a.mu.Unlock()
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			a.poll(s, stop)
			s.mu.Lock()
			delay := time.Duration(s.settings.PollMinutes) * time.Minute
			s.mu.Unlock()
			t := time.NewTimer(delay)
			select {
			case <-stop:
				t.Stop()
				return
			case <-wake:
				t.Stop()
			case <-t.C:
			}
		}
	}()
	return nil
}
func (a *service) poll(s *Store, stop <-chan struct{}) {
	a.mu.Lock()
	fault := a.fault
	a.mu.Unlock()
	if fault {
		return
	}
	accounts, err := listAccounts()
	if err != nil {
		a.mu.Lock()
		a.health = err.Error()
		a.mu.Unlock()
		return
	}
	a.mu.Lock()
	a.health = ""
	a.mu.Unlock()
	seen := map[string]bool{}
	for _, account := range accounts {
		select {
		case <-stop:
			return
		default:
		}
		if account.Disabled || seen[account.Key] {
			continue
		}
		seen[account.Key] = true
		s.mu.Lock()
		p := s.settings.Accounts[account.Key]
		if !p.Monitor {
			s.mu.Unlock()
			continue
		}
		c, err := credentials(account)
		if err == nil {
			pending := s.runtime.Accounts[account.Key].Attempt != nil
			err = process(s, account.Key, p, c, backend{&OpenAIClient{}}, time.Now())
			if err == nil && pending && s.runtime.Accounts[account.Key].Attempt == nil {
				if _, cooldownErr := hostCall("host.routing.reset_cooldown", map[string]any{"auth_index": account.AuthIndex}); cooldownErr != nil {
					s.event(account.Key, "Reset verified; CPA cooldown could not be cleared: "+cooldownErr.Error(), time.Now())
					err = s.saveRuntime()
				}
			}
			if err != nil {
				a.mu.Lock()
				a.fault = true
				a.mu.Unlock()
			}
		}
		if err != nil {
			r := s.runtime.Accounts[account.Key]
			r.Error = err.Error()
			s.runtime.Accounts[account.Key] = r
			// No more irreversible work after a durability failure in this poll.
			a.mu.Lock()
			a.health = err.Error()
			a.mu.Unlock()
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
	}
}
func handleMethod(method string, raw []byte) ([]byte, error) {
	switch method {
	case "plugin.register", "plugin.reconfigure":
		if err := global.configure(raw); err != nil {
			return nil, err
		}
		return okEnvelope(map[string]any{
			"schema_version": 1,
			"metadata":       map[string]any{"Name": "CPA Codex Reset Manager", "Version": pluginVersion, "Author": "Autsunset", "GitHubRepository": "https://github.com/Autsunset/" + pluginID, "ConfigFields": []any{map[string]any{"Name": "data_dir", "Type": "string", "Description": "Absolute persistent directory; default /CLIProxyAPI/data/cpa-codex-reset-manager"}}},
			"capabilities":   map[string]bool{"management_api": true},
		})
	case "plugin.quiesce":
		global.stop()
		return okEnvelope(struct{}{})
	case "management.register":
		routes := []any{}
		for _, r := range [][2]string{{"GET", "/status"}, {"PUT", "/settings"}, {"POST", "/check"}, {"POST", "/resolve"}} {
			routes = append(routes, map[string]string{"Method": r[0], "Path": "/" + pluginID + r[1]})
		}
		return okEnvelope(map[string]any{"routes": routes, "resources": []any{map[string]string{"Path": "/dashboard", "Menu": "Codex 重置与节奏", "Description": "Persistent reset policies and quota pace reminders"}}})
	case "management.handle":
		var req managementRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, err
		}
		return okEnvelope(global.handle(req))
	default:
		return nil, fmt.Errorf("unsupported method %s", method)
	}
}
func response(code int, v any) managementResponse {
	raw, _ := json.Marshal(v)
	return managementResponse{code, http.Header{"Content-Type": {"application/json"}, "Cache-Control": {"no-store"}}, raw}
}
func (a *service) handle(req managementRequest) managementResponse {
	// A settings write must never overlap a reconfigure read/reopen.
	a.lifecycle.Lock()
	defer a.lifecycle.Unlock()
	if strings.HasPrefix(req.Path, "/v0/resource/plugins/") {
		if req.Method == "GET" && req.Path == "/v0/resource/plugins/"+pluginID+"/dashboard" {
			return managementResponse{200, http.Header{"Content-Type": {"text/html; charset=utf-8"}, "Cache-Control": {"no-store"}, "Content-Security-Policy": {"default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; connect-src 'self'; object-src 'none'; base-uri 'none'"}}, dashboard}
		}
		return response(404, map[string]string{"error": "not found"})
	}
	path := strings.TrimPrefix(req.Path, "/v0/management")
	path = strings.TrimPrefix(path, "/"+pluginID)
	a.mu.Lock()
	s, wake, health := a.store, a.wake, a.health
	a.mu.Unlock()
	if s == nil {
		return response(503, map[string]string{"error": "not configured"})
	}
	if req.Method == "POST" && path == "/check" {
		select {
		case wake <- struct{}{}:
		default:
		}
		return response(202, map[string]bool{"queued": true})
	}
	var accounts []Account
	var listErr error
	if req.Method == "GET" && path == "/status" {
		accounts, listErr = listAccounts()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case req.Method == "GET" && path == "/status":
		if listErr != nil {
			health = listErr.Error()
		}
		all := []Notice{}
		for id, p := range s.settings.Accounts {
			all = append(all, notices(id, s.runtime.Accounts[id], p, s.settings, time.Now())...)
		}
		return response(200, map[string]any{"version": pluginVersion, "settings": s.settings, "runtime": s.runtime, "accounts": accounts, "notices": all, "health": health, "data_dir": s.dir, "server_time": time.Now()})
	case req.Method == "PUT" && path == "/settings":
		var v Settings
		if err := json.Unmarshal(req.Body, &v); err != nil {
			return response(400, map[string]string{"error": err.Error()})
		}
		if err := s.saveSettings(v); err != nil {
			return response(409, map[string]string{"error": err.Error()})
		}
		select {
		case wake <- struct{}{}:
		default:
		}
		return response(200, s.settings)
	case req.Method == "POST" && path == "/resolve":
		var v struct {
			Account   string `json:"account"`
			RequestID string `json:"request_id"`
		}
		if err := json.Unmarshal(req.Body, &v); err != nil {
			return response(400, map[string]string{"error": "invalid request"})
		}
		r := s.runtime.Accounts[v.Account]
		if r.Attempt == nil || r.Attempt.RequestID != v.RequestID {
			return response(409, map[string]string{"error": "attempt changed; refresh"})
		}
		// Resolving an ambiguous journal ALWAYS revokes automatic redemption.
		// Explicit settings save is needed to grant it again.
		next := s.settings
		next.Accounts = map[string]Policy{}
		for id, p := range s.settings.Accounts {
			next.Accounts[id] = p
		}
		p := next.Accounts[v.Account]
		p.Expiring = false
		p.Exhausted = false
		next.Accounts[v.Account] = p
		if err := s.saveSettings(next); err != nil {
			return response(500, map[string]string{"error": err.Error()})
		}
		old := r
		r.Attempt = nil
		r.Error = ""
		r.WeekLock = time.Now().Add(7 * 24 * time.Hour)
		s.runtime.Accounts[v.Account] = r
		s.event(v.Account, "Operator acknowledged unresolved attempt; automation disabled", time.Now())
		if err := s.saveRuntime(); err != nil {
			s.runtime.Accounts[v.Account] = old
			return response(500, map[string]string{"error": err.Error()})
		}
		return response(200, map[string]bool{"resolved": true, "automation_disabled": true})
	}
	return response(404, map[string]string{"error": "not found"})
}
