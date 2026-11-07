package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

//go:embed web/index.html
var dashboard []byte

//go:embed logo.png
var logo []byte

//go:embed web/app.js
var dashboardJS []byte

//go:embed web/style.css
var dashboardCSS []byte

//go:embed web/workspace.js
var workspaceJS []byte

type managementRequest struct {
	Method string     `json:"Method"`
	Path   string     `json:"Path"`
	Body   []byte     `json:"Body"`
	Query  url.Values `json:"Query"`
}
type managementResponse struct {
	StatusCode int         `json:"StatusCode"`
	Headers    http.Header `json:"Headers"`
	Body       []byte      `json:"Body"`
}
type service struct {
	lifecycle  sync.Mutex
	mu         sync.Mutex
	store      *Store
	cancel     chan struct{}
	done       chan struct{}
	wake       chan struct{}
	notifyWake chan struct{}
	health     string
	fault      bool
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
	a.notifyWake = make(chan struct{}, 1)
	stop, done, wake := a.cancel, a.done, a.wake
	notifyWake := a.notifyWake
	a.mu.Unlock()
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
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
	go func() {
		defer workers.Done()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() {
			select {
			case <-stop:
				cancel()
			case <-ctx.Done():
			}
		}()
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			deliverNotifications(ctx, s, sendNotification)
			select {
			case <-stop:
				return
			case <-ticker.C:
			case <-notifyWake:
			}
		}
	}()
	go func() { workers.Wait(); close(done) }()
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
		if account.Disabled || account.UpstreamID == "" || seen[account.Key] {
			continue
		}
		seen[account.Key] = true
		s.mu.Lock()
		p := s.settings.Accounts[account.Key]
		if !p.Monitor {
			s.mu.Unlock()
			continue
		}
		runtime := s.runtime.Accounts[account.Key]
		runtime.Label = account.Email
		if runtime.Label == "" {
			runtime.Label = account.Name
		}
		s.runtime.Accounts[account.Key] = runtime
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
			"metadata":       map[string]any{"Name": "Codex 额度管家", "Version": pluginVersion, "Author": "Autsunset", "GitHubRepository": "https://github.com/Autsunset/" + pluginID, "Logo": "https://raw.githubusercontent.com/Autsunset/" + pluginID + "/main/logo.png", "ConfigFields": []any{map[string]any{"Name": "data_dir", "Type": "string", "Description": "Absolute persistent directory; default /CLIProxyAPI/data/cpa-codex-reset-manager"}}},
			"capabilities":   map[string]bool{"management_api": true},
		})
	case "plugin.quiesce":
		global.stop()
		return okEnvelope(struct{}{})
	case "management.register":
		routes := []any{}
		for _, r := range [][2]string{{"GET", "/status"}, {"GET", "/history"}, {"GET", "/notifications"}, {"PUT", "/notifications"}, {"PUT", "/settings"}, {"POST", "/check"}, {"POST", "/resolve"}} {
			routes = append(routes, map[string]string{"Method": r[0], "Path": "/" + pluginID + r[1]})
		}
		return okEnvelope(map[string]any{"routes": routes, "resources": []any{map[string]string{"Path": "/dashboard", "Menu": "Codex 额度管家", "Description": "额度监控、用量提醒与持久重置策略"}, map[string]string{"Path": "/logo.png"}, map[string]string{"Path": "/app.js"}, map[string]string{"Path": "/workspace.js"}, map[string]string{"Path": "/style.css"}}})
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
		if req.Method == "GET" {
			for path, asset := range map[string]struct {
				contentType string
				body        []byte
			}{
				"/app.js":       {"text/javascript; charset=utf-8", dashboardJS},
				"/style.css":    {"text/css; charset=utf-8", dashboardCSS},
				"/workspace.js": {"text/javascript; charset=utf-8", workspaceJS},
			} {
				if req.Path == "/v0/resource/plugins/"+pluginID+path {
					return managementResponse{200, http.Header{"Content-Type": {asset.contentType}, "Cache-Control": {"no-store"}, "X-Content-Type-Options": {"nosniff"}}, asset.body}
				}
			}
		}
		if req.Method == "GET" && req.Path == "/v0/resource/plugins/"+pluginID+"/logo.png" {
			return managementResponse{200, http.Header{"Content-Type": {"image/png"}, "Cache-Control": {"no-cache"}, "X-Content-Type-Options": {"nosniff"}}, logo}
		}
		if req.Method == "GET" && req.Path == "/v0/resource/plugins/"+pluginID+"/dashboard" {
			return managementResponse{200, http.Header{"Content-Type": {"text/html; charset=utf-8"}, "Cache-Control": {"no-store"}, "Content-Security-Policy": {"default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; connect-src 'self'; object-src 'none'; base-uri 'none'"}}, dashboard}
		}
		return response(404, map[string]string{"error": "not found"})
	}
	path := strings.TrimPrefix(req.Path, "/v0/management")
	path = strings.TrimPrefix(path, "/"+pluginID)
	a.mu.Lock()
	s, wake, health, notifyWake := a.store, a.wake, a.health, a.notifyWake
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
	case req.Method == "GET" && path == "/notifications":
		return response(200, s.deliveryView())
	case req.Method == "PUT" && path == "/notifications":
		if err := s.saveDelivery(req.Body); err != nil {
			return response(400, map[string]string{"error": err.Error()})
		}
		select {
		case notifyWake <- struct{}{}:
		default:
		}
		return response(200, s.deliveryView())
	case req.Method == "GET" && path == "/history":
		id := req.Query.Get("account")
		r, ok := s.runtime.Accounts[id]
		if !ok {
			return response(404, map[string]string{"error": "account not found"})
		}
		hours, _ := strconv.Atoi(req.Query.Get("hours"))
		if hours != 6 && hours != 24 && hours != 168 && hours != 336 {
			hours = 24
		}
		start := time.Now().Add(-time.Duration(hours) * time.Hour)
		points := []HistorySample{}
		for _, p := range samples(r) {
			if !p.At.Before(start) {
				points = append(points, p)
			}
		}
		return response(200, map[string]any{"account": id, "points": points, "forecasts": forecasts(id, r, s.settings.Accounts[id], s.settings, time.Now()), "server_time": time.Now()})
	case req.Method == "GET" && path == "/status":
		if listErr != nil {
			health = listErr.Error()
		}
		all := []Notice{}
		for id, p := range s.settings.Accounts {
			all = append(all, notices(id, s.runtime.Accounts[id], p, s.settings, time.Now())...)
		}
		summary := RuntimeState{Accounts: map[string]Runtime{}, Events: s.runtime.Events}
		predictions := map[string][]Forecast{}
		for id, r := range s.runtime.Accounts {
			predictions[id] = forecasts(id, r, s.settings.Accounts[id], s.settings, time.Now())
			r.History = nil
			summary.Accounts[id] = r
		}
		return response(200, map[string]any{"version": pluginVersion, "settings": s.settings, "runtime": summary, "forecasts": predictions, "accounts": accounts, "notices": all, "health": health, "data_dir": s.dir, "server_time": time.Now()})
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
