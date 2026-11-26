package covercheck_test

import (
	"context"
	"encoding/hex"
	"io"
	"log/slog"
	"testing"

	"github.com/larueli/biblisann/internal/covercheck"
	"github.com/larueli/biblisann/internal/dbtest"
	"github.com/larueli/biblisann/internal/storage"
)

func TestRun(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	// sums are invented: a complete cover, one without thumbnail, one without any file, one too new to judge.
	sums := [][]byte{make32(1), make32(2), make32(3), make32(4)}
	for _, s := range sums {
		if _, err := pool.Exec(ctx, "INSERT INTO covers (sha256, width, height) VALUES ($1, 10, 10)", s); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, "UPDATE covers SET created_at = now() - interval '2 hours' WHERE sha256 <> $1", sums[3]); err != nil {
		t.Fatal(err)
	}
	put := func(key string) {
		if err := store.Put(ctx, key, []byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	k1, k2 := hex.EncodeToString(sums[0]), hex.EncodeToString(sums[1])
	put(k1)
	put(k1 + ".thumb")
	put(k2)

	r, err := covercheck.Run(ctx, pool, store, log)
	if err != nil {
		t.Fatal(err)
	}
	want := covercheck.Report{Checked: 3, MissingFull: 1, MissingThumb: 2}
	if r != want || !r.Missing() {
		t.Errorf("report = %s, want %+v", r, want)
	}

	// repairing the files makes the next run clean
	put(k2 + ".thumb")
	put(hex.EncodeToString(sums[2]))
	put(hex.EncodeToString(sums[2]) + ".thumb")
	if r, _ = covercheck.Run(ctx, pool, store, log); r.Missing() || r.Checked != 3 {
		t.Errorf("after repair: %s", r)
	}
}

func make32(b byte) []byte {
	out := make([]byte, 32)
	out[0] = b
	return out
}
