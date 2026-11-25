package backup_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/larueli/biblisann/internal/auth"
	"github.com/larueli/biblisann/internal/authz"
	"github.com/larueli/biblisann/internal/backup"
	"github.com/larueli/biblisann/internal/catalog"
	"github.com/larueli/biblisann/internal/dbtest"
	"github.com/larueli/biblisann/internal/library"
	"github.com/larueli/biblisann/internal/secretbox"
	"github.com/larueli/biblisann/internal/storage"
)

func TestRoundTrip(t *testing.T) {
	ctx := context.Background()
	src := dbtest.Pool(t)
	covers, _ := storage.NewLocal(t.TempDir())
	a, _ := auth.NewService(src, auth.RegistrationOpen)
	u, err := a.Register(ctx, "me@example.org", "tulips in the garden", "Me")
	if err != nil {
		t.Fatal(err)
	}
	uctx := authz.With(ctx, &authz.Principal{UserID: u.ID, Via: "session"})
	lib := library.NewService(src)
	cat := catalog.NewService(src, catalog.Config{Covers: covers, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	l, _ := lib.CreateLibrary(uctx, "Home")
	ed, _, err := cat.Create(ctx, u.ID, catalog.EditionInput{ISBN: "9782070612758", Title: "Le Petit Prince", Authors: []string{"Saint-Exupéry"}}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lib.AddInstance(uctx, l.ID, library.InstanceInput{EditionID: ed.ID, Notes: "signed"}, ""); err != nil {
		t.Fatal(err)
	}
	// a stored cover referenced by a row
	png := tinyPNG()
	if id := cat.StoreCover(ctx, png); id == nil {
		t.Fatal("cover not stored")
	}

	var buf bytes.Buffer
	if err := backup.Export(ctx, src, covers, &buf, "test"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "tulips in the garden") {
		t.Error("the backup must not contain plaintext passwords")
	}

	dst := dbtest.Pool(t)
	dstCovers, _ := storage.NewLocal(t.TempDir())
	if err := backup.Restore(ctx, dst, dstCovers, bytes.NewReader(buf.Bytes()), nil); err != nil {
		t.Fatal(err)
	}
	// Accounts work, data is there, the change log lets offline clients resync, covers came along.
	a2, _ := auth.NewService(dst, auth.RegistrationClosed)
	if _, err := a2.Login(ctx, "me@example.org", "tulips in the garden", ""); err != nil {
		t.Errorf("login after restore: %v", err)
	}
	lib2 := library.NewService(dst)
	page, err := lib2.ListInstances(uctx, l.ID, "", 10)
	if err != nil || len(page.Items) != 1 || page.Items[0].Title != "Le Petit Prince" || page.Items[0].Notes != "signed" {
		t.Fatalf("restored instances: %+v %v", page, err)
	}
	if idx, _ := lib2.SyncIndex(uctx, l.ID, 0, 100); len(idx.Entries) != 1 {
		t.Errorf("sync after restore: %+v", idx)
	}
	var n int
	if err := dst.QueryRow(ctx, "SELECT count(*) FROM covers").Scan(&n); err != nil || n != 1 {
		t.Errorf("covers rows: %d %v", n, err)
	}

	// Restoring into a non-empty instance is refused, and garbage is rejected.
	if err := backup.Restore(ctx, dst, dstCovers, bytes.NewReader(buf.Bytes()), nil); !errors.Is(err, backup.ErrNotEmpty) {
		t.Errorf("non-empty target: %v", err)
	}
	if err := backup.Restore(ctx, dbtest.Pool(t), dstCovers, strings.NewReader("not a tar"), nil); !errors.Is(err, backup.ErrFormat) {
		t.Errorf("garbage: %v", err)
	}
}

func TestRestorePreservesFactorsAndRequiresOriginalKey(t *testing.T) {
	ctx := context.Background()
	src := dbtest.Pool(t)
	covers, _ := storage.NewLocal(t.TempDir())
	a, _ := auth.NewService(src, auth.RegistrationOpen)
	u, err := a.Register(ctx, "user@example.org", "tulips in the garden", "Reader")
	if err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{1}, 32)
	sealed, err := secretbox.Seal(key, []byte("test authenticator secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := src.Exec(ctx, "INSERT INTO user_totp (user_id, secret_enc, confirmed_at) VALUES ($1, $2, now())", u.ID, sealed); err != nil {
		t.Fatal(err)
	}
	if _, err := src.Exec(ctx, "INSERT INTO identities (issuer, subject, user_id) VALUES ($1, $2, $3)", "https://example.org", "subject", u.ID); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := backup.Export(ctx, src, covers, &archive, "test"); err != nil {
		t.Fatal(err)
	}
	dst := dbtest.Pool(t)
	for _, wrong := range [][]byte{nil, bytes.Repeat([]byte{2}, 32)} {
		if err := backup.Restore(ctx, dst, covers, bytes.NewReader(archive.Bytes()), wrong); err == nil {
			t.Fatal("restore accepted an incorrect secret key")
		}
		var users int
		if err := dst.QueryRow(ctx, "SELECT count(*) FROM users").Scan(&users); err != nil || users != 0 {
			t.Fatalf("failed restore committed users: %d, %v", users, err)
		}
	}
	if err := backup.Restore(ctx, dst, covers, bytes.NewReader(archive.Bytes()), key); err != nil {
		t.Fatal(err)
	}
	var factors, identities int
	if err := dst.QueryRow(ctx, "SELECT (SELECT count(*) FROM user_totp), (SELECT count(*) FROM identities)").Scan(&factors, &identities); err != nil || factors != 1 || identities != 1 {
		t.Fatalf("restored factors=%d identities=%d: %v", factors, identities, err)
	}
}

func tinyPNG() []byte {
	var b bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	_ = png.Encode(&b, img)
	return b.Bytes()
}
