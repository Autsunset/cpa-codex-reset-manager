package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fakeBackend struct {
	reading       Reading
	err           error
	consumeErr    error
	calls         []Attempt
	beforeConsume func()
}

func (b *fakeBackend) Read(CodexCredentials) (Reading, error) {
	r := b.reading
	r.Windows = append([]Window(nil), r.Windows...)
	r.Credits = append([]Credit(nil), r.Credits...)
	return r, b.err
}
func (b *fakeBackend) Consume(_ CodexCredentials, id, credit string) (ConsumeResponse, error) {
	if b.beforeConsume != nil {
		b.beforeConsume()
	}
	b.calls = append(b.calls, Attempt{RequestID: id, CreditID: credit})
	return ConsumeResponse{Code: ConsumeCodeReset}, b.consumeErr
}
func fixture(t *testing.T) (*Store, *fakeBackend, time.Time) {
	t.Helper()
	s, err := openStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	b := &fakeBackend{reading: Reading{
		At: now, Count: 2, Credits: []Credit{{ID: "credit-1", Status: "available", ExpiresAt: now.Add(10 * time.Minute)}, {ID: "credit-2", Status: "available", ExpiresAt: now.Add(24 * time.Hour)}},
		Windows: []Window{{Name: "primary", Used: 100, Seconds: 604800, ResetAt: now.Add(3 * 24 * time.Hour)}},
	}}
	return s, b, now
}
func TestSettingsSurviveReloadAndAccountDisappearance(t *testing.T) {
	s, _, _ := fixture(t)
	next := s.settings
	next.Accounts["account:a"] = Policy{true, true, true}
	if err := s.saveSettings(next); err != nil {
		t.Fatal(err)
	}
	s.runtime.Accounts = map[string]Runtime{}
	if err := s.saveRuntime(); err != nil {
		t.Fatal(err)
	}
	reopened, err := openStore(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.settings.Accounts["account:a"] != (Policy{true, true, true}) {
		t.Fatal("lost consent")
	}
	disabled := reopened.settings
	disabled.Accounts = map[string]Policy{}
	if err := reopened.saveSettings(disabled); err != nil {
		t.Fatal(err)
	}
	again, err := openStore(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.settings.Accounts) != 0 {
		t.Fatal("explicit disable lost")
	}
}
func TestStaleSettingsCannotOverwrite(t *testing.T) {
	s, _, _ := fixture(t)
	old := s.settings
	if err := s.saveSettings(old); err != nil {
		t.Fatal(err)
	}
	if err := s.saveSettings(old); err == nil {
		t.Fatal("stale write accepted")
	}
}
func TestCorruptFilesFailClosed(t *testing.T) {
	for _, name := range []string{"settings.json", "runtime.json"} {
		t.Run(name, func(t *testing.T) {
			s, _, _ := fixture(t)
			path := filepath.Join(s.dir, name)
			os.WriteFile(path, []byte("{broken"), 0600)
			if _, err := openStore(s.dir); err == nil {
				t.Fatal("corruption silently reset")
			}
			b, _ := os.ReadFile(path)
			if string(b) != "{broken" {
				t.Fatal("corruption destroyed")
			}
		})
	}
}

func TestEmptyObjectAndNullStateFailClosed(t *testing.T) {
	for _, name := range []string{"settings.json", "runtime.json"} {
		for _, raw := range []string{"null", "{}"} {
			s, _, _ := fixture(t)
			if err := os.WriteFile(filepath.Join(s.dir, name), []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := openStore(s.dir); err == nil {
				t.Fatalf("accepted %s in %s", raw, name)
			}
		}
	}
}

func TestMissingJournalNeverLooksLikeFreshInstall(t *testing.T) {
	for _, name := range []string{"settings.json", "runtime.json"} {
		s, _, _ := fixture(t)
		if err := os.Remove(filepath.Join(s.dir, name)); err != nil {
			t.Fatal(err)
		}
		if _, err := openStore(s.dir); err == nil {
			t.Fatal("accepted incomplete state")
		}
	}
}
func TestFailedSettingsSaveKeepsMemory(t *testing.T) {
	s, _, _ := fixture(t)
	s.dir = filepath.Join(s.dir, "missing")
	next := s.settings
	next.PollMinutes = 3
	if err := s.saveSettings(next); err == nil {
		t.Fatal("expected write failure")
	}
	if s.settings.PollMinutes != 5 || s.settings.Revision != 0 {
		t.Fatal("unsaved settings applied")
	}
}
func TestIntentDurableBeforeConsumeAndRetryAcrossRestart(t *testing.T) {
	s, b, now := fixture(t)
	b.consumeErr = errors.New("timeout after upstream accepted")
	b.beforeConsume = func() {
		reloaded, err := openStore(s.dir)
		if err != nil {
			t.Fatal(err)
		}
		a := reloaded.runtime.Accounts["a"].Attempt
		if a == nil || a.RequestID == "" || a.CreditID != "credit-1" {
			t.Fatal("consume before durable intent")
		}
	}
	p := Policy{true, true, false}
	if err := process(s, "a", p, CodexCredentials{}, b, now); err != nil {
		t.Fatal(err)
	}
	reloaded, err := openStore(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := process(reloaded, "a", p, CodexCredentials{}, b, now.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if len(b.calls) != 2 || b.calls[0].RequestID != b.calls[1].RequestID || b.calls[0].CreditID != b.calls[1].CreditID {
		t.Fatal("retry identity changed")
	}
}
func TestPersistenceFailurePreventsRedemption(t *testing.T) {
	s, b, now := fixture(t)
	s.dir = filepath.Join(s.dir, "missing")
	if err := process(s, "a", Policy{true, true, true}, CodexCredentials{}, b, now); err == nil {
		t.Fatal("expected journal failure")
	}
	if len(b.calls) != 0 {
		t.Fatal("redeemed without durable intent")
	}
}
func TestVerifyNoSecondConsumeAndWeeklyLock(t *testing.T) {
	s, b, now := fixture(t)
	if err := process(s, "a", Policy{true, false, true}, CodexCredentials{}, b, now); err != nil {
		t.Fatal(err)
	}
	b.reading.Count = 1
	b.reading.Credits = b.reading.Credits[1:]
	b.reading.Windows[0].Used = 0
	if err := process(s, "a", Policy{true, false, true}, CodexCredentials{}, b, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	r := s.runtime.Accounts["a"]
	if r.Attempt != nil || !r.WeekLock.After(now) {
		t.Fatal("verification/lock missing")
	}
	b.reading.Windows[0].Used = 100
	if err := process(s, "a", Policy{true, false, true}, CodexCredentials{}, b, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if len(b.calls) != 1 {
		t.Fatal("spent twice within weekly lock")
	}
}
func TestUncertainResultBlocksNewCredit(t *testing.T) {
	s, b, now := fixture(t)
	process(s, "a", Policy{true, true, true}, CodexCredentials{}, b, now)
	b.reading.Credits = b.reading.Credits[1:] // Count/quota did not verify.
	process(s, "a", Policy{true, true, true}, CodexCredentials{}, b, now.Add(time.Hour))
	if len(b.calls) != 1 || s.runtime.Accounts["a"].Attempt.Status != "blocked" {
		t.Fatal("uncertain outcome not blocked")
	}
}
func TestDisabledPolicyNeverConsumes(t *testing.T) {
	s, b, now := fixture(t)
	process(s, "a", Policy{}, CodexCredentials{}, b, now)
	if len(b.calls) != 0 {
		t.Fatal("disabled redemption")
	}
}
func TestFiveHourExhaustionDoesNotUseWeeklyPolicy(t *testing.T) {
	s, b, now := fixture(t)
	b.reading.Windows[0].Seconds = 18000
	process(s, "a", Policy{true, false, true}, CodexCredentials{}, b, now)
	if len(b.calls) != 0 {
		t.Fatal("five hour triggered weekly redemption")
	}
}
func TestNoticesSuppressAccelerationWhenAnotherWindowConstrained(t *testing.T) {
	s, b, now := fixture(t)
	w := Window{"primary", 50, 604800, now.Add(2 * time.Hour)}
	short := Window{"secondary", 99, 18000, now.Add(time.Hour)}
	r := Runtime{Current: Reading{At: now, Windows: []Window{w, short}}, Previous: Reading{At: now.Add(-5 * time.Minute), Windows: []Window{w, short}}}
	ns := notices("a", r, Policy{Monitor: true}, s.settings, now)
	for _, n := range ns {
		if n.Kind == "speed_up" {
			t.Fatal("conflicting acceleration notice")
		}
	}
	r.Current = b.reading
	r.Current.At = now.Add(-time.Hour)
	if got := notices("a", r, Policy{Monitor: true}, s.settings, now); len(got) != 1 || got[0].Kind != "stale" {
		t.Fatal("stale prediction")
	}
}
func TestSpeedUpAndNaturalResetBoundary(t *testing.T) {
	s, _, now := fixture(t)
	w := Window{"primary", 30, 604800, now.Add(2 * time.Hour)}
	r := Runtime{Current: Reading{At: now, Windows: []Window{w}}, Previous: Reading{At: now.Add(-5 * time.Minute), Windows: []Window{w}}}
	ns := notices("a", r, Policy{Monitor: true}, s.settings, now)
	if ns[0].Kind != "speed_up" {
		t.Fatal(ns)
	}
	r.Previous.Windows[0].ResetAt = now.Add(time.Hour)
	if got := notices("a", r, Policy{Monitor: true}, s.settings, now); got[0].Kind != "learning" {
		t.Fatal("used cross-cycle speed")
	}
}
func TestParseWindowsRejectsUnknownAndIgnoresReviewPool(t *testing.T) {
	for _, raw := range []string{`{}`, `{"rate_limit":{"primary_window":{"used_percent":2}}}`, `{"code_review_rate_limit":{"primary_window":{"used_percent":100,"limit_window_seconds":604800,"reset_at":1800000000}}}`} {
		if _, err := parseWindows([]byte(raw)); err == nil {
			t.Fatal("accepted missing main windows")
		}
	}
	w, err := parseWindows([]byte(`{"rate_limit":{"primary_window":{"used_percent":2,"limit_window_seconds":604800,"reset_at":1800000000}}}`))
	if err != nil || len(w) != 1 || w[0].Seconds != 604800 {
		t.Fatal(w, err)
	}
}
func TestPublicResourceNeverLeaksStateOrAcceptsWrites(t *testing.T) {
	s, _, _ := fixture(t)
	s.settings.Accounts["secret-account"] = Policy{Monitor: true}
	a := &service{store: s, wake: make(chan struct{}, 1)}
	r := a.handle(managementRequest{Method: "GET", Path: "/v0/resource/plugins/" + pluginID + "/dashboard"})
	if r.StatusCode != 200 {
		t.Fatal(r.StatusCode)
	}
	var v any
	if json.Unmarshal(r.Body, &v) == nil {
		t.Fatal("resource served JSON")
	}
	r = a.handle(managementRequest{Method: "PUT", Path: "/v0/resource/plugins/" + pluginID + "/settings", Body: []byte(`{}`)})
	if r.StatusCode != 404 {
		t.Fatal("resource mutation allowed")
	}
}
