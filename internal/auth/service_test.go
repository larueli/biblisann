package auth_test

import (
	"context"
	"errors"
	"sync"
	"testing"

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
