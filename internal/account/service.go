package account

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/larueli/biblisann/internal/authz"
	"github.com/larueli/biblisann/internal/catalog"
	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
	"github.com/larueli/biblisann/internal/library"
	"github.com/larueli/biblisann/internal/lists"
	"github.com/larueli/biblisann/internal/loans"
	"github.com/larueli/biblisann/internal/reading"
)

// Service exports, imports and deletes.
type Service struct {
	pool    *pgxpool.Pool
	q       *dbsqlc.Queries
	library *library.Service
	catalog *catalog.Service
	lists   *lists.Service
	loans   *loans.Service
	reading *reading.Service
	imports chan struct{} // limits concurrent archive imports
}

// maxImportLibraries bounds the libraries one archive may create.
const maxImportLibraries = 50

// ErrBusy is returned when too many imports are already running.
var ErrBusy = errors.New("account: too many imports in progress, try again later")

// NewService creates the service.
func NewService(pool *pgxpool.Pool, l *library.Service, c *catalog.Service, ls *lists.Service, ln *loans.Service, r *reading.Service) *Service {
	return &Service{pool: pool, q: dbsqlc.New(pool), library: l, catalog: c, lists: ls, loans: ln, reading: r, imports: make(chan struct{}, 2)}
}

// libraryEntry renders one library as NDJSON and returns the cover IDs it references.
func (s *Service) libraryEntry(ctx context.Context, userID, libraryID uuid.UUID) ([]byte, map[string]uuid.UUID, error) {
	lib, err := s.library.GetLibrary(ctx, libraryID)
	if err != nil {
		return nil, nil, err
	}
	w := newWriter(FormatLibrary)
	_ = w.add("library", libraryRow{Name: lib.Name, Settings: lib.Settings})
	covers := map[string]uuid.UUID{}

	insts, err := s.q.ExportInstances(ctx, libraryID)
	if err != nil {
		return nil, nil, err
	}
	// places, parents first
	places, err := s.q.ListPlaces(ctx, libraryID)
	if err != nil {
		return nil, nil, err
	}
	done := map[uuid.UUID]bool{}
	for len(done) < len(places) {
		progressed := false
		for _, p := range places {
			if done[p.ID] || (p.ParentID != nil && !done[*p.ParentID]) {
				continue
			}
			row := placeRow{Key: p.ID.String(), Name: p.Name, Kind: p.Kind, Position: int(p.Position)}
			if p.ParentID != nil {
				row.Parent = p.ParentID.String()
			}
			_ = w.add("place", row)
			done[p.ID] = true
			progressed = true
		}
		if !progressed {
			break
		}
	}
	// editions used by the copies
	seen := map[uuid.UUID]bool{}
	for _, i := range insts {
		if seen[i.EditionID] {
			continue
		}
		seen[i.EditionID] = true
		e, err := s.q.GetEdition(ctx, i.EditionID)
		if err != nil {
			return nil, nil, err
		}
		row := editionRow{Key: e.ID.String(), Title: e.Title, Subtitle: e.Subtitle, Authors: nonNil(e.Authors), Publisher: e.Publisher,
			Language: e.Language, Description: e.Description, SeriesName: e.SeriesName, SeriesPosition: e.SeriesPosition}
		if e.Isbn13 != nil {
			row.ISBN13 = *e.Isbn13
		}
		if e.PublishedYear != nil {
			row.Year = int(*e.PublishedYear)
		}
		if e.Pages != nil {
			row.Pages = int(*e.Pages)
		}
		if e.CoverID != nil {
			if sha, err := s.catalog.CoverHash(ctx, *e.CoverID); err == nil {
				row.CoverSHA = sha
				covers[sha] = *e.CoverID
			}
		}
		_ = w.add("edition", row)
	}
	for _, i := range insts {
		row := instanceRow{Key: i.ID.String(), Edition: i.EditionID.String(), Kind: i.Kind, Condition: i.Condition, Notes: i.Notes, AcquiredOn: day(i.AcquiredOn)}
		if i.PlaceID != nil {
			row.Place = i.PlaceID.String()
		}
		_ = w.add("instance", row)
	}
	// library lists
	ls, err := s.q.ListLibraryLists(ctx, &libraryID)
	if err != nil {
		return nil, nil, err
	}
	for _, l := range ls {
		_ = w.add("list", listRow{Key: l.ID.String(), Name: l.Name})
		items, err := s.q.ListItemsOfList(ctx, l.ID)
		if err != nil {
			return nil, nil, err
		}
		for _, it := range items {
			_ = w.add("list_item", listItemRowOf(l.ID.String(), it))
		}
	}
	// the exporting user's own loans in this library
	lns, err := s.q.ListLoansInLibrary(ctx, dbsqlc.ListLoansInLibraryParams{LibraryID: libraryID, OwnerID: userID})
	if err != nil {
		return nil, nil, err
	}
	for _, l := range lns {
		r := loanRow{Direction: l.Direction, Counterparty: l.Counterparty, Title: l.Title, LentOn: l.LentOn.Format("2006-01-02"),
			DueOn: day(l.DueOn), ReturnedOn: day(l.ReturnedOn), Notes: l.Notes}
		if l.InstanceID != nil {
			r.Instance = l.InstanceID.String()
		}
		_ = w.add("loan", r)
	}
	return w.buf.Bytes(), covers, nil
}

func listItemRowOf(listKey string, it dbsqlc.ListItemsOfListRow) listItemRow {
	r := listItemRow{List: listKey, Title: it.Title, Authors: nonNil(it.Authors), SeriesName: it.SeriesName, SeriesPosition: it.SeriesPosition, Note: it.Note}
	if it.Isbn13 != nil {
		r.ISBN13 = *it.Isbn13
	}
	return r
}

func nonNil(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

func (s *Service) writeCovers(ctx context.Context, tw *tar.Writer, covers map[string]uuid.UUID, written map[string]bool) error {
	for sha, id := range covers {
		if written[sha] {
			continue
		}
		rc, err := s.catalog.CoverReader(ctx, id, false)
		if err != nil {
			continue // a missing file must not fail the export
		}
		data, err := io.ReadAll(io.LimitReader(rc, maxCover))
		_ = rc.Close()
		if err != nil {
			continue
		}
		if err := putEntry(tw, "covers/"+sha, data); err != nil {
			return err
		}
		written[sha] = true
	}
	return nil
}

// ExportLibrary writes a portable archive of one library (needs read access).
func (s *Service) ExportLibrary(ctx context.Context, libraryID uuid.UUID, out io.Writer) error {
	p, err := authz.From(ctx)
	if err != nil {
		return err
	}
	data, covers, err := s.libraryEntry(ctx, p.UserID, libraryID)
	if err != nil {
		return err
	}
	tw := tar.NewWriter(out)
	if err := putEntry(tw, "library.ndjson", data); err != nil {
		return err
	}
	if err := s.writeCovers(ctx, tw, covers, map[string]bool{}); err != nil {
		return err
	}
	return tw.Close()
}

// ExportAccount writes everything personal: profile, contacts, personal lists, reading status,
// grades, and every library the user administers (with its copies, places, lists and loans).
// Libraries that merely share with the user belong to someone else and are not included.
func (s *Service) ExportAccount(ctx context.Context, out io.Writer) error {
	p, err := authz.From(ctx)
	if err != nil {
		return err
	}
	u, err := s.q.GetUser(ctx, p.UserID)
	if err != nil {
		return err
	}
	w := newWriter(FormatAccount)
	_ = w.add("profile", profileRow{Email: u.Email, DisplayName: u.DisplayName, Locale: u.Locale, Settings: u.Settings})
	contacts, _ := s.q.ListContacts(ctx, p.UserID)
	for _, c := range contacts {
		_ = w.add("contact", contactRow{c.Name, c.Email, c.Notes})
	}
	pls, err := s.q.ListPersonalLists(ctx, &p.UserID)
	if err != nil {
		return err
	}
	for _, l := range pls {
		kind := ""
		if l.Kind == "wishlist" {
			kind = "wishlist"
		}
		_ = w.add("list", listRow{Key: l.ID.String(), Name: l.Name, Kind: kind})
		items, err := s.q.ListItemsOfList(ctx, l.ID)
		if err != nil {
			return err
		}
		for _, it := range items {
			_ = w.add("list_item", listItemRowOf(l.ID.String(), it))
		}
	}
	rs, err := s.q.ExportReading(ctx, p.UserID)
	if err != nil {
		return err
	}
	for _, r := range rs {
		_ = w.add("reading", readingRow{Book: ref(r.Isbn13, r.Title, r.Authors), Status: r.Status, StartedOn: day(r.StartedOn), FinishedOn: day(r.FinishedOn)})
	}
	gs, err := s.q.ExportGrades(ctx, p.UserID)
	if err != nil {
		return err
	}
	for _, g := range gs {
		var grade *int
		if g.Grade != nil {
			v := int(*g.Grade)
			grade = &v
		}
		_ = w.add("grade", gradeRow{Book: ref(g.Isbn13, g.Title, g.Authors), Grade: grade, Comment: g.Comment})
	}

	tw := tar.NewWriter(out)
	libs, err := s.q.ListAdminedLibraries(ctx, p.UserID)
	if err != nil {
		return err
	}
	written := map[string]bool{}
	for n, l := range libs {
		data, covers, err := s.libraryEntry(ctx, p.UserID, l.ID)
		if err != nil {
			return err
		}
		file := "libraries/" + strconv.Itoa(n+1) + ".ndjson"
		_ = w.add("library", libraryRefRow{File: file, Name: l.Name, Role: "admin"})
		if err := putEntry(tw, file, data); err != nil {
			return err
		}
		if err := s.writeCovers(ctx, tw, covers, written); err != nil {
			return err
		}
	}
	if err := putEntry(tw, "account.ndjson", w.buf.Bytes()); err != nil {
		return err
	}
	return tw.Close()
}

func ref(isbn *string, title string, authors []string) bookRef {
	r := bookRef{Title: title, Authors: nonNil(authors)}
	if isbn != nil {
		r.ISBN13 = *isbn
	}
	return r
}

// ImportReport counts what an import created.
type ImportReport struct {
	Libraries, Editions, Instances, Places, Lists, ListItems, Loans, Contacts, Reading, Grades int
	LibraryIDs                                                                                 []uuid.UUID
}

// ImportLibrary creates a new library, owned by the caller, from a library archive.
func (s *Service) ImportLibrary(ctx context.Context, r io.Reader) (ImportReport, error) {
	var rep ImportReport
	if _, err := authz.From(ctx); err != nil {
		return rep, err
	}
	select {
	case s.imports <- struct{}{}: // bounded memory: archives are held in RAM while they are imported
		defer func() { <-s.imports }()
	default:
		return rep, ErrBusy
	}
	a, err := readArchive(r)
	if err != nil {
		return rep, err
	}
	data, ok := a.entries["library.ndjson"]
	if !ok {
		return rep, fmt.Errorf("%w: no library.ndjson", ErrArchive)
	}
	if err := s.importLibrary(ctx, a, data, &rep); err != nil {
		return rep, err
	}
	return rep, nil
}

func (s *Service) importLibrary(ctx context.Context, a *archive, data []byte, rep *ImportReport) error {
	p, _ := authz.From(ctx)
	var libID uuid.UUID
	editions := map[string]dbsqlc.Edition{}
	places := map[string]uuid.UUID{}
	instances := map[string]uuid.UUID{}
	listIDs := map[string]uuid.UUID{}
	stored := map[string]*uuid.UUID{}

	return each(data, FormatLibrary, func(t string, raw json.RawMessage) error {
		switch t {
		case "library":
			var r libraryRow
			if err := json.Unmarshal(raw, &r); err != nil {
				return ErrArchive
			}
			l, err := s.library.CreateLibrary(ctx, r.Name)
			if err != nil {
				return fmt.Errorf("%w: library: %w", ErrArchive, err)
			}
			libID = l.ID
			rep.Libraries++
			rep.LibraryIDs = append(rep.LibraryIDs, l.ID)
			if len(r.Settings) > 0 && string(r.Settings) != "null" {
				_, _ = s.library.UpdateLibrary(ctx, l.ID, l.Name, r.Settings) // best effort: settings are optional
			}
		case "edition":
			var r editionRow
			if json.Unmarshal(raw, &r) != nil {
				return ErrArchive
			}
			e, created, err := s.catalog.Create(ctx, p.UserID, catalog.EditionInput{ISBN: r.ISBN13, Title: r.Title, Subtitle: r.Subtitle,
				Authors: r.Authors, Publisher: r.Publisher, Year: r.Year, Language: r.Language, Pages: r.Pages, Description: r.Description,
				Series: r.SeriesName, SeriesPos: r.SeriesPosition}, "", map[string]catalog.Provenance{"title": {Provider: "import"}})
			if err != nil {
				return fmt.Errorf("%w: edition %q: %w", ErrArchive, r.Title, err)
			}
			if created {
				rep.Editions++
			}
			editions[r.Key] = e
			if r.CoverSHA != "" && created && e.CoverID == nil { // never fill covers of editions other users already share
				if data, ok := a.covers[r.CoverSHA]; ok {
					if _, done := stored[r.CoverSHA]; !done {
						stored[r.CoverSHA] = s.catalog.StoreCover(ctx, data) // re-encoded: never trust archive images
					}
					if id := stored[r.CoverSHA]; id != nil {
						_ = s.catalog.AttachCover(ctx, e.ID, *id)
					}
				}
			}
		case "place":
			var r placeRow
			if json.Unmarshal(raw, &r) != nil {
				return ErrArchive
			}
			in := library.PlaceInput{Name: r.Name, Kind: r.Kind, Position: r.Position}
			if r.Parent != "" {
				pid, ok := places[r.Parent]
				if !ok {
					return fmt.Errorf("%w: place parent", ErrArchive)
				}
				in.ParentID = &pid
			}
			pl, err := s.library.CreatePlace(ctx, libID, in)
			if err != nil {
				return fmt.Errorf("%w: place: %w", ErrArchive, err)
			}
			places[r.Key] = pl.ID
			rep.Places++
		case "instance":
			var r instanceRow
			if json.Unmarshal(raw, &r) != nil {
				return ErrArchive
			}
			e, ok := editions[r.Edition]
			if !ok {
				return fmt.Errorf("%w: instance edition", ErrArchive)
			}
			in := library.InstanceInput{EditionID: e.ID, Kind: r.Kind, Condition: r.Condition, Notes: r.Notes, AcquiredOn: parseDay(r.AcquiredOn)}
			if r.Place != "" {
				if pid, ok := places[r.Place]; ok {
					in.PlaceID = &pid
				}
			}
			inst, err := s.library.AddInstance(ctx, libID, in, "")
			if err != nil {
				return fmt.Errorf("%w: instance: %w", ErrArchive, err)
			}
			instances[r.Key] = inst.ID
			rep.Instances++
		case "list":
			var r listRow
			if json.Unmarshal(raw, &r) != nil {
				return ErrArchive
			}
			l, err := s.lists.Create(ctx, r.Name, &libID)
			if err != nil {
				return fmt.Errorf("%w: list: %w", ErrArchive, err)
			}
			listIDs[r.Key] = l.ID
			rep.Lists++
		case "list_item":
			return s.importItem(ctx, raw, listIDs, editions, rep)
		case "loan":
			return s.importLoan(ctx, raw, libID, instances, rep)
		}
		return nil
	})
}

func (s *Service) importItem(ctx context.Context, raw json.RawMessage, listIDs map[string]uuid.UUID, editions map[string]dbsqlc.Edition, rep *ImportReport) error {
	var r listItemRow
	if json.Unmarshal(raw, &r) != nil {
		return ErrArchive
	}
	lid, ok := listIDs[r.List]
	if !ok {
		return fmt.Errorf("%w: list item without list", ErrArchive)
	}
	in := lists.ItemInput{Title: r.Title, Authors: r.Authors, SeriesName: r.SeriesName, SeriesPosition: r.SeriesPosition, Note: r.Note}
	if _, _, err := catalog.NormalizeISBN(r.ISBN13); err == nil {
		in.ISBN13 = r.ISBN13
	}
	if _, err := s.lists.AddItem(ctx, lid, in); err != nil {
		return fmt.Errorf("%w: list item: %w", ErrArchive, err)
	}
	rep.ListItems++
	_ = editions
	return nil
}

func (s *Service) importLoan(ctx context.Context, raw json.RawMessage, libID uuid.UUID, instances map[string]uuid.UUID, rep *ImportReport) error {
	var r loanRow
	if json.Unmarshal(raw, &r) != nil {
		return ErrArchive
	}
	var (
		l   dbsqlc.Loan
		err error
	)
	switch r.Direction {
	case "lent":
		iid, ok := instances[r.Instance]
		if !ok {
			return nil // the copy was not exported (deleted): the loan has nothing to attach to
		}
		l, err = s.loans.Lend(ctx, loans.LendInput{InstanceID: iid, Counterparty: r.Counterparty, DueOn: parseDay(r.DueOn), LentOn: parseDay(r.LentOn), Notes: r.Notes})
	case "borrowed":
		l, err = s.loans.Borrow(ctx, loans.BorrowInput{LibraryID: libID, Title: r.Title, Counterparty: r.Counterparty, DueOn: parseDay(r.DueOn), LentOn: parseDay(r.LentOn), Notes: r.Notes})
	default:
		return ErrArchive
	}
	if err != nil {
		return fmt.Errorf("%w: loan: %w", ErrArchive, err)
	}
	if d := parseDay(r.ReturnedOn); d != nil {
		if _, err := s.loans.Return(ctx, l.ID, d); err != nil {
			return err
		}
	}
	rep.Loans++
	return nil
}

// ImportAccount restores personal data into the caller's account: contacts, personal lists,
// reading status, grades, and each library of the archive as a new library. The profile is read
// but never applied (an import must not change your email or sign-in).
func (s *Service) ImportAccount(ctx context.Context, r io.Reader) (ImportReport, error) {
	var rep ImportReport
	p, err := authz.From(ctx)
	if err != nil {
		return rep, err
	}
	a, err := readArchive(r)
	if err != nil {
		return rep, err
	}
	acc, ok := a.entries["account.ndjson"]
	if !ok {
		return rep, fmt.Errorf("%w: no account.ndjson", ErrArchive)
	}
	listIDs := map[string]uuid.UUID{}
	var files []string
	err = each(acc, FormatAccount, func(t string, raw json.RawMessage) error {
		switch t {
		case "contact":
			var c contactRow
			if json.Unmarshal(raw, &c) != nil {
				return ErrArchive
			}
			if _, err := s.loans.CreateContact(ctx, c.Name, c.Email, c.Notes); err == nil {
				rep.Contacts++
			}
		case "list":
			var l listRow
			if json.Unmarshal(raw, &l) != nil {
				return ErrArchive
			}
			var dst dbsqlc.List
			var err error
			if l.Kind == "wishlist" {
				dst, err = s.lists.Wishlist(ctx)
			} else {
				dst, err = s.lists.Create(ctx, l.Name, nil)
				rep.Lists++
			}
			if err != nil {
				return fmt.Errorf("%w: list: %w", ErrArchive, err)
			}
			listIDs[l.Key] = dst.ID
		case "list_item":
			return s.importItem(ctx, raw, listIDs, nil, &rep)
		case "reading":
			var rr readingRow
			if json.Unmarshal(raw, &rr) != nil {
				return ErrArchive
			}
			if e, err := s.bookFor(ctx, p.UserID, rr.Book); err == nil {
				if _, err := s.reading.SetStatus(ctx, e.WorkID, rr.Status, parseDay(rr.StartedOn), parseDay(rr.FinishedOn)); err == nil {
					rep.Reading++
				}
			}
		case "grade":
			var g gradeRow
			if json.Unmarshal(raw, &g) != nil {
				return ErrArchive
			}
			if e, err := s.bookFor(ctx, p.UserID, g.Book); err == nil {
				if _, err := s.reading.SetGrade(ctx, e.WorkID, g.Grade, g.Comment); err == nil {
					rep.Grades++
				}
			}
		case "library":
			var lr libraryRefRow
			if json.Unmarshal(raw, &lr) != nil || !strings.HasPrefix(lr.File, "libraries/") {
				return ErrArchive
			}
			if len(files) >= maxImportLibraries {
				return fmt.Errorf("%w: too many libraries", ErrArchive)
			}
			files = append(files, lr.File)
		}
		return nil
	})
	if err != nil {
		return rep, err
	}
	slices.Sort(files)
	files = slices.Compact(files) // a file listed twice is imported once
	for _, f := range files {
		data, ok := a.entries[f]
		if !ok {
			return rep, fmt.Errorf("%w: missing %s", ErrArchive, f)
		}
		if err := s.importLibrary(ctx, a, data, &rep); err != nil {
			return rep, err
		}
	}
	return rep, nil
}

// bookFor finds or creates the edition a personal record refers to.
func (s *Service) bookFor(ctx context.Context, userID uuid.UUID, b bookRef) (dbsqlc.Edition, error) {
	e, _, err := s.catalog.Create(ctx, userID, catalog.EditionInput{ISBN: b.ISBN13, Title: b.Title, Authors: b.Authors}, "",
		map[string]catalog.Provenance{"title": {Provider: "import"}})
	return e, err
}

// ---- deletion

// ErrSoleAdmin lists shared libraries the user alone administers.
type ErrSoleAdmin struct{ Libraries []string }

func (e *ErrSoleAdmin) Error() string {
	return "you are the only administrator of shared libraries (" + strings.Join(e.Libraries, ", ") + "): give another member the admin role first, or delete with promotion"
}

// ErrLastInstanceAdmin is returned when deleting the last instance administrator.
var ErrLastInstanceAdmin = errors.New("this is the last instance administrator")

// Delete removes an account and everything that belongs only to it. Libraries it alone uses are
// deleted with their copies; a shared library it alone administers blocks the deletion, or, with
// promote, passes to its most privileged other member. Books, covers and works of the shared
// catalog stay (they are shared). The caller must be the account itself or an instance
// administrator; confirming credentials is the caller's job.
func (s *Service) Delete(ctx context.Context, target uuid.UUID, promote bool) error {
	p, err := authz.From(ctx)
	if err != nil {
		return err
	}
	if p.UserID != target && !p.IsAdmin {
		return authz.ErrForbidden
	}
	if !p.HasScope("write") {
		return authz.ErrForbidden
	}
	u, err := s.q.GetUser(ctx, target)
	if errors.Is(err, pgx.ErrNoRows) {
		return authz.ErrNotFound
	}
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext('biblisann.register'))"); err != nil {
		return err
	}
	if u.IsAdmin && u.DisabledAt == nil {
		if n, err := q.CountAdminUsers(ctx); err != nil {
			return err
		} else if n <= 1 {
			return ErrLastInstanceAdmin
		}
	}
	memberships, err := q.ListMembershipsOfUser(ctx, target)
	if err != nil {
		return err
	}
	var blocked []string
	for _, m := range memberships {
		if m.Role != "admin" {
			continue
		}
		members, err := q.ListLibraryMembers(ctx, m.LibraryID)
		if err != nil {
			return err
		}
		otherAdmin := false
		var best *dbsqlc.ListLibraryMembersRow
		for i, o := range members {
			if o.UserID == target {
				continue
			}
			if o.Role == "admin" {
				otherAdmin = true
			}
			if best == nil || rank(o.Role) > rank(best.Role) {
				best = &members[i]
			}
		}
		if otherAdmin {
			continue
		}
		shares, err := q.CountLibraryGroupShares(ctx, m.LibraryID)
		if err != nil {
			return err
		}
		switch {
		case best == nil && shares == 0:
			if err := q.DeleteLibrary(ctx, m.LibraryID); err != nil {
				return err
			}
		case best != nil && promote && best.Role != "custom":
			if err := q.ShareWithUser(ctx, dbsqlc.ShareWithUserParams{LibraryID: m.LibraryID, UserID: best.UserID, Role: "admin"}); err != nil {
				return err
			}
		case best != nil && promote: // a custom profile cannot simply be raised: replace it by admin
			if err := q.ShareWithUser(ctx, dbsqlc.ShareWithUserParams{LibraryID: m.LibraryID, UserID: best.UserID, Role: "admin"}); err != nil {
				return err
			}
		default:
			blocked = append(blocked, m.Name)
		}
	}
	if len(blocked) > 0 {
		return &ErrSoleAdmin{Libraries: blocked}
	}
	if err := q.DeleteUser(ctx, target); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func rank(role string) int {
	switch role {
	case "admin":
		return 3
	case "edit":
		return 2
	case "custom":
		return 1
	}
	return 0
}
