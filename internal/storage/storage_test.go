package storage

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestLocal(t *testing.T) {
	ctx := context.Background()
	l, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := strings.Repeat("ab", 32)
	if err := l.Put(ctx, key, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := l.Put(ctx, key, []byte("other")); err != nil { // content addressed: no overwrite
		t.Fatal(err)
	}
	r, err := l.Open(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r)
	_ = r.Close()
	if string(b) != "hello" {
		t.Errorf("content %q", b)
	}
	if err := l.Put(ctx, key+".thumb", []byte("t")); err != nil {
		t.Errorf("thumb variant: %v", err)
	}
	if err := l.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Open(ctx, key); !errors.Is(err, ErrNotFound) {
		t.Errorf("after delete: %v", err)
	}
}

func TestLocalRejectsBadKeys(t *testing.T) {
	l, _ := NewLocal(t.TempDir())
	for _, k := range []string{"", "../../etc/passwd", "abc", strings.Repeat("A", 64), strings.Repeat("a", 64) + "/x", strings.Repeat("a", 64) + ".jpg"} {
		if err := l.Put(context.Background(), k, []byte("x")); err == nil {
			t.Errorf("key %q accepted", k)
		}
		if _, err := l.Open(context.Background(), k); err == nil {
			t.Errorf("key %q opened", k)
		}
	}
}

func TestCopyBetweenStores(t *testing.T) {
	ctx := context.Background()
	a, _ := NewLocal(t.TempDir())
	b, _ := NewLocal(t.TempDir())
	k1, k2 := strings.Repeat("a1", 32), strings.Repeat("b2", 32)+".thumb"
	_ = a.Put(ctx, k1, []byte("one"))
	_ = a.Put(ctx, k2, []byte("two"))
	n, err := Copy(ctx, a, b)
	if err != nil || n != 2 {
		t.Fatalf("copy: %d %v", n, err)
	}
	r, err := b.Open(ctx, k2)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(r)
	_ = r.Close()
	if string(got) != "two" {
		t.Errorf("content %q", got)
	}
}
