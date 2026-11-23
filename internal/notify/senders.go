package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
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
	"strings"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"

	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
	"github.com/larueli/biblisann/internal/safehttp"
)

// Doer sends an HTTP request through the SSRF-safe client.
type Doer interface {
	Do(req *http.Request) (*safehttp.Response, error)
}

func post(ctx context.Context, d Doer, target string, body []byte, headers map[string]string) error {
	u, err := url.Parse(target)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return errors.New("invalid target URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header.Set(k, clean(v))
	}
	resp, err := d.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.Status < 200 || resp.Status > 299 {
		return fmt.Errorf("target answered status %d", resp.Status)
	}
	return nil
}

// ---- webhook

// Webhook posts a JSON document signed with the channel secret (HMAC-SHA256 in the
// X-Biblisann-Signature header) so the receiver can verify it came from this instance.
type Webhook struct{ HTTP Doer }

// Kind implements Sender.
func (Webhook) Kind() string { return "webhook" }

// Sign returns the signature header value for a body.
func Sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// Send implements Sender.
func (w Webhook) Send(ctx context.Context, ch dbsqlc.NotificationChannel, m Message) error {
	body, _ := json.Marshal(map[string]string{"event": m.Event, "title": m.Title, "body": m.Body})
	var cfg struct {
		Secret string `json:"secret"`
	}
	_ = json.Unmarshal(ch.Config, &cfg)
	h := map[string]string{"Content-Type": "application/json", "User-Agent": "Biblisann-Webhook"}
	if cfg.Secret != "" {
		h["X-Biblisann-Signature"] = Sign(cfg.Secret, body)
	}
	return post(ctx, w.HTTP, ch.Target, body, h)
}

// ---- ntfy

// Ntfy publishes to an ntfy topic URL (https://ntfy.sh/my-topic or a self-hosted server).
type Ntfy struct{ HTTP Doer }

// Kind implements Sender.
func (Ntfy) Kind() string { return "ntfy" }

// Send implements Sender.
func (n Ntfy) Send(ctx context.Context, ch dbsqlc.NotificationChannel, m Message) error {
	var cfg struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(ch.Config, &cfg)
	h := map[string]string{"Title": mime.QEncoding.Encode("utf-8", m.Title), "Tags": "books", "Content-Type": "text/plain; charset=utf-8"}
	if cfg.Token != "" {
		h["Authorization"] = "Bearer " + cfg.Token
	}
	return post(ctx, n.HTTP, ch.Target, []byte(m.Body), h)
}

// ---- web push

// WebPush sends VAPID-authenticated push messages. The standard client comes from safehttp.
type WebPush struct {
	HTTP       *http.Client //nolint:forbidigo // the SSRF-safe client from safehttp.Client.Std
	PublicKey  string
	PrivateKey string
	Subject    string // mailto: or https: contact
}

// Kind implements Sender.
func (WebPush) Kind() string { return "webpush" }

// Send implements Sender.
func (w WebPush) Send(ctx context.Context, ch dbsqlc.NotificationChannel, m Message) error {
	var keys struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	}
	if err := json.Unmarshal(ch.Config, &keys); err != nil || keys.P256dh == "" || keys.Auth == "" {
		return errors.New("push subscription keys missing")
	}
	payload, _ := json.Marshal(map[string]string{"title": m.Title, "body": m.Body, "event": m.Event})
	resp, err := webpush.SendNotificationWithContext(ctx, payload, &webpush.Subscription{
		Endpoint: ch.Target, Keys: webpush.Keys{P256dh: keys.P256dh, Auth: keys.Auth}},
		&webpush.Options{HTTPClient: w.HTTP, Subscriber: w.Subject, VAPIDPublicKey: w.PublicKey,
			VAPIDPrivateKey: w.PrivateKey, TTL: 24 * 3600, Urgency: webpush.UrgencyNormal})
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("push service answered status %d", resp.StatusCode)
	}
	return nil
}

// ---- email

// SMTPConfig configures outgoing mail.
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	// Mode is "starttls" (default), "tls" (implicit TLS) or "none" (trusted local relay only).
	Mode string
}

// Email sends plain-text mail over SMTP.
type Email struct{ Config SMTPConfig }

// Kind implements Sender.
func (Email) Kind() string { return "email" }

func buildMessage(from, to, subject, body string, now time.Time) ([]byte, error) {
	f, err := mail.ParseAddress(from)
	if err != nil {
		return nil, fmt.Errorf("invalid From address")
	}
	t, err := mail.ParseAddress(to)
	if err != nil {
		return nil, fmt.Errorf("invalid recipient address")
	}
	var b bytes.Buffer
	b.WriteString("From: " + f.String() + "\r\n")
	b.WriteString("To: " + t.String() + "\r\n")
	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", clean(subject)) + "\r\n")
	b.WriteString("Date: " + now.UTC().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n\r\n")
	enc := base64.StdEncoding.EncodeToString([]byte(body))
	for len(enc) > 76 {
		b.WriteString(enc[:76] + "\r\n")
		enc = enc[76:]
	}
	b.WriteString(enc + "\r\n")
	return b.Bytes(), nil
}

// Send implements Sender.
func (e Email) Send(ctx context.Context, ch dbsqlc.NotificationChannel, m Message) error {
	msg, err := buildMessage(e.Config.From, ch.Target, m.Title, m.Body, time.Now())
	if err != nil {
		return err
	}
	from, _ := mail.ParseAddress(e.Config.From)
	to, _ := mail.ParseAddress(ch.Target)
	return e.deliver(ctx, from.Address, to.Address, msg)
}

func (e Email) deliver(ctx context.Context, from, to string, msg []byte) error {
	cfg := e.Config
	addr := net.JoinHostPort(cfg.Host, fmt.Sprint(cfg.Port))
	d := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("smtp connect: %w", err)
	}
	if cfg.Mode == "tls" {
		conn = tls.Client(conn, &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12})
	}
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	c, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("smtp handshake: %w", err)
	}
	defer func() { _ = c.Close() }()
	if cfg.Mode == "" || cfg.Mode == "starttls" {
		if err := c.StartTLS(&tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("smtp starttls: %w", err)
		}
	}
	if cfg.Username != "" {
		// PlainAuth refuses to send credentials over an unencrypted connection.
		if err := c.Auth(smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// ValidTarget checks a channel target at creation time.
func ValidTarget(kind, target string) bool {
	if target == "" || len(target) > 2000 || strings.ContainsAny(target, "\r\n") {
		return false
	}
	switch kind {
	case "email":
		a, err := mail.ParseAddress(target)
		return err == nil && a.Address == target
	default:
		u, err := url.Parse(target)
		return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil
	}
}

// SendMail sends a plain-text message to an address (password reset links, for example).
func (e Email) SendMail(ctx context.Context, to, subject, body string) error {
	msg, err := buildMessage(e.Config.From, to, subject, body, time.Now())
	if err != nil {
		return err
	}
	from, _ := mail.ParseAddress(e.Config.From)
	rcpt, _ := mail.ParseAddress(to)
	return e.deliver(ctx, from.Address, rcpt.Address, msg)
}
