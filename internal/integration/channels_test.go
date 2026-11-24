package integration_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"regexp"
	"testing"

	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
	"github.com/larueli/biblisann/internal/notify"
)

type captureMailer struct{ bodies []string }

func (m *captureMailer) SendMail(_ context.Context, _, _, body string) error {
	m.bodies = append(m.bodies, body)
	return nil
}

type countingSender struct{ n int }

func (*countingSender) Kind() string { return "email" }
func (s *countingSender) Send(context.Context, dbsqlc.NotificationChannel, notify.Message) error {
	s.n++
	return nil
}

func TestEmailChannelNeedsConfirmationAndRespectsTheDailyCap(t *testing.T) {
	e := newEnv(t)
	ctx, uid := e.user(t, "a@example.org")
	q := dbsqlc.New(e.pool)
	mailer := &captureMailer{}
	channels := notify.NewChannels(q)
	channels.SetMailer(mailer, 2)
	sender := &countingSender{}
	d := notify.NewDispatcher(q, slog.New(slog.NewTextHandler(io.Discard, nil)), sender)
	d.EmailDailyCap = 2

	ch, err := channels.Create(ctx, uid, "email", "someone@example.org", []byte("{}"))
	if err != nil || ch.ConfirmedAt != nil {
		t.Fatalf("create: %+v %v", ch, err)
	}
	if err := channels.SendCode(ctx, uid, ch.ID); err != nil { // quota: 1 of 2
		t.Fatal(err)
	}
	if err := d.Deliver(ctx, uid, notify.Message{Event: "test", Title: "t", Body: "b"}); err != nil || sender.n != 0 {
		t.Fatalf("unconfirmed channel must stay silent: sent=%d err=%v", sender.n, err)
	}

	if err := channels.Confirm(ctx, uid, ch.ID, "000000"); !errors.Is(err, notify.ErrBadCode) {
		t.Errorf("wrong code: %v", err)
	}
	code := regexp.MustCompile(`\d{6}`).FindString(mailer.bodies[len(mailer.bodies)-1])
	if err := channels.Confirm(ctx, uid, ch.ID, code); err != nil {
		t.Fatalf("right code: %v", err)
	}
	if err := channels.Confirm(ctx, uid, ch.ID, code); !errors.Is(err, notify.ErrNotFound) {
		t.Errorf("second confirmation: %v", err)
	}

	// one mail left in today's cap (the code took one), then silence
	m := notify.Message{Event: "test", Title: "t", Body: "b"}
	for range 3 {
		if err := d.Deliver(ctx, uid, m); err != nil {
			t.Fatal(err)
		}
	}
	if sender.n != 1 {
		t.Errorf("mails delivered today: %d, want 1", sender.n)
	}
}

func TestConfirmationAttemptsAreLimited(t *testing.T) {
	e := newEnv(t)
	ctx, uid := e.user(t, "a@example.org")
	channels := notify.NewChannels(dbsqlc.New(e.pool))
	mailer := &captureMailer{}
	channels.SetMailer(mailer, 20)
	ch, _ := channels.Create(ctx, uid, "email", "someone@example.org", []byte("{}"))
	if err := channels.SendCode(ctx, uid, ch.ID); err != nil {
		t.Fatal(err)
	}
	code := regexp.MustCompile(`\d{6}`).FindString(mailer.bodies[0])
	for range 5 {
		_ = channels.Confirm(ctx, uid, ch.ID, "999999")
	}
	if err := channels.Confirm(ctx, uid, ch.ID, code); !errors.Is(err, notify.ErrTooManyTries) {
		t.Errorf("right code after too many wrong ones: %v", err)
	}
	if err := channels.SendCode(ctx, uid, ch.ID); err != nil { // a new code resets the counter
		t.Fatal(err)
	}
	code = regexp.MustCompile(`\d{6}`).FindString(mailer.bodies[len(mailer.bodies)-1])
	if err := channels.Confirm(ctx, uid, ch.ID, code); err != nil {
		t.Errorf("fresh code: %v", err)
	}
}
