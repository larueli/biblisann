package oauth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/larueli/biblisann/internal/auth"
	"github.com/larueli/biblisann/internal/dbtest"
	"github.com/larueli/biblisann/internal/oauth"
)

func TestRegistrationReclaimsOnlyAbandonedClients(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)
	a, err := auth.NewService(pool, auth.RegistrationOpen)
	if err != nil {
		t.Fatal(err)
	}
	u, err := a.Register(ctx, "user@example.org", "tulips in the garden", "Reader")
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO oauth_clients (id, name, redirect_uris, created_at)
		SELECT 'client-' || n, 'client', ARRAY['https://example.org/cb'],
		CASE WHEN n <= 2 THEN now() - interval '2 days' ELSE now() END
		FROM generate_series(1, 2000) n`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO oauth_codes (code_hash, client_id, user_id, redirect_uri, code_challenge, scopes, resource, expires_at)
		VALUES ('\x01', 'client-1', $1, 'https://example.org/cb', 'challenge', ARRAY['read'], 'resource', now() + interval '1 hour')`, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	svc := oauth.New(pool, "https://example.org")
	if _, err := svc.RegisterClient(ctx, "new", []string{"https://example.org/cb"}); err != nil {
		t.Fatal(err)
	}
	var active, abandoned bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM oauth_clients WHERE id='client-1'),
		EXISTS (SELECT 1 FROM oauth_clients WHERE id='client-2')`).Scan(&active, &abandoned); err != nil {
		t.Fatal(err)
	}
	if !active || abandoned {
		t.Fatalf("active = %v, abandoned = %v", active, abandoned)
	}
	if _, err := svc.RegisterClient(ctx, "another", []string{"https://example.org/cb"}); !errors.Is(err, oauth.ErrTooManyClients) {
		t.Fatalf("full with no eligible clients: %v", err)
	}
}
