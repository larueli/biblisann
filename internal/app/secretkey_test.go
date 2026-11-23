package app_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"testing"

	"github.com/larueli/biblisann/internal/app"
	"github.com/larueli/biblisann/internal/auth"
	"github.com/larueli/biblisann/internal/config"
	"github.com/larueli/biblisann/internal/dbtest"
	"github.com/larueli/biblisann/internal/secretbox"
)

func newKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	return k
}

func TestCheckSecretKey(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)
	keyA, keyB := newKey(t), newKey(t)
	cfg := func(k []byte) config.Config {
		if k == nil {
			return config.Config{}
		}
		return config.Config{SecretKey: config.Secret(base64.StdEncoding.EncodeToString(k))}
	}

	// nothing sealed yet: any configuration starts
	for _, k := range [][]byte{nil, keyA} {
		if err := app.CheckSecretKey(ctx, cfg(k), pool); err != nil {
			t.Fatalf("empty database: %v", err)
		}
	}
	a, _ := auth.NewService(pool, auth.RegistrationOpen)
	u, err := a.Register(ctx, "a@example.org", "tulips in the garden", "A")
	if err != nil {
		t.Fatal(err)
	}
	sealed, _ := secretbox.Seal(keyA, []byte("totp secret"))
	if _, err := pool.Exec(ctx, "INSERT INTO user_totp (user_id, secret_enc, confirmed_at) VALUES ($1, $2, now())", u.ID, sealed); err != nil {
		t.Fatal(err)
	}

	if err := app.CheckSecretKey(ctx, cfg(keyA), pool); err != nil {
		t.Errorf("right key: %v", err)
	}
	if err := app.CheckSecretKey(ctx, cfg(nil), pool); !errors.Is(err, app.ErrSecretKey) {
		t.Errorf("missing key must stop the server: %v", err)
	}
	if err := app.CheckSecretKey(ctx, cfg(keyB), pool); !errors.Is(err, app.ErrSecretKey) {
		t.Errorf("wrong key must stop the server: %v", err)
	}
	if err := app.CheckSecretKey(ctx, config.Config{SecretKey: "not base64 of 32 bytes"}, pool); !errors.Is(err, app.ErrSecretKey) {
		t.Errorf("malformed key: %v", err)
	}
}
