// Package account moves a person's data in and out of Biblisann: portable exports of a library
// or of a whole account, imports of those exports, and account deletion.
//
// Archives are tar files holding NDJSON (one {"t": type, "r": row} object per line) and the
// cover images. They do not reference database IDs: rows point at each other through local keys
// and at books through ISBN and title, so an export from one instance imports into another.
// An export contains personal data (notes, grades, loans) but no credentials or secrets.
package account

import (
	"archive/tar"
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Archive formats and limits.
const (
	FormatLibrary = "biblisann-library"
	FormatAccount = "biblisann-account"
	Version       = 1

	maxArchiveBytes = 64 << 20
	maxRows         = 200_000
	maxLine         = 4 << 20
	maxCover        = 8 << 20
)

// ErrArchive is returned for archives that cannot be read or are too large.
var ErrArchive = errors.New("account: unsupported or corrupt archive")

type header struct {
	Format     string    `json:"format"`
	Version    int       `json:"version"`
	ExportedAt time.Time `json:"exported_at"`
}

type line struct {
	T string          `json:"t"`
	R json.RawMessage `json:"r"`
}

// Row types. Keys are local to the archive.
type (
	libraryRow struct {
		Name     string          `json:"name"`
		Settings json.RawMessage `json:"settings,omitempty"`
	}
	editionRow struct {
		Key            string   `json:"key"`
		ISBN13         string   `json:"isbn13,omitempty"`
		Title          string   `json:"title"`
		Subtitle       string   `json:"subtitle,omitempty"`
		Authors        []string `json:"authors"`
		Publisher      string   `json:"publisher,omitempty"`
		Year           int      `json:"year,omitempty"`
		Language       string   `json:"language,omitempty"`
		Pages          int      `json:"pages,omitempty"`
		Description    string   `json:"description,omitempty"`
		SeriesName     string   `json:"series_name,omitempty"`
		SeriesPosition *float64 `json:"series_position,omitempty"`
		CoverSHA       string   `json:"cover,omitempty"`
	}
	placeRow struct {
		Key      string `json:"key"`
		Parent   string `json:"parent,omitempty"`
		Name     string `json:"name"`
		Kind     string `json:"kind"`
		Position int    `json:"position"`
	}
	instanceRow struct {
		Key        string `json:"key"`
		Edition    string `json:"edition"`
		Place      string `json:"place,omitempty"`
		Kind       string `json:"kind"`
		Condition  string `json:"condition"`
		Notes      string `json:"notes,omitempty"`
		AcquiredOn string `json:"acquired_on,omitempty"`
	}
	listRow struct {
		Key  string `json:"key"`
		Name string `json:"name"`
		Kind string `json:"kind,omitempty"` // "wishlist" for the personal default list
	}
	listItemRow struct {
		List           string   `json:"list"`
		Edition        string   `json:"edition,omitempty"`
		Title          string   `json:"title"`
		Authors        []string `json:"authors"`
		ISBN13         string   `json:"isbn13,omitempty"`
		SeriesName     string   `json:"series_name,omitempty"`
		SeriesPosition *float64 `json:"series_position,omitempty"`
		Note           string   `json:"note,omitempty"`
	}
	loanRow struct {
		Instance     string `json:"instance,omitempty"`
		Direction    string `json:"direction"`
		Counterparty string `json:"counterparty"`
		Title        string `json:"title"`
		LentOn       string `json:"lent_on"`
		DueOn        string `json:"due_on,omitempty"`
		ReturnedOn   string `json:"returned_on,omitempty"`
		Notes        string `json:"notes,omitempty"`
	}
	profileRow struct {
		Email       string          `json:"email"`
		DisplayName string          `json:"display_name"`
		Locale      string          `json:"locale"`
		Settings    json.RawMessage `json:"settings,omitempty"`
	}
	contactRow struct {
		Name  string `json:"name"`
		Email string `json:"email,omitempty"`
		Notes string `json:"notes,omitempty"`
	}
	// bookRef identifies a book in personal data without any ID.
	bookRef struct {
		ISBN13  string   `json:"isbn13,omitempty"`
		Title   string   `json:"title"`
		Authors []string `json:"authors"`
	}
	readingRow struct {
		Book       bookRef `json:"book"`
		Status     string  `json:"status"`
		StartedOn  string  `json:"started_on,omitempty"`
		FinishedOn string  `json:"finished_on,omitempty"`
	}
	gradeRow struct {
		Book    bookRef `json:"book"`
		Grade   *int    `json:"grade,omitempty"`
		Comment string  `json:"comment,omitempty"`
	}
	libraryRefRow struct {
		File string `json:"file"`
		Name string `json:"name"`
		Role string `json:"role"`
	}
)

func day(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02")
}

func parseDay(s string) *time.Time {
	if s == "" {
		return nil
	}
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil
	}
	return &d
}

// writer builds one NDJSON entry in memory (metadata only; covers are streamed separately).
type writer struct {
	buf bytes.Buffer
	enc *json.Encoder
	n   int
}

func newWriter(format string) *writer {
	w := &writer{}
	w.enc = json.NewEncoder(&w.buf)
	_ = w.enc.Encode(header{format, Version, time.Now().UTC()})
	return w
}

func (w *writer) add(t string, row any) error {
	raw, err := json.Marshal(row)
	if err != nil {
		return err
	}
	w.n++
	return w.enc.Encode(line{t, raw})
}

func putEntry(tw *tar.Writer, name string, data []byte) error {
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data)), ModTime: time.Now()}); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}

// archive is a parsed archive: NDJSON entries by name (held in memory, they are metadata) and
// covers by SHA (spooled to a temporary directory on disk: they are the bulk of an archive, and
// two imports may run at once). Close removes the directory.
type archive struct {
	entries map[string][]byte
	covers  map[string]string // SHA -> temporary file
	dir     string
}

// Close deletes the spooled covers.
func (a *archive) Close() {
	if a != nil && a.dir != "" {
		_ = os.RemoveAll(a.dir)
	}
}

// cover returns the bytes of a spooled cover (at most maxCover).
func (a *archive) cover(sha string) ([]byte, bool) {
	path, ok := a.covers[sha]
	if !ok {
		return nil, false
	}
	data, err := os.ReadFile(path) //nolint:gosec // path was built from a counter inside our own temporary directory
	return data, err == nil
}

func readArchive(r io.Reader) (_ *archive, err error) {
	a := &archive{entries: map[string][]byte{}, covers: map[string]string{}}
	defer func() {
		if err != nil {
			a.Close()
		}
	}()
	tr := tar.NewReader(io.LimitReader(r, maxArchiveBytes+1))
	total := 0
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrArchive, err)
		}
		limit := int64(maxArchiveBytes)
		if strings.HasPrefix(h.Name, "covers/") {
			limit = maxCover
		}
		data, err := io.ReadAll(io.LimitReader(tr, limit+1))
		if err != nil || int64(len(data)) > limit {
			return nil, ErrArchive
		}
		if total += len(data); total > maxArchiveBytes {
			return nil, ErrArchive
		}
		switch {
		case strings.HasPrefix(h.Name, "covers/"):
			sha := strings.TrimPrefix(h.Name, "covers/")
			if _, dup := a.covers[sha]; dup {
				continue // a repeated name must not replace the first cover
			}
			if a.dir == "" {
				if a.dir, err = os.MkdirTemp("", "biblisann-import-"); err != nil {
					return nil, fmt.Errorf("account: spool covers: %w", err)
				}
			}
			// The name inside the archive is untrusted: the file name is a counter, never the entry name.
			path := filepath.Join(a.dir, strconv.Itoa(len(a.covers)))
			if err := os.WriteFile(path, data, 0o600); err != nil {
				return nil, fmt.Errorf("account: spool covers: %w", err)
			}
			a.covers[sha] = path
		case strings.HasSuffix(h.Name, ".ndjson"):
			a.entries[h.Name] = data
		}
	}
	return a, nil
}

// each decodes the rows of an entry after checking its header format.
func each(data []byte, format string, fn func(t string, raw json.RawMessage) error) error {
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 1<<20), maxLine)
	if !sc.Scan() {
		return ErrArchive
	}
	var h header
	if err := json.Unmarshal(sc.Bytes(), &h); err != nil || h.Format != format || h.Version < 1 || h.Version > Version {
		return ErrArchive
	}
	n := 0
	for sc.Scan() {
		if n++; n > maxRows {
			return fmt.Errorf("%w: too many rows", ErrArchive)
		}
		var l line
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			return ErrArchive
		}
		if err := fn(l.T, l.R); err != nil {
			return err
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("%w: %w", ErrArchive, err)
	}
	return nil
}

// validateLibraryEntry requires one library record, before any dependent records.
// Validate before importing so a malformed archive cannot create multiple libraries.
func validateLibraryEntry(data []byte) error {
	count := 0
	err := each(data, FormatLibrary, func(t string, _ json.RawMessage) error {
		if t == "library" {
			count++
			if count > 1 {
				return fmt.Errorf("%w: multiple library records", ErrArchive)
			}
		} else if count == 0 {
			return fmt.Errorf("%w: library record must come first", ErrArchive)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("%w: missing library record", ErrArchive)
	}
	return nil
}

// validateAccountArchive checks all referenced library entries before any rows are applied.
func validateAccountArchive(a *archive, data []byte) error {
	files := map[string]bool{}
	rows := 0
	err := each(data, FormatAccount, func(t string, raw json.RawMessage) error {
		rows++
		if t == "library" {
			var ref libraryRefRow
			if json.Unmarshal(raw, &ref) != nil || !strings.HasPrefix(ref.File, "libraries/") {
				return ErrArchive
			}
			files[ref.File] = true
			if len(files) > maxImportLibraries {
				return fmt.Errorf("%w: too many libraries", ErrArchive)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for file := range files {
		entry, ok := a.entries[file]
		if !ok {
			return fmt.Errorf("%w: missing library entry", ErrArchive)
		}
		if err := validateLibraryEntry(entry); err != nil {
			return err
		}
		if err := each(entry, FormatLibrary, func(_ string, _ json.RawMessage) error {
			rows++
			if rows > maxRows {
				return fmt.Errorf("%w: too many rows", ErrArchive)
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}
