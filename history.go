package main

import (
	"math"
	"sort"
	"time"
)

type HistorySample struct {
	At      time.Time `json:"at"`
	Windows []Window  `json:"windows"`
}

const historyLimit = 4096

func appendHistory(r *Runtime, reading Reading) {
	if reading.At.IsZero() || len(reading.Windows) == 0 {
		return
	}
	if len(r.History) > 0 && !reading.At.After(r.History[len(r.History)-1].At) {
		return
	}
	r.History = append(r.History, HistorySample{reading.At, append([]Window(nil), reading.Windows...)})
	cut := 0
	for cut < len(r.History) && r.History[cut].At.Before(reading.At.Add(-14*24*time.Hour)) {
		cut++
	}
	if len(r.History)-cut > historyLimit {
		cut = len(r.History) - historyLimit
	}
	r.History = append([]HistorySample(nil), r.History[cut:]...)
}
func samples(r Runtime) []HistorySample {
	points := append([]HistorySample(nil), r.History...)
	points = append(points, HistorySample{r.Previous.At, r.Previous.Windows}, HistorySample{r.Current.At, r.Current.Windows})
	sort.SliceStable(points, func(i, j int) bool { return points[i].At.Before(points[j].At) })
	out := []HistorySample{}
	for _, p := range points {
		if p.At.IsZero() || len(p.Windows) == 0 {
			continue
		}
		if len(out) > 0 && p.At.Equal(out[len(out)-1].At) {
			out[len(out)-1] = p
			continue
		}
		out = append(out, p)
	}
	return out
}
func sameWindow(a, b Window) bool {
	return a.Name == b.Name && a.Seconds == b.Seconds && a.ResetAt.Equal(b.ResetAt)
}
func recentRate(r Runtime, w Window, s Settings) (float64, bool) {
	latest := r.Current.At
	oldest := latest
	used := w.Used
	points := samples(r)
	for i := len(points) - 1; i >= 0; i-- {
		p := points[i]
		if p.At.After(latest) {
			continue
		}
		if latest.Sub(p.At) > time.Hour || oldest.Sub(p.At) > time.Duration(s.PollMinutes*3)*time.Minute {
			break
		}
		found := false
		for _, prev := range p.Windows {
			if sameWindow(prev, w) && prev.Used <= used {
				oldest, used = p.At, prev.Used
				found = true
				break
			}
		}
		if !found {
			break
		}
	}
	span := latest.Sub(oldest)
	if span < time.Minute {
		return 0, false
	}
	return math.Max(0, (w.Used-used)/span.Hours()), true
}

type Forecast struct {
	Window           string     `json:"window"`
	Seconds          int        `json:"seconds"`
	Remaining        float64    `json:"remaining"`
	ObservedAt       time.Time  `json:"observed_at"`
	ResetAt          time.Time  `json:"reset_at"`
	Deadline         time.Time  `json:"deadline"`
	Rate             float64    `json:"rate_per_hour"`
	TargetRate       float64    `json:"target_per_hour"`
	Known            bool       `json:"known"`
	Kind             string     `json:"kind"`
	ExhaustAt        *time.Time `json:"exhaust_at,omitempty"`
	RemainingAtReset *float64   `json:"remaining_at_reset,omitempty"`
}

func forecasts(id string, r Runtime, p Policy, s Settings, now time.Time) []Forecast {
	out := []Forecast{}
	ns := notices(id, r, p, s, now)
	for _, w := range r.Current.Windows {
		f := Forecast{Window: w.Name, Seconds: w.Seconds, Remaining: 100 - w.Used, ObservedAt: r.Current.At, ResetAt: w.ResetAt, Deadline: w.ResetAt, Kind: "stale"}
		for _, n := range ns {
			if n.Window != w.Name {
				continue
			}
			f.Kind, f.Deadline, f.Rate, f.TargetRate = n.Kind, n.Deadline, n.Rate, n.TargetRate
			_, f.Known = recentRate(r, w, s)
		}
		if f.Known && f.Deadline.After(now) {
			v := math.Max(0, math.Min(100, f.Remaining-f.Rate*f.Deadline.Sub(r.Current.At).Hours()))
			f.RemainingAtReset = &v
			if f.Rate > 0 {
				seconds := f.Remaining / f.Rate * 3600
				// Avoid duration overflow and misleading decades-long ETAs.
				if seconds <= 366*24*3600 {
					at := r.Current.At.Add(time.Duration(seconds * float64(time.Second)))
					f.ExhaustAt = &at
				}
			}
		}
		out = append(out, f)
	}
	return out
}
