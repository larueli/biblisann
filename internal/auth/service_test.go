package auth_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/larueli/biblisann/internal/auth"
	"github.com/larueli/biblisann/internal/dbtest"
)

func TestRegisterLoginLogout(t *testing.T) {
	ctx := context.Background()
	svc, err := auth.NewService(dbtest.Pool(t), auth.RegistrationFirstUser)
	if err != nil {
		t.Fatal(err)
	}
	u, err := svc.Register(ctx, "Me@Example.org", "tulips in the garden", "Me")
	if err != nil {
		t.Fatal(err)
	}
	if !u.IsAdmin {
		t.Error("first user must be admin")
	}
	if _, err := svc.Register(ctx, "second@example.org", "tulips in the garden", "Two"); !errors.Is(err, auth.ErrRegistrationClosed) {
		t.Errorf("second registration: %v, want closed", err)
	}

	if _, err := svc.Login(ctx, "me@example.org", "wrong password!!", "ua"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("bad password: %v", err)
	}
	if _, err := svc.Login(ctx, "nobody@example.org", "tulips in the garden", "ua"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("unknown user must look like a bad password: %v", err)
	}
	sess, err := svc.Login(ctx, "ME@example.org", "tulips in the garden", "ua")
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.Lookup(ctx, sess.Token)
	if err != nil || got.UserID != u.ID || got.CSRFToken != sess.CSRFToken {
		t.Fatalf("lookup: %+v %v", got, err)
	}
	if err := svc.Logout(ctx, sess.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Lookup(ctx, sess.Token); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Errorf("session survived logout: %v", err)
	}
}

func TestConcurrentFirstUsers(t *testing.T) {
	ctx := context.Background()
	svc, _ := auth.NewService(dbtest.Pool(t), auth.RegistrationFirstUser)
	var wg sync.WaitGroup
	results := make(chan error, 5)
	for i := range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.Register(ctx, "u"+string(rune('a'+i))+"@example.org", "tulips in the garden", "")
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	ok := 0
	for err := range results {
		if err == nil {
			ok++
		}
	}
	if ok != 1 {
		t.Errorf("%d registrations succeeded, want exactly 1", ok)
	}
}

func TestChangePasswordEndsSessions(t *testing.T) {
	ctx := context.Background()
	svc, _ := auth.NewService(dbtest.Pool(t), auth.RegistrationOpen)
	u, _ := svc.Register(ctx, "a@example.org", "tulips in the garden", "A")
	sess, _ := svc.Login(ctx, "a@example.org", "tulips in the garden", "")
	if err := svc.ChangePassword(ctx, u.ID, "nope nope nope", "another long passphrase"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("wrong current password: %v", err)
	}
	if err := svc.ChangePassword(ctx, u.ID, "tulips in the garden", "another long passphrase"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Lookup(ctx, sess.Token); err == nil {
		t.Error("old session must be revoked")
	}
	if _, err := svc.Login(ctx, "a@example.org", "another long passphrase", ""); err != nil {
		t.Errorf("new password: %v", err)
	}
	if _, err := svc.Register(ctx, "a@EXAMPLE.org", "tulips in the garden", ""); !errors.Is(err, auth.ErrEmailTaken) {
		t.Errorf("duplicate email (case-insensitive): %v", err)
	}
}

type captureMailer struct{ to, subject, body string }

func (c *captureMailer) SendMail(_ context.Context, to, subject, body string) error {
	c.to, c.subject, c.body = to, subject, body
	return nil
}

func TestTOTPAndRecoveryFlow(t *testing.T) {
	ctx := context.Background()
	svc, _ := auth.NewService(dbtest.Pool(t), auth.RegistrationOpen)
	if err := svc.EnableMFA(bytes.Repeat([]byte{1}, 32)); err != nil {
		t.Fatal(err)
	}
	u, _ := svc.Register(ctx, "a@example.org", "tulips in the garden", "A")
	secret, _, err := svc.EnrollTOTP(ctx, u.ID, "Biblisann")
	if err != nil {
		t.Fatal(err)
	}
	// Before confirmation login stays single-factor.
	if _, err := svc.Login(ctx, "a@example.org", "tulips in the garden", ""); err != nil {
		t.Fatalf("login before confirm: %v", err)
	}
	if _, err := svc.ConfirmTOTP(ctx, u.ID, "000000"); !errors.Is(err, auth.ErrInvalidCode) {
		t.Errorf("wrong code: %v", err)
	}
	code := auth.CurrentTOTP(secret, time.Now())
	recovery, err := svc.ConfirmTOTP(ctx, u.ID, code)
	if err != nil || len(recovery) != 10 {
		t.Fatalf("confirm: %v %v", recovery, err)
	}
	// The same code cannot be replayed.
	var mfa *auth.MFARequired
	_, err = svc.Login(ctx, "a@example.org", "tulips in the garden", "")
	if !errors.As(err, &mfa) {
		t.Fatalf("expected MFARequired, got %v", err)
	}
	if _, err := svc.VerifyMFA(ctx, mfa.Challenge, code, ""); !errors.Is(err, auth.ErrInvalidCode) {
		t.Errorf("replayed code: %v", err)
	}
	// A recovery code works once.
	_, err = svc.Login(ctx, "a@example.org", "tulips in the garden", "")
	errors.As(err, &mfa)
	if _, err := svc.VerifyMFA(ctx, mfa.Challenge, recovery[0], ""); err != nil {
		t.Fatalf("recovery code: %v", err)
	}
	_, err = svc.Login(ctx, "a@example.org", "tulips in the garden", "")
	errors.As(err, &mfa)
	if _, err := svc.VerifyMFA(ctx, mfa.Challenge, recovery[0], ""); !errors.Is(err, auth.ErrInvalidCode) {
		t.Errorf("recovery code reuse: %v", err)
	}
	// Brute force: the challenge dies after a few wrong tries.
	_, err = svc.Login(ctx, "a@example.org", "tulips in the garden", "")
	errors.As(err, &mfa)
	var last error
	for range 7 {
		_, last = svc.VerifyMFA(ctx, mfa.Challenge, "111111", "")
	}
	if !errors.Is(last, auth.ErrTooManyTries) && !errors.Is(last, auth.ErrInvalidToken) {
		t.Errorf("brute force not stopped: %v", last)
	}
	if err := svc.DisableTOTP(ctx, u.ID, "wrong password!!"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("disable with wrong password: %v", err)
	}
	if err := svc.DisableTOTP(ctx, u.ID, "tulips in the garden"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Login(ctx, "a@example.org", "tulips in the garden", ""); err != nil {
		t.Errorf("login after disable: %v", err)
	}
}

func TestPasswordReset(t *testing.T) {
	ctx := context.Background()
	svc, _ := auth.NewService(dbtest.Pool(t), auth.RegistrationOpen)
	if err := svc.RequestReset(ctx, "a@example.org", "https://x"); !errors.Is(err, auth.ErrNoMailer) {
		t.Errorf("no mailer: %v", err)
	}
	m := &captureMailer{}
	svc.SetMailer(m)
	u, _ := svc.Register(ctx, "a@example.org", "tulips in the garden", "A")
	sess, _ := svc.Login(ctx, "a@example.org", "tulips in the garden", "")
	if err := svc.RequestReset(ctx, "nobody@example.org", "https://x"); err != nil || m.to != "" {
		t.Errorf("unknown address must be silent: %v %q", err, m.to)
	}
	if err := svc.RequestReset(ctx, "a@example.org", "https://x"); err != nil || m.to != "a@example.org" {
		t.Fatalf("request: %v", err)
	}
	token := m.body[strings.Index(m.body, "token=")+6:]
	token = strings.Fields(token)[0]
	if err := svc.Reset(ctx, token, "short"); err == nil {
		t.Error("weak password accepted")
	}
	if err := svc.Reset(ctx, token, "a brand new passphrase"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Reset(ctx, token, "another new passphrase"); !errors.Is(err, auth.ErrInvalidToken) {
		t.Errorf("token reuse: %v", err)
	}
	if _, err := svc.Lookup(ctx, sess.Token); err == nil {
		t.Error("sessions must end after a reset")
	}
	if _, err := svc.Login(ctx, "a@example.org", "a brand new passphrase", ""); err != nil {
		t.Errorf("login with new password: %v", err)
	}
	_ = u
}
