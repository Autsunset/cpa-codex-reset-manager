package main

import (
	"encoding/json"
	"errors"
	"strings"
)

type Account struct {
	ID          string `json:"id"`
	AuthIndex   string `json:"auth_index"`
	Provider    string `json:"provider"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	Email       string `json:"email"`
	AccountType string `json:"account_type,omitempty"`
	UpstreamID  string `json:"-"`
	Disabled    bool   `json:"disabled"`
	Key         string `json:"key"`
}

var hostCall = callHostCallbackABI

func listAccounts() ([]Account, error) {
	raw, err := hostCall("host.auth.list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var v struct {
		Files []Account `json:"files"`
	}
	if err = json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	out := []Account{}
	for _, a := range v.Files {
		if !strings.EqualFold(a.Provider, "codex") && !strings.EqualFold(a.Type, "codex") {
			continue
		}
		// Host "account" is an OAuth email (or an API key for API-key
		// credentials), NOT ChatGPT's account ID. Never expose that field.
		if a.AccountType == "api_key" {
			continue
		}
		// Resolve the real identity from credential metadata. A transient
		// failure falls back to the credential ID, with no inherited consent.
		a.Key = "id:" + a.ID
		if c, err := credentials(a); err == nil && c.ChatGPTAccountID != "" {
			a.UpstreamID = c.ChatGPTAccountID
			a.Key = "account:" + c.ChatGPTAccountID
		}
		if a.ID == "" && a.UpstreamID == "" {
			continue
		}
		out = append(out, a)
	}
	return out, nil
}
func credentials(a Account) (CodexCredentials, error) {
	raw, err := hostCall("host.auth.get", map[string]any{"auth_index": a.AuthIndex})
	if err != nil {
		return CodexCredentials{}, err
	}
	var v struct {
		JSON json.RawMessage `json:"json"`
	}
	if err = json.Unmarshal(raw, &v); err != nil {
		return CodexCredentials{}, err
	}
	c, err := ExtractCodexCredentials(v.JSON)
	if err == nil && a.UpstreamID != "" && c.ChatGPTAccountID != a.UpstreamID {
		return c, errors.New("account identity changed; reset blocked")
	}
	return c, err
}
