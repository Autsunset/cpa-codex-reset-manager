package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestHostOAuthEmailIsNotChatGPTAccountID(t *testing.T) {
	old := hostCall
	defer func() { hostCall = old }()
	hostCall = func(method string, payload any) (json.RawMessage, error) {
		switch method {
		case "host.auth.list":
			return json.RawMessage(`{"files":[{"id":"codex-demo.json","auth_index":"idx","provider":"codex","account_type":"oauth","account":"person@example.invalid","email":"person@example.invalid"},{"id":"api.json","provider":"codex","account_type":"api_key","account":"SECRET_API_KEY"}]}`), nil
		case "host.auth.get":
			return json.RawMessage(`{"json":{"access_token":"SECRET_ACCESS_TOKEN","account_id":"real-chatgpt-id"}}`), nil
		default:
			return nil, errors.New("unexpected callback")
		}
	}
	accounts, err := listAccounts()
	if err != nil || len(accounts) != 1 || accounts[0].Key != "account:real-chatgpt-id" {
		t.Fatal(accounts, err)
	}
	if _, err := credentials(accounts[0]); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(accounts)
	if strings.Contains(string(raw), "SECRET") {
		t.Fatal("credential leaked")
	}
}

func TestTransientHostReadRetainsExplicitFallbackIdentity(t *testing.T) {
	old := hostCall
	defer func() { hostCall = old }()
	hostCall = func(method string, payload any) (json.RawMessage, error) {
		if method == "host.auth.list" {
			return json.RawMessage(`{"files":[{"id":"codex-demo.json","auth_index":"idx","provider":"codex","account":"person@example.invalid"}]}`), nil
		}
		return nil, errors.New("temporarily unavailable")
	}
	accounts, err := listAccounts()
	if err != nil || len(accounts) != 1 || accounts[0].Key != "id:codex-demo.json" {
		t.Fatal(accounts, err)
	}
}

func TestChangedCredentialIdentityBlocksReset(t *testing.T) {
	old := hostCall
	defer func() { hostCall = old }()
	hostCall = func(string, any) (json.RawMessage, error) {
		return json.RawMessage(`{"json":{"access_token":"token","account_id":"replacement-account"}}`), nil
	}
	if _, err := credentials(Account{AuthIndex: "idx", UpstreamID: "original-account"}); err == nil {
		t.Fatal("account changed without new consent")
	}
}
