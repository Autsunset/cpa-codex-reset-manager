package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const pluginID = "cpa-codex-reset-manager"

var pluginVersion = "0.1.1"

type Policy struct {
	Monitor   bool `json:"monitor"`
	Expiring  bool `json:"expiring"`
	Exhausted bool `json:"exhausted"`
}

type Settings struct {
	Revision      int64             `json:"revision"`
	PollMinutes   int               `json:"poll_minutes"`
	LeadMinutes   int               `json:"lead_minutes"`
	ReminderHours int               `json:"reminder_hours"`
	Accounts      map[string]Policy `json:"accounts"`
}

func defaults() Settings {
	return Settings{PollMinutes: 5, LeadMinutes: 30, ReminderHours: 24, Accounts: map[string]Policy{}}
}

func (s Settings) validate() error {
	if s.PollMinutes < 1 || s.PollMinutes > 60 || s.LeadMinutes < s.PollMinutes*2 || s.LeadMinutes > 1440 || s.ReminderHours < 1 || s.ReminderHours > 168 || s.Accounts == nil {
		return errors.New("invalid settings: poll 1–60m; lead >= 2 polls and <= 1440m; reminder 1–168h; accounts required")
	}
	for id, p := range s.Accounts {
		if id == "" || (!p.Monitor && (p.Expiring || p.Exhausted)) {
			return errors.New("reset policies require monitoring and a nonempty account identity")
		}
	}
	return nil
}

type Window struct {
	Name    string    `json:"name"`
	Used    float64   `json:"used"`
	Seconds int       `json:"seconds"`
	ResetAt time.Time `json:"reset_at"`
}
type Reading struct {
	At      time.Time `json:"at"`
	Windows []Window  `json:"windows"`
	Credits []Credit  `json:"credits"`
	Count   int       `json:"count"`
}
type Attempt struct {
	RequestID string    `json:"request_id"`
	CreditID  string    `json:"credit_id"`
	Reason    string    `json:"reason"`
	Started   time.Time `json:"started"`
	Before    Reading   `json:"before"`
	Status    string    `json:"status"`
	Tries     int       `json:"tries"`
	Next      time.Time `json:"next"`
}
type Runtime struct {
	Current  Reading   `json:"current"`
	Previous Reading   `json:"previous"`
	Attempt  *Attempt  `json:"attempt,omitempty"`
	WeekLock time.Time `json:"week_lock"`
	Error    string    `json:"error,omitempty"`
}
type Event struct {
	At      time.Time `json:"at"`
	Account string    `json:"account"`
	Message string    `json:"message"`
}
type RuntimeState struct {
	Accounts map[string]Runtime `json:"accounts"`
	Events   []Event            `json:"events"`
}

// Settings and runtime have separate files: polling can never overwrite consent.
// Every successful write fsyncs the file before rename. Corruption is an error,
// never interpreted as fresh consent or an empty reset journal.
type Store struct {
	mu       sync.Mutex
	dir      string
	settings Settings
	runtime  RuntimeState
}

func openStore(dir string) (*Store, error) {
	if !filepath.IsAbs(dir) {
		return nil, errors.New("data_dir must be absolute and on persistent storage")
	}
	s := &Store{dir: dir, settings: defaults(), runtime: RuntimeState{Accounts: map[string]Runtime{}}}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	_, settingsErr := os.Stat(filepath.Join(dir, "settings.json"))
	_, runtimeErr := os.Stat(filepath.Join(dir, "runtime.json"))
	if errors.Is(settingsErr, os.ErrNotExist) != errors.Is(runtimeErr, os.ErrNotExist) {
		return nil, errors.New("incomplete state: restore settings.json and runtime.json together; refusing to discard redemption history")
	}
	for name, target := range map[string]any{"settings.json": &s.settings, "runtime.json": &s.runtime} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		// Decode existing files into zero values so valid JSON such as null
		// or {} cannot masquerade as a fresh installation.
		if name == "settings.json" {
			s.settings = Settings{}
		} else {
			s.runtime = RuntimeState{}
		}
		if err = json.Unmarshal(b, target); err != nil {
			return nil, fmt.Errorf("%s corrupt; restore backup before enabling resets: %w", name, err)
		}
	}
	if err := s.settings.validate(); err != nil {
		return nil, err
	}
	if s.runtime.Accounts == nil {
		return nil, errors.New("runtime accounts missing")
	}
	// Test durability before accepting the plugin registration.
	if err := atomicJSON(filepath.Join(dir, "settings.json"), s.settings); err != nil {
		return nil, err
	}
	if err := s.saveRuntime(); err != nil {
		return nil, err
	}
	return s, nil
}
func atomicJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".write-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}
func (s *Store) saveRuntime() error {
	return atomicJSON(filepath.Join(s.dir, "runtime.json"), s.runtime)
}
func (s *Store) saveSettings(next Settings) error {
	if err := next.validate(); err != nil {
		return err
	}
	if next.Revision != s.settings.Revision {
		return errors.New("settings changed elsewhere; refresh before saving")
	}
	next.Revision++
	if err := atomicJSON(filepath.Join(s.dir, "settings.json"), next); err != nil {
		return err
	}
	s.settings = next
	return nil
}
func (s *Store) event(id, msg string, now time.Time) {
	s.runtime.Events = append(s.runtime.Events, Event{now, id, msg})
	if len(s.runtime.Events) > 500 {
		s.runtime.Events = s.runtime.Events[len(s.runtime.Events)-500:]
	}
}
