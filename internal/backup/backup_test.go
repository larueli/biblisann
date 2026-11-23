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
	if err := backup.Restore(ctx, dst, dstCovers, bytes.NewReader(buf.Bytes())); err != nil {
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
	if err := backup.Restore(ctx, dst, dstCovers, bytes.NewReader(buf.Bytes())); !errors.Is(err, backup.ErrNotEmpty) {
		t.Errorf("non-empty target: %v", err)
	}
	if err := backup.Restore(ctx, dbtest.Pool(t), dstCovers, strings.NewReader("not a tar")); !errors.Is(err, backup.ErrFormat) {
		t.Errorf("garbage: %v", err)
	}
}

func tinyPNG() []byte {
	var b bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	_ = png.Encode(&b, img)
	return b.Bytes()
}
