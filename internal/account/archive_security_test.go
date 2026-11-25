package account

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/larueli/biblisann/internal/authz"
)

func TestImportRequiresWriteScope(t *testing.T) {
	s := NewService(nil, nil, nil, nil, nil, nil)
	ctx := authz.With(context.Background(), &authz.Principal{Scopes: []string{"read"}})
	for _, run := range []func(context.Context) error{
		func(ctx context.Context) error { _, err := s.ImportLibrary(ctx, bytes.NewReader(nil)); return err },
		func(ctx context.Context) error { _, err := s.ImportAccount(ctx, bytes.NewReader(nil)); return err },
	} {
		if err := run(ctx); !errors.Is(err, authz.ErrForbidden) {
			t.Fatalf("read-only import: %v", err)
		}
	}
}

func TestValidateLibraryEntry(t *testing.T) {
	for _, tc := range []struct {
		name string
		rows []string
		ok   bool
	}{
		{"one", []string{"library", "edition"}, true},
		{"none", []string{"edition"}, false},
		{"multiple", []string{"library", "library"}, false},
		{"out of order", []string{"edition", "library"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWriter(FormatLibrary)
			for _, row := range tc.rows {
				if err := w.add(row, map[string]string{}); err != nil {
					t.Fatal(err)
				}
			}
			err := validateLibraryEntry(w.buf.Bytes())
			if (err == nil) != tc.ok {
				t.Fatalf("validation: %v", err)
			}
		})
	}
}

func TestValidateAccountArchiveRejectsMalformedLibrary(t *testing.T) {
	account := newWriter(FormatAccount)
	if err := account.add("library", libraryRefRow{File: "libraries/1.ndjson"}); err != nil {
		t.Fatal(err)
	}
	library := newWriter(FormatLibrary)
	if err := library.add("library", libraryRow{Name: "One"}); err != nil {
		t.Fatal(err)
	}
	if err := library.add("library", libraryRow{Name: "Two"}); err != nil {
		t.Fatal(err)
	}
	a := &archive{entries: map[string][]byte{"libraries/1.ndjson": library.buf.Bytes()}}
	if err := validateAccountArchive(a, account.buf.Bytes()); !errors.Is(err, ErrArchive) {
		t.Fatalf("malformed library: %v", err)
	}
}
