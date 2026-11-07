package main

import (
	"testing"
	"time"
)

func TestHistoryPersistsAndMigratesLastReadings(t *testing.T) {
	s, b, now := fixture(t)
	r := Runtime{Previous: b.reading, Current: b.reading}
	r.Previous.At = now.Add(-5 * time.Minute)
	s.runtime.Accounts["a"] = r
	if err := s.saveRuntime(); err != nil {
		t.Fatal(err)
	}
	reopened, err := openStore(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.runtime.Accounts["a"].History) != 2 {
		t.Fatal("last readings not migrated")
	}
	r = reopened.runtime.Accounts["a"]
	appendHistory(&r, r.Current)
	if len(r.History) != 2 {
		t.Fatal("duplicate sample appended")
	}
}
func TestRateUsesContinuousHistoryAndStopsAtResets(t *testing.T) {
	now := time.Now()
	reset := now.Add(24 * time.Hour)
	r := Runtime{Current: Reading{At: now, Windows: []Window{{"primary", 50, 604800, reset}}}}
	for i := 0; i <= 12; i++ {
		appendHistory(&r, Reading{At: now.Add(time.Duration(i-12) * 5 * time.Minute), Windows: []Window{{"primary", float64(38 + i), 604800, reset}}})
	}
	rate, ok := recentRate(r, r.Current.Windows[0], defaults())
	if !ok || rate != 12 {
		t.Fatal(rate, ok)
	}
	r.Current.Windows[0].Used = 1
	r.History[len(r.History)-1].Windows[0].Used = 1
	if _, ok = recentRate(r, r.Current.Windows[0], defaults()); ok {
		t.Fatal("predicted across an early reset")
	}
}
func TestHistoryRetentionAndForecastStaleness(t *testing.T) {
	now := time.Now()
	r := Runtime{}
	for i := 0; i < historyLimit+10; i++ {
		appendHistory(&r, Reading{At: now.Add(time.Duration(i-historyLimit) * time.Minute), Windows: []Window{{"primary", 50, 604800, now.Add(time.Hour)}}})
	}
	if len(r.History) != historyLimit {
		t.Fatal(len(r.History))
	}
	r.Current = Reading{At: now.Add(-time.Hour), Windows: []Window{{"primary", 50, 604800, now.Add(time.Hour)}}}
	f := forecasts("a", r, Policy{Monitor: true}, defaults(), now)
	if len(f) != 1 || f[0].Known || f[0].ExhaustAt != nil {
		t.Fatal("stale prediction", f)
	}
}
