// Package importer loads books from other services. It never queries metadata providers: the
// import uses the data in the file, so a large import makes no outbound request.
package importer

import (
	"bufio"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/larueli/biblisann/internal/authz"
	"github.com/larueli/biblisann/internal/catalog"
	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
	"github.com/larueli/biblisann/internal/library"
	"github.com/larueli/biblisann/internal/lists"
	"github.com/larueli/biblisann/internal/reading"
)

// Limits protect the server from oversized files.
const (
	MaxRows = 20000
)

// ErrFormat is returned for files that are not the expected export, or are too large.
var ErrFormat = errors.New("importer: unsupported file")

// Report summarizes an import.
type Report struct {
	Rows            int
	EditionsCreated int
	InstancesAdded  int
	ReadingStates   int
	Grades          int
	WishlistAdded   int
	Skipped         int
	Errors          []RowError
}

// RowError describes a row that could not be imported.
type RowError struct {
	Row     int
	Message string
}

// Importer imports into the caller's account.
type Importer struct {
	pool    *pgxpool.Pool
	q       *dbsqlc.Queries
	catalog *catalog.Service
	library *library.Service
	reading *reading.Service
	lists   *lists.Service
}

// New creates an Importer.
func New(pool *pgxpool.Pool, c *catalog.Service, l *library.Service, r *reading.Service, ls *lists.Service) *Importer {
	return &Importer{pool: pool, q: dbsqlc.New(pool), catalog: c, library: l, reading: r, lists: ls}
}

// unwrapExcel removes the ="..." wrapper Goodreads puts around ISBNs.
func unwrapExcel(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "=")
	return strings.Trim(s, `"`)
}

// bookRow is one book in the neutral form every source is mapped to.
type bookRow struct {
	Title, Publisher, ISBN string
	Authors                []string
	Year, Pages            int
	Status                 string // to_read, reading, read, abandoned or ""
	FinishedOn             *time.Time
	Grade                  *int // 1..10
	Review                 string
	Owned                  bool
}

// mapper turns one CSV record (accessed by column name) into a bookRow; ok=false skips the row.
type mapper func(get func(name string) string) (row bookRow, ok bool)

func parseDate(layouts []string, s string) *time.Time {
	for _, l := range layouts {
		if d, err := time.Parse(l, strings.TrimSpace(s)); err == nil {
			return &d
		}
	}
	return nil
}

func splitAuthors(s string, sep string) []string {
	var out []string
	for _, a := range strings.Split(s, sep) {
		if a = strings.TrimSpace(a); a != "" {
			out = append(out, a)
		}
	}
	return out
}

// starsToGrade maps a 0..5 star rating (possibly fractional) to the 1..10 scale.
func starsToGrade(stars float64) *int {
	g := int(stars*2 + 0.5)
	if g < 1 {
		return nil
	}
	g = min(g, 10)
	return &g
}

func mapGoodreads(get func(string) string) (bookRow, bool) {
	title := get("Title")
	if title == "" {
		return bookRow{}, false
	}
	isbn := unwrapExcel(get("ISBN13"))
	if isbn == "" {
		isbn = unwrapExcel(get("ISBN"))
	}
	year, _ := strconv.Atoi(get("Year Published"))
	pages, _ := strconv.Atoi(get("Number of Pages"))
	owned, _ := strconv.Atoi(get("Owned Copies"))
	stars, _ := strconv.ParseFloat(get("My Rating"), 64)
	r := bookRow{Title: title, ISBN: isbn, Publisher: get("Publisher"), Year: year, Pages: pages, Review: get("My Review"),
		Authors: append(splitAuthors(get("Author"), "|"), splitAuthors(get("Additional Authors"), ",")...), Grade: starsToGrade(stars),
		Owned: owned > 0}
	shelf := get("Exclusive Shelf")
	r.Status = map[string]string{"read": "read", "currently-reading": "reading", "to-read": "to_read"}[shelf]
	if shelf == "read" || shelf == "currently-reading" {
		r.Owned = true // reading a book usually means having it
	}
	if r.Status == "read" {
		r.FinishedOn = parseDate([]string{"2006/01/02"}, get("Date Read"))
	}
	return r, true
}

func mapStoryGraph(get func(string) string) (bookRow, bool) {
	title := get("Title")
	if title == "" {
		return bookRow{}, false
	}
	stars, _ := strconv.ParseFloat(get("Star Rating"), 64)
	r := bookRow{Title: title, ISBN: unwrapExcel(get("ISBN/UID")), Authors: splitAuthors(get("Authors"), ","), Grade: starsToGrade(stars),
		Review: get("Review"), Owned: strings.EqualFold(get("Owned?"), "yes")}
	r.Status = map[string]string{"read": "read", "currently-reading": "reading", "to-read": "to_read", "did-not-finish": "abandoned"}[strings.ToLower(get("Read Status"))]
	if r.Status == "read" {
		r.FinishedOn = parseDate([]string{"2006/01/02"}, get("Last Date Read"))
		r.Owned = r.Owned || get("Owned?") == "" // a read book without ownership info: keep a copy
	}
	return r, true
}

func mapLibraryThing(get func(string) string) (bookRow, bool) {
	title := get("Title")
	if title == "" {
		return bookRow{}, false
	}
	isbn := strings.Trim(unwrapExcel(get("ISBN")), "[] ")
	if i := strings.IndexAny(isbn, "] ,"); i > 0 { // several ISBNs: keep the first
		isbn = isbn[:i]
	}
	if isbn == "" {
		isbn = strings.Trim(strings.SplitN(strings.Trim(get("ISBNs"), "[] "), "]", 2)[0], "[] ,")
	}
	pages, _ := strconv.Atoi(get("Page Count"))
	stars, _ := strconv.ParseFloat(get("Rating"), 64)
	var authors []string
	if a := get("Primary Author"); a != "" {
		if last, first, ok := strings.Cut(a, ","); ok {
			a = strings.TrimSpace(first) + " " + strings.TrimSpace(last)
		}
		authors = append(authors, a)
	}
	// "Publication" is "Publisher, Year" or "Publisher (City), Year".
	pub, year := get("Publication"), 0
	if i := strings.LastIndex(pub, ","); i >= 0 {
		if y, err := strconv.Atoi(strings.TrimSpace(pub[i+1:])); err == nil {
			year, pub = y, strings.TrimSpace(pub[:i])
		}
	}
	if year == 0 {
		year, _ = strconv.Atoi(get("Date"))
	}
	r := bookRow{Title: title, ISBN: isbn, Authors: authors, Publisher: pub, Year: year, Pages: pages, Grade: starsToGrade(stars),
		Review: get("Review"), Owned: true} // a LibraryThing catalog lists books the user has
	if d := parseDate([]string{"2006-01-02", "2006/01/02"}, get("Date Read")); d != nil {
		r.Status, r.FinishedOn = "read", d
	}
	return r, true
}

// Goodreads imports a Goodreads library export (CSV) into a library. Re-importing the same file
// does not duplicate copies.
func (im *Importer) Goodreads(ctx context.Context, libraryID uuid.UUID, r io.Reader) (Report, error) {
	return im.importCSV(ctx, libraryID, r, "Title", "goodreads-import", mapGoodreads)
}

// StoryGraph imports a StoryGraph export (CSV).
func (im *Importer) StoryGraph(ctx context.Context, libraryID uuid.UUID, r io.Reader) (Report, error) {
	return im.importCSV(ctx, libraryID, r, "Read Status", "storygraph-import", mapStoryGraph)
}

// LibraryThing imports a LibraryThing export (CSV or tab-separated).
func (im *Importer) LibraryThing(ctx context.Context, libraryID uuid.UUID, r io.Reader) (Report, error) {
	return im.importCSV(ctx, libraryID, r, "Primary Author", "librarything-import", mapLibraryThing)
}

func (im *Importer) importCSV(ctx context.Context, libraryID uuid.UUID, r io.Reader, signature, provenance string, mp mapper) (Report, error) {
	var rep Report
	p, err := authz.From(ctx)
	if err != nil {
		return rep, err
	}
	if _, err := im.library.RequirePerm(ctx, nil, libraryID, authz.PermWrite); err != nil {
		return rep, err
	}
	br := bufio.NewReader(r)
	first, _ := br.Peek(4096)
	cr := csv.NewReader(br)
	cr.LazyQuotes = true
	cr.FieldsPerRecord = -1
	if line, _, ok := strings.Cut(string(first), "\n"); ok && strings.Count(line, "\t") > strings.Count(line, ",") {
		cr.Comma = '\t' // LibraryThing offers a tab-separated export
	}
	head, err := cr.Read()
	if err != nil {
		return rep, fmt.Errorf("%w: unreadable header: %w", ErrFormat, err)
	}
	col := map[string]int{}
	for i, h := range head {
		col[strings.TrimPrefix(strings.TrimSpace(h), "\ufeff")] = i
	}
	if _, ok := col[signature]; !ok {
		return rep, fmt.Errorf("%w: this does not look like the expected export (no %q column)", ErrFormat, signature)
	}
	wishlist, err := im.lists.Wishlist(ctx)
	if err != nil {
		return rep, err
	}
	for row := 2; ; row++ {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			rep.Errors = append(rep.Errors, RowError{row, "unreadable row"})
			rep.Skipped++
			continue
		}
		rep.Rows++
		if rep.Rows > MaxRows {
			return rep, fmt.Errorf("%w: more than %d rows", ErrFormat, MaxRows)
		}
		get := func(name string) string {
			if i, ok := col[name]; ok && i < len(rec) {
				return strings.TrimSpace(rec[i])
			}
			return ""
		}
		b, ok := mp(get)
		if !ok {
			rep.Skipped++
			continue
		}
		im.importOne(ctx, p.UserID, libraryID, wishlist.ID, row, provenance, b, &rep)
	}
	return rep, nil
}

func (im *Importer) importOne(ctx context.Context, uid, libraryID, wishlistID uuid.UUID, row int, provenance string, b bookRow, rep *Report) {
	isbn := b.ISBN
	if _, _, err := catalog.NormalizeISBN(isbn); err != nil {
		isbn = "" // many rows have no usable ISBN: import them as ISBN-less books
	}
	prov := map[string]catalog.Provenance{"title": {Provider: provenance, FetchedAt: time.Now()}}
	ed, created, err := im.catalog.Create(ctx, uid, catalog.EditionInput{ISBN: isbn, Title: b.Title, Authors: b.Authors,
		Publisher: b.Publisher, Year: b.Year, Pages: b.Pages}, "", prov)
	if err != nil {
		rep.Errors = append(rep.Errors, RowError{row, "invalid book"})
		rep.Skipped++
		return
	}
	if created {
		rep.EditionsCreated++
	}
	if b.Owned {
		if n, err := im.q.CountInstancesOfEdition(ctx, dbsqlc.CountInstancesOfEditionParams{LibraryID: libraryID, EditionID: ed.ID}); err == nil && n == 0 {
			if _, err := im.library.AddInstance(ctx, libraryID, library.InstanceInput{EditionID: ed.ID}, ""); err != nil {
				rep.Errors = append(rep.Errors, RowError{row, "could not add the copy"})
			} else {
				rep.InstancesAdded++
			}
		}
	}
	if b.Status != "" {
		var finished *time.Time
		if b.Status == "read" {
			finished = b.FinishedOn
		}
		if _, err := im.reading.SetStatus(ctx, ed.WorkID, b.Status, nil, finished); err == nil {
			rep.ReadingStates++
		}
	}
	if b.Status == "to_read" && !b.Owned {
		if _, err := im.lists.AddItem(ctx, wishlistID, lists.ItemInput{EditionID: &ed.ID}); err == nil {
			rep.WishlistAdded++
		}
	}
	if b.Grade != nil || b.Review != "" {
		if _, err := im.reading.SetGrade(ctx, ed.WorkID, b.Grade, b.Review); err == nil {
			rep.Grades++
		}
	}
}
