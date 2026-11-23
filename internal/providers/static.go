package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
)

// Static serves metadata from a local JSON file: {"<isbn13>": {"title": "...", ...}}. It makes no
// network request, which suits air-gapped instances, curated data and tests.
type Static struct{ records map[string]Record }

// LoadStatic reads the file (at most 16 MiB).
func LoadStatic(path string) (*Static, error) {
	f, err := os.Open(path) //nolint:gosec // operator-provided configuration path
	if err != nil {
		return nil, fmt.Errorf("static provider: %w", err)
	}
	defer func() { _ = f.Close() }()
	var m map[string]Record
	if err := json.NewDecoder(&limitedReader{f: f, n: 16 << 20}).Decode(&m); err != nil {
		return nil, fmt.Errorf("static provider: invalid JSON: %w", err)
	}
	return &Static{records: m}, nil
}

type limitedReader struct {
	f interface{ Read([]byte) (int, error) }
	n int
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.n <= 0 {
		return 0, fmt.Errorf("file too large")
	}
	if len(p) > l.n {
		p = p[:l.n]
	}
	n, err := l.f.Read(p)
	l.n -= n
	return n, err
}

// Name implements Provider.
func (*Static) Name() string { return "static" }

// Lookup implements Provider.
func (s *Static) Lookup(_ context.Context, isbn13 string) (*Record, error) {
	r, ok := s.records[isbn13]
	if !ok || r.Title == "" {
		return nil, nil
	}
	r.Title = clean(r.Title)
	return &r, nil
}
