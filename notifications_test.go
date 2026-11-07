package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func configuredDelivery(t *testing.T) *Store {
	t.Helper()
	s, err := openStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := defaultDelivery()
	c.SMTP = SMTPConfig{Enabled: true, Host: "smtp.example.invalid", Port: 465, TLS: "implicit", Username: "sender", Password: "secret-password", From: "sender@example.invalid", Recipients: []string{"one@example.invalid", "two@example.invalid", "one@example.invalid"}}
	c.Kinds = []string{"slow_down"}
	raw, _ := json.Marshal(c)
	if err := s.saveDelivery(raw); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	s.settings.Accounts["account"] = Policy{Monitor: true}
	s.runtime.Accounts["account"] = Runtime{Current: Reading{At: now, Windows: []Window{{Name: "primary", Used: 99, Seconds: 604800, ResetAt: now.Add(time.Hour)}}}}
	return s
}
func TestDeliverySecretsAreWriteOnlyAndPreserved(t *testing.T) {
	s := configuredDelivery(t)
	view, _ := json.Marshal(s.deliveryView())
	if strings.Contains(string(view), "secret-password") {
		t.Fatal("password returned")
	}
	if len(s.delivery.SMTP.Recipients) != 2 {
		t.Fatal("recipient dedup failed")
	}
	c := s.delivery
	c.SMTP.Password = ""
	raw, _ := json.Marshal(c)
	if err := s.saveDelivery(raw); err != nil {
		t.Fatal(err)
	}
	if s.delivery.SMTP.Password != "secret-password" {
		t.Fatal("blank replaced existing secret")
	}
	reopened, err := openStore(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.delivery.SMTP.Password != "secret-password" {
		t.Fatal("secret lost after restart")
	}
	if st, err := os.Stat(filepath.Join(s.dir, "notifications.json")); err != nil || (runtime.GOOS != "windows" && st.Mode().Perm()&0077 != 0) {
		t.Fatal("unsafe notification file permissions", err)
	}
}
func TestWebhookSecretNeverReturned(t *testing.T) {
	s, err := openStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := defaultDelivery()
	c.Webhook = WebhookConfig{Enabled: true, URL: "https://example.invalid/hook?token=secret-token"}
	raw, _ := json.Marshal(c)
	if err = s.saveDelivery(raw); err != nil {
		t.Fatal(err)
	}
	view, _ := json.Marshal(s.deliveryView())
	if strings.Contains(string(view), "secret-token") || strings.Contains(string(view), "example.invalid") {
		t.Fatal("webhook URL leaked")
	}
	c = s.delivery
	c.Webhook.URL = ""
	raw, _ = json.Marshal(c)
	if err = s.saveDelivery(raw); err != nil || s.delivery.Webhook.URL == "" {
		t.Fatal(err)
	}
}
func TestMultiRecipientFailureDoesNotRepeatSuccessfulRecipient(t *testing.T) {
	s := configuredDelivery(t)
	var mu sync.Mutex
	calls := map[string]int{}
	send := func(_ context.Context, j deliveryJob) error {
		mu.Lock()
		defer mu.Unlock()
		calls[j.record.Recipient]++
		if j.record.Recipient == "two@example.invalid" {
			return errors.New("temporary failure")
		}
		return nil
	}
	deliverNotifications(context.Background(), s, send)
	if calls["one@example.invalid"] != 1 || calls["two@example.invalid"] != 1 {
		t.Fatal(calls)
	}
	for id, r := range s.deliveryLog {
		if r.Status == "failed" {
			r.At = time.Now().Add(-11 * time.Minute)
			s.deliveryLog[id] = r
		}
	}
	deliverNotifications(context.Background(), s, send)
	if calls["one@example.invalid"] != 1 || calls["two@example.invalid"] != 2 {
		t.Fatal("successful recipient repeated", calls)
	}
	if err := s.saveRuntime(); err != nil {
		t.Fatal(err)
	}
	reopened, err := openStore(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	reopened.settings = s.settings
	deliverNotifications(context.Background(), reopened, send)
	if calls["one@example.invalid"] != 1 || calls["two@example.invalid"] != 2 {
		t.Fatal("restart ignored cooldown")
	}
}
func TestDisabledNotificationsAndFailedJournalNeverSend(t *testing.T) {
	s := configuredDelivery(t)
	calls := 0
	send := func(context.Context, deliveryJob) error { calls++; return nil }
	s.delivery.SMTP.Enabled = false
	deliverNotifications(context.Background(), s, send)
	if calls != 0 {
		t.Fatal("disabled channel sent")
	}
	s.delivery.SMTP.Enabled = true
	s.dir = filepath.Join(s.dir, "missing")
	deliverNotifications(context.Background(), s, send)
	if calls != 0 || s.deliveryError == "" {
		t.Fatal("sent without a durable journal")
	}
}

func TestUnknownDeliveryWaitsFullCooldown(t *testing.T) {
	s := configuredDelivery(t)
	send := func(context.Context, deliveryJob) error { return &uncertainDelivery{"acknowledgement missing"} }
	deliverNotifications(context.Background(), s, send)
	for id, r := range s.deliveryLog {
		if r.Status != "unknown" {
			t.Fatal(r)
		}
		r.At = time.Now().Add(-11 * time.Minute)
		s.deliveryLog[id] = r
	}
	if jobs := s.notificationJobs(time.Now()); len(jobs) != 0 {
		t.Fatal("uncertain deliveries retried as ordinary failures")
	}
}
func TestEnabledNotificationsNeedJournalOnRestart(t *testing.T) {
	s := configuredDelivery(t)
	if err := os.Remove(filepath.Join(s.dir, "notification-log.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := openStore(s.dir); err == nil {
		t.Fatal("enabled channel silently discarded delivery history")
	}
}
func TestSMTPValidationAndPrivateRecipientMessage(t *testing.T) {
	c := defaultDelivery()
	c.SMTP = SMTPConfig{Enabled: true, Host: "smtp.example.invalid", Port: 465, TLS: "implicit", From: "sender@example.invalid", Recipients: []string{"one@example.invalid"}}
	if err := c.validate(); err != nil {
		t.Fatal(err)
	}
	c.SMTP.TLS = "none"
	if err := c.validate(); err == nil {
		t.Fatal("plaintext SMTP accepted")
	}
	c.SMTP.TLS = "implicit"
	c.SMTP.From = "sender@example.invalid\r\nBcc: thief@example.invalid"
	if err := c.validate(); err == nil {
		t.Fatal("header injection accepted")
	}
	raw := formatEmail("sender@example.invalid", "one@example.invalid", NotificationMessage{ID: "id", Title: "额度提醒", Text: "余量不足\n请减速"})
	if strings.Contains(raw, "two@example.invalid") || !strings.Contains(raw, "To: one@example.invalid\r\n") {
		t.Fatal("recipient privacy broken")
	}
	body := strings.SplitN(raw, "\r\n\r\n", 2)[1]
	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(body, "\r\n", ""))
	if err != nil || !strings.Contains(string(decoded), "余量不足") {
		t.Fatal("UTF-8 email body broken", err)
	}
}
func TestSMTPRefusesCredentialsWithoutSTARTTLS(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	captured := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		_, _ = io.WriteString(conn, "220 mock smtp\r\n")
		b := make([]byte, 1024)
		n, _ := conn.Read(b)
		_, _ = io.WriteString(conn, "250-mock\r\n250 AUTH PLAIN\r\n")
		more, _ := io.ReadAll(conn)
		captured <- string(b[:n]) + string(more)
	}()
	port := listener.Addr().(*net.TCPAddr).Port
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err = sendSMTP(ctx, SMTPConfig{Host: "127.0.0.1", Port: port, TLS: "starttls", Username: "user", Password: "do-not-send"}, "one@example.invalid", NotificationMessage{})
	if err == nil {
		t.Fatal("accepted SMTP without TLS")
	}
	if wire := <-captured; strings.Contains(wire, "AUTH PLAIN") || strings.Contains(wire, "do-not-send") {
		t.Fatal("credentials sent without TLS")
	}
}
