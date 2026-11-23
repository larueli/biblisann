// Package storage stores cover files behind an interface so a local directory can be replaced
// by an S3-compatible bucket without touching callers.
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

// ErrNotFound is returned when a key does not exist.
var ErrNotFound = errors.New("storage: not found")

// Store keeps immutable blobs addressed by key.
type Store interface {
	Put(ctx context.Context, key string, data []byte) error
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}

// keyRe is the only key shape allowed: a SHA-256 hex digest with an optional variant suffix.
// Callers never pass user-controlled paths, and Local still refuses anything else.
var keyRe = regexp.MustCompile(`^[0-9a-f]{64}(\.thumb)?$`)

// Local stores blobs as files in a directory.
type Local struct{ dir string }

// NewLocal creates the directory (mode 0750) when missing.
func NewLocal(dir string) (*Local, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("storage: create %s: %w", dir, err)
	}
	return &Local{dir: dir}, nil
}

func (l *Local) path(key string) (string, error) {
	if !keyRe.MatchString(key) {
		return "", fmt.Errorf("storage: invalid key")
	}
	return filepath.Join(l.dir, key[:2], key), nil
}

// Put writes atomically; writing an existing key is a no-op because content is addressed.
func (l *Local) Put(_ context.Context, key string, data []byte) error {
	p, err := l.path(key)
	if err != nil {
		return err
	}
	if _, err := os.Stat(p); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return fmt.Errorf("storage: mkdir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".tmp-*")
	if err != nil {
		return fmt.Errorf("storage: temp file: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("storage: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("storage: close: %w", err)
	}
	if err := os.Rename(tmp.Name(), p); err != nil {
		return fmt.Errorf("storage: rename: %w", err)
	}
	return nil
}

// Open returns a reader or ErrNotFound.
func (l *Local) Open(_ context.Context, key string) (io.ReadCloser, error) {
	p, err := l.path(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p) //nolint:gosec // p is built from a validated key
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: open: %w", err)
	}
	return f, nil
}

// Delete removes a blob; a missing key is not an error.
func (l *Local) Delete(_ context.Context, key string) error {
	p, err := l.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("storage: delete: %w", err)
	}
	return nil
}

// Lister is implemented by stores that can enumerate their keys.
type Lister interface {
	List(ctx context.Context) ([]string, error)
}

// List returns every blob key in the directory.
func (l *Local) List(_ context.Context) ([]string, error) {
	var out []string
	err := filepath.WalkDir(l.dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && keyRe.MatchString(d.Name()) {
			out = append(out, d.Name())
		}
		return nil
	})
	return out, err
}

// Copy copies every blob of src into dst. It returns the number of blobs copied. Blobs already
// present in dst are overwritten with identical content (content-addressed), so the operation can
// be repeated after an interruption.
func Copy(ctx context.Context, src Store, dst Store) (int, error) {
	l, ok := src.(Lister)
	if !ok {
		return 0, errors.New("storage: source cannot list its keys")
	}
	keys, err := l.List(ctx)
	if err != nil {
		return 0, err
	}
	for i, k := range keys {
		rc, err := src.Open(ctx, k)
		if err != nil {
			return i, fmt.Errorf("storage: read %s: %w", k, err)
		}
		data, err := io.ReadAll(io.LimitReader(rc, 32<<20))
		_ = rc.Close()
		if err != nil {
			return i, err
		}
		if err := dst.Put(ctx, k, data); err != nil {
			return i, err
		}
	}
	return len(keys), nil
}
