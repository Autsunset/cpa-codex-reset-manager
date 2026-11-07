package main

import (
	"encoding/json"
	"errors"
	"strings"
)

type Account struct {
	ID        string `json:"id"`
	AuthIndex string `json:"auth_index"`
	Provider  string `json:"provider"`
	Type      string `json:"type"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	Account   string `json:"account"`
	Disabled  bool   `json:"disabled"`
	Key       string `json:"key"`
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
		// Prefer upstream identity over CPA's changeable list index.
		a.Key = "id:" + a.ID
		if a.Account != "" {
			a.Key = "account:" + a.Account
		}
		if a.ID == "" && a.Account == "" {
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
	if err == nil && a.Account != "" && c.ChatGPTAccountID != a.Account {
		return c, errors.New("account identity changed; reset blocked")
	}
	return c, err
}
