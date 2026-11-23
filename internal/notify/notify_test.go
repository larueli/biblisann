package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
	"github.com/larueli/biblisann/internal/safehttp"
)

type fakeDoer struct {
	req    *http.Request
	body   string
	status int
}

func (f *fakeDoer) Do(req *http.Request) (*safehttp.Response, error) {
	f.req = req
	b, _ := io.ReadAll(req.Body)
	f.body = string(b)
	return &safehttp.Response{Status: f.status, Body: io.NopCloser(strings.NewReader(""))}, nil
}

func TestWebhookIsSigned(t *testing.T) {
	d := &fakeDoer{status: 200}
	ch := dbsqlc.NotificationChannel{Target: "https://hooks.example/x", Config: []byte(`{"secret":"s3cret"}`)}
	if err := (Webhook{HTTP: d}).Send(context.Background(), ch, Message{Event: "loan.overdue", Title: "T", Body: "B"}); err != nil {
		t.Fatal(err)
	}
	if got, want := d.req.Header.Get("X-Biblisann-Signature"), Sign("s3cret", []byte(d.body)); got != want || !strings.HasPrefix(got, "sha256=") {
		t.Errorf("signature %q, want %q", got, want)
	}
	var v map[string]string
	if err := json.Unmarshal([]byte(d.body), &v); err != nil || v["event"] != "loan.overdue" {
		t.Errorf("body %s", d.body)
	}
}

func TestPostRefusesBadTargetsAndStatuses(t *testing.T) {
	for _, target := range []string{"file:///etc/passwd", "ftp://x", "not a url", ""} {
		if err := post(context.Background(), &fakeDoer{status: 200}, target, nil, nil); err == nil {
			t.Errorf("target %q accepted", target)
		}
	}
	if err := post(context.Background(), &fakeDoer{status: 500}, "https://x.example", nil, nil); err == nil {
		t.Error("500 must be an error")
	}
}

func TestNtfy(t *testing.T) {
	d := &fakeDoer{status: 200}
	ch := dbsqlc.NotificationChannel{Target: "https://ntfy.example/topic", Config: []byte(`{"token":"tk_abc"}`)}
	if err := (Ntfy{HTTP: d}).Send(context.Background(), ch, Message{Title: "En retard : Dune é", Body: "body"}); err != nil {
		t.Fatal(err)
	}
	if d.req.Header.Get("Authorization") != "Bearer tk_abc" || d.body != "body" || strings.ContainsAny(d.req.Header.Get("Title"), "\r\n") {
		t.Errorf("headers %v body %q", d.req.Header, d.body)
	}
}

func TestBuildMessageBlocksHeaderInjection(t *testing.T) {
	msg, err := buildMessage("Biblisann <noreply@example.org>", "me@example.org", "Hello\r\nBcc: evil@example.org", "body é", time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	s := string(msg)
	if strings.Contains(s, "\r\nBcc:") {
		t.Errorf("subject injected a header:\n%s", s)
	}
	if !strings.Contains(s, "Content-Transfer-Encoding: base64") {
		t.Error("body must be encoded")
	}
	if _, err := buildMessage("noreply@example.org", "me@example.org\r\nBcc: x@y", "s", "b", time.Now()); err == nil {
		t.Error("recipient with CRLF must be refused")
	}
}

func TestValidTarget(t *testing.T) {
	cases := []struct {
		kind, target string
		ok           bool
	}{
		{"email", "me@example.org", true}, {"email", "Me <me@example.org>", false}, {"email", "nope", false},
		{"webhook", "https://hooks.example/x", true}, {"webhook", "https://u:p@hooks.example/x", false},
		{"ntfy", "ftp://x", false}, {"webpush", "https://push.example/abc", true}, {"webhook", "https://x\r\nHost: y", false},
	}
	for _, c := range cases {
		if got := ValidTarget(c.kind, c.target); got != c.ok {
			t.Errorf("%s %q = %v", c.kind, c.target, got)
		}
	}
}
