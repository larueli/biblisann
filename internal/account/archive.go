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

// archive is a parsed archive: NDJSON entries by name, and cover bytes by SHA.
type archive struct {
	entries map[string][]byte
	covers  map[string][]byte
}

func readArchive(r io.Reader) (*archive, error) {
	a := &archive{entries: map[string][]byte{}, covers: map[string][]byte{}}
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
			a.covers[strings.TrimPrefix(h.Name, "covers/")] = data
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
