package api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/larueli/biblisann/internal/auth"
	"github.com/larueli/biblisann/internal/authz"
	"github.com/larueli/biblisann/internal/httpmw"
)

func TestBackupFailureDoesNotReturnSuccessfulArchive(t *testing.T) {
	a, err := auth.NewService(nil, auth.RegistrationOpen)
	if err != nil {
		t.Fatal(err)
	}
	for _, partial := range []bool{false, true} {
		t.Run(map[bool]string{false: "before write", true: "after write"}[partial], func(t *testing.T) {
			log := slog.New(slog.NewTextHandler(io.Discard, nil))
			s := &Server{Deps: Deps{Auth: a, Log: log, Backup: func(_ context.Context, w io.Writer) error {
				if partial {
					// Exceed net/http's write buffer so the client sees headers before abort.
					_, _ = w.Write(bytes.Repeat([]byte("x"), 64<<10))
				}
				return errors.New("backup failed")
			}}}
			until := time.Now().Add(time.Minute)
			h := httpmw.Recover(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ctx := authz.With(r.Context(), &authz.Principal{IsAdmin: true, Via: "session"})
				ctx = context.WithValue(ctx, sessionKey{}, &auth.Session{StepUpUntil: &until})
				s.serveBackup(w, r.WithContext(ctx))
			}))
			srv := httptest.NewServer(h)
			defer srv.Close()
			resp, err := http.Post(srv.URL, "", nil) //nolint:noctx // test server only
			if partial {
				if err != nil {
					t.Fatalf("headers should have been sent: %v", err)
				}
				defer func() { _ = resp.Body.Close() }()
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("status %d, want 200 before abort", resp.StatusCode)
				}
				if _, err := io.ReadAll(resp.Body); err == nil {
					t.Fatal("partial backup ended cleanly")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusInternalServerError {
				t.Fatalf("status %d, want 500", resp.StatusCode)
			}
		})
	}
}
