// Package importer loads books from other services. It never queries metadata providers: the
// import uses the data in the file, so a large import makes no outbound request.
package importer

import (
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

// Goodreads imports a Goodreads library export (CSV) into a library. Re-importing the same file
// does not duplicate copies.
func (im *Importer) Goodreads(ctx context.Context, libraryID uuid.UUID, r io.Reader) (Report, error) {
	var rep Report
	p, err := authz.From(ctx)
	if err != nil {
		return rep, err
	}
	if _, err := im.library.RequirePerm(ctx, nil, libraryID, authz.PermWrite); err != nil {
		return rep, err
	}
	cr := csv.NewReader(r)
	cr.LazyQuotes = true
	cr.FieldsPerRecord = -1
	head, err := cr.Read()
	if err != nil {
		return rep, fmt.Errorf("importer: unreadable header: %w", err)
	}
	col := map[string]int{}
	for i, h := range head {
		col[strings.TrimPrefix(strings.TrimSpace(h), "\ufeff")] = i
	}
	if _, ok := col["Title"]; !ok {
		return rep, errors.New("importer: this does not look like a Goodreads export (no Title column)")
	}
	get := func(rec []string, name string) string {
		if i, ok := col[name]; ok && i < len(rec) {
			return strings.TrimSpace(rec[i])
		}
		return ""
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
			return rep, fmt.Errorf("importer: more than %d rows", MaxRows)
		}
		title := get(rec, "Title")
		if title == "" {
			rep.Skipped++
			continue
		}
		isbn := unwrapExcel(get(rec, "ISBN13"))
		if isbn == "" {
			isbn = unwrapExcel(get(rec, "ISBN"))
		}
		if _, _, err := catalog.NormalizeISBN(isbn); err != nil {
			isbn = "" // many Goodreads rows have no usable ISBN: import them as ISBN-less books
		}
		authors := []string{}
		if a := get(rec, "Author"); a != "" {
			authors = append(authors, a)
		}
		for _, a := range strings.Split(get(rec, "Additional Authors"), ",") {
			if a = strings.TrimSpace(a); a != "" {
				authors = append(authors, a)
			}
		}
		year, _ := strconv.Atoi(get(rec, "Year Published"))
		pages, _ := strconv.Atoi(get(rec, "Number of Pages"))
		prov := map[string]catalog.Provenance{"title": {Provider: "goodreads-import", FetchedAt: time.Now()}}
		ed, created, err := im.catalog.Create(ctx, p.UserID, catalog.EditionInput{ISBN: isbn, Title: title, Authors: authors,
			Publisher: get(rec, "Publisher"), Year: year, Pages: pages}, "", prov)
		if err != nil {
			rep.Errors = append(rep.Errors, RowError{row, "invalid book: " + err.Error()})
			rep.Skipped++
			continue
		}
		if created {
			rep.EditionsCreated++
		}

		owned, _ := strconv.Atoi(get(rec, "Owned Copies"))
		shelf := get(rec, "Exclusive Shelf")
		if owned > 0 || shelf == "read" || shelf == "currently-reading" {
			// Reading a book usually means having it: import a copy unless one is already there.
			if n, err := im.q.CountInstancesOfEdition(ctx, dbsqlc.CountInstancesOfEditionParams{LibraryID: libraryID, EditionID: ed.ID}); err == nil && n == 0 {
				if _, err := im.library.AddInstance(ctx, libraryID, library.InstanceInput{EditionID: ed.ID}, ""); err != nil {
					rep.Errors = append(rep.Errors, RowError{row, "could not add the copy"})
				} else {
					rep.InstancesAdded++
				}
			}
		}
		switch shelf {
		case "read", "currently-reading", "to-read":
			status := map[string]string{"read": "read", "currently-reading": "reading", "to-read": "to_read"}[shelf]
			var finished *time.Time
			if d, err := time.Parse("2006/01/02", get(rec, "Date Read")); err == nil && status == "read" {
				finished = &d
			}
			if _, err := im.reading.SetStatus(ctx, ed.WorkID, status, nil, finished); err == nil {
				rep.ReadingStates++
			}
		}
		if shelf == "to-read" && owned == 0 {
			if _, err := im.lists.AddItem(ctx, wishlist.ID, lists.ItemInput{EditionID: &ed.ID}); err == nil {
				rep.WishlistAdded++
			}
		}
		if stars, _ := strconv.Atoi(get(rec, "My Rating")); stars > 0 || get(rec, "My Review") != "" {
			var grade *int
			if stars >= 1 && stars <= 5 {
				g := stars * 2 // five stars map to the 1..10 scale
				grade = &g
			}
			if _, err := im.reading.SetGrade(ctx, ed.WorkID, grade, get(rec, "My Review")); err == nil {
				rep.Grades++
			}
		}
	}
	return rep, nil
}
