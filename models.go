package main

import (
	"sort"
	"time"
)

// Credit models one reset-credit row from GET /rate-limit-reset-credits
// (spec §2.1). A zero ExpiresAt means "never expires".
type Credit struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Snapshot is a (credits, weekly %) pair captured at key moments during a
// reset cycle: pre-reset confirmation, and post-reset verification.
type Snapshot struct {
	AvailableCount int       `json:"available_count"`
	Credits        []Credit  `json:"credits"`
	WeeklyPct      int       `json:"weekly_pct"`
	CapturedAt     time.Time `json:"captured_at"`
}

// availableCreditsSorted implements spec §2.3: filter to status=="available"
// AND expires_at > now (local defensive filter against server-side cleanup
// lag), then sort ascending by ExpiresAt with zero-expiry ("never") entries
// last. This is the production port of the filter+sort that the simulation
// validated in sim/engine.go's FakeOpenAIClient.list plus pickTarget.
func availableCreditsSorted(credits []Credit, now time.Time) []Credit {
	out := make([]Credit, 0, len(credits))
	for _, c := range credits {
		if c.Status != "available" {
			continue
		}
		if !c.ExpiresAt.IsZero() && !c.ExpiresAt.After(now) {
			continue // already expired; local filter
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ExpiresAt.IsZero() {
			return false // never-expiry sorts last
		}
		if out[j].ExpiresAt.IsZero() {
			return true
		}
		return out[i].ExpiresAt.Before(out[j].ExpiresAt)
	})
	return out
}
