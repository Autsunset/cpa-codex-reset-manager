package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"time"
)

type Backend interface {
	Read(CodexCredentials) (Reading, error)
	Consume(CodexCredentials, string, string) (ConsumeResponse, error)
}
type backend struct{ *OpenAIClient }

func (b backend) Read(c CodexCredentials) (Reading, error) {
	credits, err := b.ListCredits(c)
	if err != nil {
		return Reading{}, err
	}
	req, err := http.NewRequest("GET", b.baseURL()+usageEndpointPath, nil)
	if err != nil {
		return Reading{}, err
	}
	b.setAuthHeaders(req, c)
	raw, err := b.do(req)
	if err != nil {
		return Reading{}, err
	}
	w, err := parseWindows(raw)
	if err != nil {
		return Reading{}, err
	}
	return Reading{At: time.Now(), Windows: w, Credits: credits.Credits, Count: credits.AvailableCount}, nil
}
func parseWindows(raw []byte) ([]Window, error) {
	var v struct {
		RateLimit struct {
			Primary   json.RawMessage `json:"primary_window"`
			Secondary json.RawMessage `json:"secondary_window"`
		} `json:"rate_limit"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	out := []Window{}
	for name, raw := range map[string]json.RawMessage{"primary": v.RateLimit.Primary, "secondary": v.RateLimit.Secondary} {
		if len(raw) == 0 || string(raw) == "null" {
			continue
		}
		var w struct {
			Used    *float64 `json:"used_percent"`
			Seconds int      `json:"limit_window_seconds"`
			Reset   int64    `json:"reset_at"`
		}
		if err := json.Unmarshal(raw, &w); err != nil {
			return nil, err
		}
		if w.Used == nil || *w.Used < 0 || *w.Used > 100 || w.Seconds <= 0 || w.Reset <= 0 {
			return nil, errors.New("incomplete quota window; no reset permitted")
		}
		out = append(out, Window{name, *w.Used, w.Seconds, time.Unix(w.Reset, 0)})
	}
	if len(out) == 0 {
		return nil, errors.New("no main quota windows")
	}
	return out, nil
}

type Notice struct {
	Account    string    `json:"account"`
	Kind       string    `json:"kind"`
	Window     string    `json:"window"`
	Message    string    `json:"message"`
	Remaining  float64   `json:"remaining"`
	Rate       float64   `json:"rate_per_hour"`
	RateKnown  bool      `json:"rate_known"`
	TargetRate float64   `json:"target_per_hour"`
	Deadline   time.Time `json:"deadline"`
}

func notices(id string, r Runtime, p Policy, s Settings, now time.Time) []Notice {
	out := []Notice{}
	if !p.Monitor {
		return out
	}
	if r.Error != "" || r.Current.At.IsZero() || now.Sub(r.Current.At) > time.Duration(s.PollMinutes*3)*time.Minute {
		return []Notice{{Account: id, Kind: "stale", Message: "额度数据过期或尚未采样，暂不判断用量速度 / Stale or missing quota data"}}
	}
	for _, w := range r.Current.Windows {
		if !w.ResetAt.After(now) {
			continue
		}
		deadline := w.ResetAt
		if p.Expiring && len(r.Current.Credits) > 0 {
			c := r.Current.Credits[0]
			d := c.ExpiresAt.Add(-time.Duration(s.LeadMinutes) * time.Minute)
			if !c.ExpiresAt.IsZero() && d.After(now) && d.Before(deadline) {
				deadline = d
			}
		}
		n := Notice{Account: id, Window: w.Name, Remaining: 100 - w.Used, Deadline: deadline}
		hours := deadline.Sub(now).Hours()
		n.TargetRate = n.Remaining / hours
		rate, known := recentRate(r, w, s)
		n.Rate = rate
		n.RateKnown = known
		switch {
		case w.Used >= 99:
			n.Kind = "slow_down"
			n.Message = "额度接近耗尽，请减缓速度 / Quota nearly exhausted"
		case known && n.Rate > n.TargetRate*1.2:
			n.Kind = "slow_down"
			n.Message = "当前速度可能在重置前耗尽额度 / Pace may exhaust quota before reset"
		case known && hours <= float64(s.ReminderHours) && n.Remaining >= 10 && n.Rate < n.TargetRate*.8:
			n.Kind = "speed_up"
			n.Message = "重置临近，按当前速度将留下未用额度，可加快有价值任务 / Reset soon; room to use more quota"
		case !known:
			n.Kind = "learning"
			n.Message = "等待同周期第二个有效样本 / Waiting for a second sample in this window"
		default:
			n.Kind = "balanced"
			n.Message = "当前速度基本合适 / Pace is within budget"
		}
		out = append(out, n)
	}
	// A short window or weekly constraint must override encouragement.
	blocked := false
	for _, n := range out {
		if n.Kind == "slow_down" {
			blocked = true
		}
	}
	if blocked {
		for i := range out {
			if out[i].Kind == "speed_up" {
				out[i].Kind = "constrained"
				out[i].Message = "其他额度窗口受限，请优先减速 / Another window requires slowing down"
			}
		}
	}
	return out
}

func weekly(r Reading) (Window, bool) {
	for _, w := range r.Windows {
		if w.Seconds >= 600000 && w.Seconds <= 610000 {
			return w, true
		}
	}
	return Window{}, false
}
func newRequestID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	x := hex.EncodeToString(b)
	return x[:8] + "-" + x[8:12] + "-" + x[12:16] + "-" + x[16:20] + "-" + x[20:], nil
}
func resetReason(r Runtime, p Policy, s Settings, now time.Time) (string, Credit) {
	if !p.Monitor || len(r.Current.Credits) == 0 {
		return "", Credit{}
	}
	c := r.Current.Credits[0]
	if c.ID == "" || (!c.ExpiresAt.IsZero() && !c.ExpiresAt.After(now)) {
		return "", Credit{}
	}
	used := false
	for _, w := range r.Current.Windows {
		if w.Used > 0 && w.ResetAt.After(now) {
			used = true
		}
	}
	if !used {
		return "", Credit{}
	}
	if p.Expiring && !c.ExpiresAt.IsZero() && c.ExpiresAt.Sub(now) <= time.Duration(s.LeadMinutes)*time.Minute {
		return "expiring", c
	}
	if w, ok := weekly(r.Current); ok && p.Exhausted && w.Used >= 100 && w.ResetAt.After(now) && !now.Before(r.WeekLock) {
		return "weekly_exhausted", c
	}
	return "", Credit{}
}

// Called under Store.mu; save the intent before the irreversible request.
// Uncertain outcomes remain attached to the SAME credit and request ID.
func process(s *Store, id string, p Policy, c CodexCredentials, b Backend, now time.Time) error {
	r := s.runtime.Accounts[id]
	reading, err := b.Read(c)
	if err != nil {
		r.Error = err.Error()
		s.runtime.Accounts[id] = r
		return s.saveRuntime()
	}
	r.Previous, r.Current, r.Error = r.Current, reading, ""
	appendHistory(&r, reading)
	if r.Attempt != nil {
		a := r.Attempt
		available := false
		for _, credit := range reading.Credits {
			if credit.ID == a.CreditID {
				available = true
			}
		}
		recovered := false
		for _, before := range a.Before.Windows {
			for _, after := range reading.Windows {
				if before.Name == after.Name && before.Seconds == after.Seconds && after.Used < before.Used {
					recovered = true
				}
			}
		}
		if !available && reading.Count == a.Before.Count-1 && recovered {
			a.Status = "verified"
			s.event(id, fmt.Sprintf("重置已验证 / Reset verified: %s; request=%s; credit=%s", a.Reason, a.RequestID, a.CreditID), now)
			r.Attempt = nil
			// Every redemption imposes a seven-day exhausted-policy lock,
			// even if natural window timestamps shift after redemption.
			r.WeekLock = now.Add(7 * 24 * time.Hour)
			s.runtime.Accounts[id] = r
			if err = s.saveRuntime(); err != nil {
				return err
			}
			return nil
		}
		if a.Status == "blocked" || a.Status == "rejected" || a.Status == "accepted" || !available || a.Tries >= 5 {
			if !available || a.Tries >= 5 {
				a.Status = "blocked"
			}
			r.Error = "重置结果待核验，已暂停自动兑换；请核对历史 / Reset outcome unresolved; automatic redemption paused"
			s.runtime.Accounts[id] = r
			return s.saveRuntime()
		}
		allowed := (a.Reason == "expiring" && p.Expiring) || (a.Reason == "weekly_exhausted" && p.Exhausted)
		if now.Before(a.Next) || !p.Monitor || !allowed {
			s.runtime.Accounts[id] = r
			return s.saveRuntime()
		}
	} else {
		reason, credit := resetReason(r, p, s.settings, now)
		if reason == "" {
			s.runtime.Accounts[id] = r
			return s.saveRuntime()
		}
		requestID, err := newRequestID()
		if err != nil {
			return err
		}
		r.Attempt = &Attempt{RequestID: requestID, CreditID: credit.ID, Reason: reason, Started: now, Before: reading, Status: "pending"}
	}
	a := r.Attempt
	a.Tries++
	a.Next = now.Add(time.Duration(math.Pow(2, float64(a.Tries))) * time.Minute)
	s.runtime.Accounts[id] = r
	if err = s.saveRuntime(); err != nil {
		return fmt.Errorf("intent not durable; reset blocked: %w", err)
	}
	resp, err := b.Consume(c, a.RequestID, a.CreditID)
	switch {
	case err == nil && (resp.Code == ConsumeCodeReset || resp.Code == ConsumeCodeAlreadyRedeemed):
		a.Status = "accepted"
	case err != nil:
		r.Error = err.Error()
		if IsClientError(err) {
			a.Status = "rejected"
		}
	default:
		a.Status = "rejected"
		r.Error = "consume returned " + string(resp.Code)
	}
	s.event(id, "Reset request: "+a.Status, now)
	s.runtime.Accounts[id] = r
	return s.saveRuntime()
}
