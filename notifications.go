package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type SMTPConfig struct {
	Enabled    bool     `json:"enabled"`
	Host       string   `json:"host"`
	Port       int      `json:"port"`
	TLS        string   `json:"tls"` // implicit or starttls; never plaintext
	Username   string   `json:"username"`
	Password   string   `json:"password,omitempty"`
	From       string   `json:"from"`
	Recipients []string `json:"recipients"`
}
type WebhookConfig struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url,omitempty"`
}
type DeliveryConfig struct {
	Revision        int64         `json:"revision"`
	Language        string        `json:"language"`
	CooldownMinutes int           `json:"cooldown_minutes"`
	Kinds           []string      `json:"kinds"`
	SMTP            SMTPConfig    `json:"smtp"`
	Webhook         WebhookConfig `json:"webhook"`
}
type DeliveryRecord struct {
	At        time.Time `json:"at"`
	Account   string    `json:"account"`
	Kind      string    `json:"kind"`
	Channel   string    `json:"channel"`
	Recipient string    `json:"recipient"`
	Status    string    `json:"status"`
	Error     string    `json:"error,omitempty"`
	MessageID string    `json:"message_id,omitempty"`
}

func defaultDelivery() DeliveryConfig {
	return DeliveryConfig{Language: "zh-CN", CooldownMinutes: 360, Kinds: []string{"slow_down", "speed_up", "reset_soon", "credit_expiring"}, SMTP: SMTPConfig{Port: 465, TLS: "implicit", Recipients: []string{}}}
}
func (s *Store) loadDelivery() error {
	s.delivery = defaultDelivery()
	s.deliveryLog = map[string]DeliveryRecord{}
	for name, target := range map[string]any{"notifications.json": &s.delivery, "notification-log.json": &s.deliveryLog} {
		raw, err := os.ReadFile(filepath.Join(s.dir, name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if string(bytes.TrimSpace(raw)) == "null" {
			return fmt.Errorf("%s is invalid", name)
		}
		if name == "notifications.json" {
			s.delivery = DeliveryConfig{}
		}
		if err = json.Unmarshal(raw, target); err != nil {
			return fmt.Errorf("%s is corrupt: %w", name, err)
		}
	}
	if s.delivery.SMTP.Enabled || s.delivery.Webhook.Enabled {
		if _, err := os.Stat(filepath.Join(s.dir, "notification-log.json")); err != nil {
			return errors.New("enabled notifications require their delivery journal; restore notification-log.json")
		}
	}
	return s.delivery.validate()
}
func cleanAddress(value string) (string, error) {
	if strings.ContainsAny(value, "\r\n") {
		return "", errors.New("email must not contain newlines")
	}
	a, err := mail.ParseAddress(strings.TrimSpace(value))
	if err != nil {
		return "", errors.New("invalid email address")
	}
	return a.Address, nil
}
func (c *DeliveryConfig) validate() error {
	if c.Language != "zh-CN" && c.Language != "en" {
		return errors.New("notification language must be zh-CN or en")
	}
	if c.CooldownMinutes < 15 || c.CooldownMinutes > 10080 {
		return errors.New("notification cooldown must be 15–10080 minutes")
	}
	for _, k := range c.Kinds {
		if k != "slow_down" && k != "speed_up" && k != "reset_soon" && k != "credit_expiring" && k != "stale" {
			return errors.New("unsupported notification kind")
		}
	}
	if c.SMTP.Enabled {
		if c.SMTP.Host == "" || strings.ContainsAny(c.SMTP.Host, "/ \r\n") || c.SMTP.Port < 1 || c.SMTP.Port > 65535 {
			return errors.New("invalid SMTP host or port")
		}
		if c.SMTP.TLS != "implicit" && c.SMTP.TLS != "starttls" {
			return errors.New("SMTP requires implicit TLS or STARTTLS")
		}
		from, err := cleanAddress(c.SMTP.From)
		if err != nil {
			return err
		}
		c.SMTP.From = from
		if len(c.SMTP.Recipients) < 1 || len(c.SMTP.Recipients) > 50 {
			return errors.New("configure 1–50 recipient addresses")
		}
	}
	if len(c.SMTP.Recipients) > 50 {
		return errors.New("at most 50 recipient addresses")
	}
	recipients := []string{}
	seen := map[string]bool{}
	for _, raw := range c.SMTP.Recipients {
		a, err := cleanAddress(raw)
		if err != nil {
			return err
		}
		if !seen[strings.ToLower(a)] {
			seen[strings.ToLower(a)] = true
			recipients = append(recipients, a)
		}
	}
	c.SMTP.Recipients = recipients
	if c.Webhook.URL != "" {
		u, err := url.Parse(c.Webhook.URL)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
			return errors.New("webhook must be an HTTPS URL without userinfo or fragment")
		}
	}
	if c.Webhook.Enabled && c.Webhook.URL == "" {
		return errors.New("webhook URL required")
	}
	return nil
}
func (s *Store) deliveryView() any {
	c := s.delivery
	hasPassword, hasURL := c.SMTP.Password != "", c.Webhook.URL != ""
	c.SMTP.Password = ""
	c.Webhook.URL = ""
	log := []DeliveryRecord{}
	for _, r := range s.deliveryLog {
		log = append(log, r)
	}
	sort.Slice(log, func(i, j int) bool { return log[i].At.After(log[j].At) })
	if len(log) > 100 {
		log = log[:100]
	}
	return map[string]any{"config": c, "password_set": hasPassword, "webhook_set": hasURL, "deliveries": log, "error": s.deliveryError}
}
func (s *Store) saveDelivery(raw []byte) error {
	var v struct {
		DeliveryConfig
		ClearPassword bool `json:"clear_password"`
		ClearWebhook  bool `json:"clear_webhook"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return errors.New("invalid notification settings")
	}
	c := v.DeliveryConfig
	if c.Revision != s.delivery.Revision {
		return errors.New("notification settings changed elsewhere; refresh first")
	}
	if c.SMTP.Password == "" && !v.ClearPassword {
		c.SMTP.Password = s.delivery.SMTP.Password
	}
	if c.Webhook.URL == "" && !v.ClearWebhook {
		c.Webhook.URL = s.delivery.Webhook.URL
	}
	if err := c.validate(); err != nil {
		return err
	}
	c.Revision++
	if c.SMTP.Enabled || c.Webhook.Enabled {
		if err := s.saveDeliveryLog(); err != nil {
			return err
		}
	}
	if err := atomicJSON(filepath.Join(s.dir, "notifications.json"), c); err != nil {
		return err
	}
	s.delivery = c
	return nil
}

type NotificationMessage struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Text   string `json:"text"`
	Notice Notice `json:"notice"`
}
type deliveryJob struct {
	cfg     DeliveryConfig
	record  DeliveryRecord
	id      string
	message NotificationMessage
}

func notificationCandidates(id string, r Runtime, p Policy, s Settings, now time.Time) []Notice {
	ns := notices(id, r, p, s, now)
	if !p.Monitor || r.Current.At.IsZero() || r.Error != "" || now.Sub(r.Current.At) > time.Duration(s.PollMinutes*3)*time.Minute {
		return ns
	}
	for _, w := range r.Current.Windows {
		if w.ResetAt.After(now) && w.ResetAt.Sub(now) <= time.Duration(s.ReminderHours)*time.Hour {
			rate, known := recentRate(r, w, s)
			ns = append(ns, Notice{Account: id, Kind: "reset_soon", Window: w.Name, Remaining: 100 - w.Used, Rate: rate, RateKnown: known, TargetRate: (100 - w.Used) / w.ResetAt.Sub(now).Hours(), Deadline: w.ResetAt, Message: "额度即将自然重置 / Quota resets soon"})
		}
	}
	if len(r.Current.Credits) > 0 {
		c := r.Current.Credits[0]
		if !c.ExpiresAt.IsZero() && c.ExpiresAt.After(now) && c.ExpiresAt.Sub(now) <= time.Duration(s.ReminderHours)*time.Hour {
			ns = append(ns, Notice{Account: id, Kind: "credit_expiring", Window: "credits", Deadline: c.ExpiresAt, Message: "重置券即将到期，请查看；提醒不会自动兑换 / A reset credit expires soon; this reminder does not redeem it"})
		}
	}
	return ns
}
func notificationText(n Notice, label, language string) NotificationMessage {
	titles := map[string][2]string{
		"slow_down": {"建议减缓用量", "Slow down usage"}, "speed_up": {"重置临近，可加快用量", "Reset soon: room for more work"},
		"reset_soon": {"额度即将重置", "Quota resets soon"}, "credit_expiring": {"重置券即将到期", "Reset credit expires soon"},
		"stale": {"额度数据暂不可用", "Quota data unavailable"},
	}
	index := 0
	if language == "en" {
		index = 1
	}
	title := titles[n.Kind][index]
	message := n.Message
	if parts := strings.SplitN(message, " / ", 2); len(parts) == 2 {
		message = parts[index]
	}
	if label == "" {
		label = n.Account
	}
	var text string
	if language == "en" {
		text = fmt.Sprintf("Account: %s\n%s\nRemaining: %.1f%%", label, message, n.Remaining)
		if n.RateKnown {
			text += fmt.Sprintf("\nObserved pace: %.2f%%/h\nBudget pace: %.2f%%/h", n.Rate, n.TargetRate)
		}
	} else {
		text = fmt.Sprintf("账号：%s\n%s\n剩余额度：%.1f%%", label, message, n.Remaining)
		if n.RateKnown {
			text += fmt.Sprintf("\n当前速度：%.2f%%/小时\n可用速度：%.2f%%/小时", n.Rate, n.TargetRate)
		}
	}
	if n.Kind == "stale" || n.Kind == "credit_expiring" {
		text = label + "\n" + message
	}
	if !n.Deadline.IsZero() {
		text += "\n" + n.Deadline.Format(time.RFC3339)
	}
	return NotificationMessage{Title: "Codex · " + title, Text: text, Notice: n}
}
func deliveryID(n Notice, channel, target string) string {
	sum := sha256.Sum256([]byte(n.Account + "|" + n.Window + "|" + n.Kind + "|" + n.Deadline.Format(time.RFC3339) + "|" + channel + "|" + target))
	return hex.EncodeToString(sum[:])
}
func eligibleDelivery(r DeliveryRecord, c DeliveryConfig, now time.Time) bool {
	wait := time.Duration(c.CooldownMinutes) * time.Minute
	if r.Status == "failed" {
		wait = 10 * time.Minute
	}
	return r.At.IsZero() || now.Sub(r.At) >= wait
}
func (s *Store) notificationJobs(now time.Time) []deliveryJob {
	for id, r := range s.deliveryLog {
		if now.Sub(r.At) > 30*24*time.Hour {
			delete(s.deliveryLog, id)
		}
	}
	cfg := s.delivery
	if !cfg.SMTP.Enabled && !cfg.Webhook.Enabled {
		return nil
	}
	kinds := map[string]bool{}
	for _, k := range cfg.Kinds {
		kinds[k] = true
	}
	jobs := []deliveryJob{}
	for id, p := range s.settings.Accounts {
		r := s.runtime.Accounts[id]
		for _, n := range notificationCandidates(id, r, p, s.settings, now) {
			if !kinds[n.Kind] {
				continue
			}
			message := notificationText(n, r.Label, cfg.Language)
			add := func(channel, target string) {
				key := deliveryID(n, channel, target)
				if !eligibleDelivery(s.deliveryLog[key], cfg, now) {
					return
				}
				m := message
				m.ID = key
				recipient := target
				if channel == "webhook" {
					recipient = "webhook"
				}
				jobs = append(jobs, deliveryJob{cfg, DeliveryRecord{At: now, Account: id, Kind: n.Kind, Channel: channel, Recipient: recipient, Status: "sending"}, key, m})
			}
			if cfg.SMTP.Enabled {
				for _, recipient := range cfg.SMTP.Recipients {
					add("smtp", recipient)
				}
			}
			if cfg.Webhook.Enabled {
				add("webhook", cfg.Webhook.URL)
			}
		}
	}
	return jobs
}
func (s *Store) saveDeliveryLog() error {
	return atomicJSON(filepath.Join(s.dir, "notification-log.json"), s.deliveryLog)
}

type notificationSender func(context.Context, deliveryJob) error
type uncertainDelivery struct{ message string }

func (e *uncertainDelivery) Error() string { return e.message }

func deliverNotifications(ctx context.Context, s *Store, send notificationSender) {
	s.mu.Lock()
	jobs := s.notificationJobs(time.Now())
	s.mu.Unlock()
	if len(jobs) == 0 {
		return
	}
	queue := make(chan deliveryJob)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range queue {
				if ctx.Err() != nil {
					return
				}
				s.mu.Lock()
				current := false
				target := job.record.Recipient
				if job.record.Channel == "webhook" {
					target = job.cfg.Webhook.URL
				}
				for _, n := range notificationCandidates(job.record.Account, s.runtime.Accounts[job.record.Account], s.settings.Accounts[job.record.Account], s.settings, time.Now()) {
					if deliveryID(n, job.record.Channel, target) == job.id {
						current = true
						break
					}
				}
				if ctx.Err() != nil || !current || s.delivery.Revision != job.cfg.Revision || !eligibleDelivery(s.deliveryLog[job.id], job.cfg, time.Now()) {
					s.mu.Unlock()
					continue
				}
				old, existed := s.deliveryLog[job.id]
				job.record.At = time.Now()
				if old.Status == "failed" && old.MessageID != "" {
					job.message.ID = old.MessageID
				} else {
					job.message.ID = job.id + "." + strconv.FormatInt(job.record.At.UnixNano(), 36)
				}
				job.record.MessageID = job.message.ID
				s.deliveryLog[job.id] = job.record
				if err := s.saveDeliveryLog(); err != nil {
					s.deliveryError = "notification journal could not be saved; message not sent"
					if existed {
						s.deliveryLog[job.id] = old
					} else {
						delete(s.deliveryLog, job.id)
					}
					s.mu.Unlock()
					continue
				}
				s.deliveryError = ""
				s.mu.Unlock()
				sendCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
				err := send(sendCtx, job)
				cancel()
				s.mu.Lock()
				rec := job.record
				rec.At = time.Now()
				if err == nil {
					rec.Status = "sent"
				} else {
					rec.Status = "failed"
					var uncertain *uncertainDelivery
					if errors.As(err, &uncertain) {
						rec.Status = "unknown"
					}
					rec.Error = err.Error()
				}
				s.deliveryLog[job.id] = rec
				if err := s.saveDeliveryLog(); err != nil {
					s.deliveryError = "delivery result could not be saved; outcome awaits confirmation"
					s.deliveryLog[job.id] = job.record
				}
				s.mu.Unlock()
			}
		}()
	}
	for _, job := range jobs {
		select {
		case queue <- job:
		case <-ctx.Done():
			close(queue)
			wg.Wait()
			return
		}
	}
	close(queue)
	wg.Wait()
}
func sendNotification(ctx context.Context, job deliveryJob) error {
	if job.record.Channel == "smtp" {
		return sendSMTP(ctx, job.cfg.SMTP, job.record.Recipient, job.message)
	}
	payload, _ := json.Marshal(job.message)
	req, err := http.NewRequestWithContext(ctx, "POST", job.cfg.Webhook.URL, bytes.NewReader(payload))
	if err != nil {
		return errors.New("invalid webhook request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", job.message.ID)
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return &uncertainDelivery{"webhook delivery outcome unknown"}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned HTTP %d", resp.StatusCode)
	}
	return nil
}
func sendSMTP(ctx context.Context, c SMTPConfig, recipient string, m NotificationMessage) error {
	if c.TLS != "implicit" && c.TLS != "starttls" {
		return errors.New("SMTP TLS required")
	}
	address := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return errors.New("SMTP connection failed")
	}
	defer conn.Close()
	rawConn := conn
	stop := context.AfterFunc(ctx, func() { _ = rawConn.Close() })
	defer stop()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	tlsConfig := &tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12}
	if c.TLS == "implicit" {
		tlsConn := tls.Client(conn, tlsConfig)
		if err = tlsConn.HandshakeContext(ctx); err != nil {
			return errors.New("SMTP TLS verification failed")
		}
		conn = tlsConn
	}
	client, err := smtp.NewClient(conn, c.Host)
	if err != nil {
		return errors.New("SMTP greeting failed")
	}
	defer client.Close()
	if c.TLS == "starttls" {
		if err = client.StartTLS(tlsConfig); err != nil {
			return errors.New("SMTP STARTTLS failed")
		}
	}
	if c.Username != "" {
		if err = client.Auth(smtp.PlainAuth("", c.Username, c.Password, c.Host)); err != nil {
			return errors.New("SMTP authentication failed")
		}
	}
	if err = client.Mail(c.From); err != nil {
		return errors.New("SMTP sender rejected")
	}
	if err = client.Rcpt(recipient); err != nil {
		return errors.New("SMTP recipient rejected")
	}
	w, err := client.Data()
	if err != nil {
		return errors.New("SMTP DATA rejected")
	}
	_, err = io.WriteString(w, formatEmail(c.From, recipient, m))
	if err != nil {
		return &uncertainDelivery{"SMTP message delivery outcome unknown"}
	}
	if err = w.Close(); err != nil {
		return &uncertainDelivery{"SMTP delivery acknowledgement missing; outcome unknown"}
	}
	// DATA acceptance is success; a failed QUIT must not cause duplicate mail.
	_ = client.Quit()
	return nil
}
func formatEmail(from, to string, m NotificationMessage) string {
	title := strings.NewReplacer("\r", " ", "\n", " ").Replace(m.Title)
	body := strings.ReplaceAll(strings.ReplaceAll(m.Text, "\r\n", "\n"), "\n", "\r\n")
	encoded := base64.StdEncoding.EncodeToString([]byte(body))
	var wrapped strings.Builder
	for len(encoded) > 76 {
		wrapped.WriteString(encoded[:76] + "\r\n")
		encoded = encoded[76:]
	}
	wrapped.WriteString(encoded + "\r\n")
	return "From: " + from + "\r\nTo: " + to + "\r\nSubject: " + mime.QEncoding.Encode("utf-8", title) + "\r\nDate: " + time.Now().Format(time.RFC1123Z) + "\r\nMessage-ID: <" + m.ID + "@codex-quota-steward.local>\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: base64\r\n\r\n" + wrapped.String()
}
