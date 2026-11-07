# Implementation research — 2026-11-07

## Original plugin

Inspected `JefferyZhang2019/cpa-plugin-codex-auto-reset` at
`a26b99522261ed588c053c005e35dde34ee4097f` and tag `v0.1.0`.
The server's installed library is `codex-auto-reset-v0.1.0.so`.

In `v0.1.0`, `defaultStatePath()` returns the relative filename
`codex-auto-reset.state.json`. The inspected container works in `/CLIProxyAPI`,
and the file exists at `/CLIProxyAPI/codex-auto-reset.state.json`. The container
mounts its config file, auth directory, logs, plugins and `/CLIProxyAPI/data`,
but not that state file or the entire working directory.

Therefore **container recreation loses the old plugin's settings**. This is an
established persistence defect for this deployment, not proof that every past
reported disappearance had the same cause. A plain `docker restart` preserves
the writable layer. No historical container event audit was available.

Later upstream code moves state toward an auth-directory-backed path, implements
migrations and backup recovery. Simply saying “upstream never persists” would
be wrong. Its current architecture also combines configuration and runtime state.
This project uses separate files, rejects malformed state and write failures,
and checks browser revisions instead.

Upstream's client provides targeted credit redemption with `credit_id` and
`redeem_request_id`; verification checks the credit, count and quota recovery.
This project reuses the protocol/client implementation with MIT attribution,
but implements its own journal, policy engine, persistence and UI.

## Magpie

Inspected local files:

- `internal/provider/codex_autoreset.go`
- `internal/provider/codex_resets.go`
- `internal/gateway/codex_autoreset.go`

Magpie separates weekly-exhaustion and expiring-credit consent, stores
redemption history, uses atomic durable writes and prevents repeated weekly
redemptions across restarts. Its gateway waits until alternative accounts cannot
serve a request before spending an exhaustion reset.

Adopted separate consent and durable history. This plugin has no equivalent
gateway candidate context, so its explicitly enabled exhaustion policy is
per-account and does not claim Magpie's all-candidates-exhausted behavior.
No Magpie source code was copied.

## CPA Quota Estimator and host ABI

Inspected local `main.go`, `types.go`, `app.go`, packaging and Release workflow.
The new plugin follows ABI version 1 registration, authenticated management
routes, static resource separation, and root-library ZIP packaging. It remains
independent of Quota Estimator and does not open its database.

Confirmed host callback JSON against CPA's `sdk/pluginapi/types.go`:
`host.auth.list`, `host.auth.get`, and `host.routing.reset_cooldown`.

## Store

Read the current `router-for-me/CLIProxyAPI-Plugins-Store` README and registry.
Initial registration needs a PR after a real release exists. Subsequent normal
releases are discovered through GitHub Releases. ZIP names use
`<id>_<version>_<goos>_<goarch>.zip`; one correctly named dynamic library is at
each ZIP root. `checksums.txt` uses SHA-256 format.

## Scope and operational limits

- Read-only server investigation; no plugin replacement, restart, or live credit
  redemption was performed.
- Default data path matches the deployment's existing persistent data mount.
- No automatic import of old settings: identity and consent must be reviewed.
- Never enable both plugins' automatic redemption on the same account.
- Browser notifications require an open page, permission and HTTPS/localhost;
  they are not push delivery or email notifications.
- Poll-based rate estimates can be noisy for integer quota observations and
  bursty workloads. Stale, cross-cycle and missing observations are excluded.
