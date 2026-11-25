// Package loans tracks copies lent to contacts and books borrowed from people or libraries.
package loans

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/larueli/biblisann/internal/authz"
	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
	"github.com/larueli/biblisann/internal/i18n"
	"github.com/larueli/biblisann/internal/ics"
)

// Errors.
var (
	ErrInvalid       = errors.New("invalid input")
	ErrNotFound      = authz.ErrNotFound
	ErrAlreadyLent   = errors.New("this copy is already lent out")
	ErrAlreadyReturn = errors.New("already returned")
)

// Access is what loans need from the library service.
type Access interface {
	RequirePerm(ctx context.Context, q *dbsqlc.Queries, libraryID uuid.UUID, perm string) (*authz.Principal, error)
}

// Service is the loans service.
type Service struct {
	pool   *pgxpool.Pool
	q      *dbsqlc.Queries
	access Access
}

// NewService creates the service.
func NewService(pool *pgxpool.Pool, a Access) *Service {
	return &Service{pool: pool, q: dbsqlc.New(pool), access: a}
}

func principal(ctx context.Context, write bool) (*authz.Principal, error) {
	p, err := authz.From(ctx)
	if err != nil {
		return nil, err
	}
	if write && !p.HasScope("write") {
		return nil, authz.ErrForbidden
	}
	if !write && !p.HasScope("read") && !p.HasScope("write") {
		return nil, authz.ErrForbidden
	}
	return p, nil
}

// CreateContact adds a person (not necessarily a user) books can be lent to.
func (s *Service) CreateContact(ctx context.Context, name, email, notes string) (dbsqlc.Contact, error) {
	p, err := principal(ctx, true)
	if err != nil {
		return dbsqlc.Contact{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 200 || len(email) > 320 || len(notes) > 5000 {
		return dbsqlc.Contact{}, ErrInvalid
	}
	return s.q.CreateContact(ctx, dbsqlc.CreateContactParams{OwnerID: p.UserID, Name: name, Email: strings.TrimSpace(email), Notes: notes})
}

// Contacts lists the caller's contacts.
func (s *Service) Contacts(ctx context.Context) ([]dbsqlc.Contact, error) {
	p, err := principal(ctx, false)
	if err != nil {
		return nil, err
	}
	return s.q.ListContacts(ctx, p.UserID)
}

// DeleteContact removes a contact (their loans keep the counterparty name).
func (s *Service) DeleteContact(ctx context.Context, id uuid.UUID) error {
	p, err := principal(ctx, true)
	if err != nil {
		return err
	}
	n, err := s.q.DeleteContact(ctx, dbsqlc.DeleteContactParams{ID: id, OwnerID: p.UserID})
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// LendInput describes lending one of my copies.
type LendInput struct {
	InstanceID   uuid.UUID
	ContactID    *uuid.UUID
	Counterparty string // used when there is no contact
	DueOn        *time.Time
	LentOn       *time.Time // defaults to today (set by imports)
	Notes        string
}

// Lend records that a copy left the shelf.
func (s *Service) Lend(ctx context.Context, in LendInput) (dbsqlc.Loan, error) {
	inst, err := s.q.GetInstance(ctx, in.InstanceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return dbsqlc.Loan{}, ErrNotFound
	}
	if err != nil {
		return dbsqlc.Loan{}, err
	}
	p, err := s.access.RequirePerm(ctx, nil, inst.LibraryID, authz.PermLoan)
	if err != nil {
		return dbsqlc.Loan{}, err
	}
	name := strings.TrimSpace(in.Counterparty)
	if in.ContactID != nil {
		c, err := s.q.GetContact(ctx, *in.ContactID)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && c.OwnerID != p.UserID) {
			return dbsqlc.Loan{}, ErrInvalid
		}
		if err != nil {
			return dbsqlc.Loan{}, err
		}
		name = c.Name
	}
	if name == "" || len(name) > 200 || len(in.Notes) > 5000 {
		return dbsqlc.Loan{}, ErrInvalid
	}
	ed, err := s.q.GetEdition(ctx, inst.EditionID)
	if err != nil {
		return dbsqlc.Loan{}, err
	}
	l, err := s.q.CreateLoan(ctx, dbsqlc.CreateLoanParams{LibraryID: inst.LibraryID, OwnerID: p.UserID, Direction: "lent",
		InstanceID: &in.InstanceID, ContactID: in.ContactID, Counterparty: name, Title: ed.Title, LentOn: dayOr(in.LentOn), DueOn: in.DueOn, Notes: in.Notes})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return dbsqlc.Loan{}, ErrAlreadyLent
	}
	return l, err
}

// BorrowInput describes a book I borrowed.
type BorrowInput struct {
	LibraryID    uuid.UUID
	Title        string
	Counterparty string // person or library
	DueOn        *time.Time
	LentOn       *time.Time // defaults to today (set by imports)
	Notes        string
}

// Borrow records a book I must give back.
func (s *Service) Borrow(ctx context.Context, in BorrowInput) (dbsqlc.Loan, error) {
	p, err := s.access.RequirePerm(ctx, nil, in.LibraryID, authz.PermLoan)
	if err != nil {
		return dbsqlc.Loan{}, err
	}
	title, who := strings.TrimSpace(in.Title), strings.TrimSpace(in.Counterparty)
	if title == "" || len(title) > 1000 || who == "" || len(who) > 200 || len(in.Notes) > 5000 {
		return dbsqlc.Loan{}, ErrInvalid
	}
	return s.q.CreateLoan(ctx, dbsqlc.CreateLoanParams{LibraryID: in.LibraryID, OwnerID: p.UserID, Direction: "borrowed",
		Counterparty: who, Title: title, LentOn: dayOr(in.LentOn), DueOn: in.DueOn, Notes: in.Notes})
}

func today() time.Time { return time.Now().UTC().Truncate(24 * time.Hour) }

func dayOr(t *time.Time) time.Time {
	if t != nil {
		return *t
	}
	return today()
}

// manage loads a loan the caller may act on: any member holding loan.write on its library, not
// only the person who recorded it. The permission is checked now, so someone who lost access
// to the library can no longer return or change the loans they created.
func (s *Service) manage(ctx context.Context, id uuid.UUID) (dbsqlc.Loan, error) {
	l, err := s.q.GetLoan(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return l, ErrNotFound
	}
	if err != nil {
		return l, err
	}
	if _, err := s.access.RequirePerm(ctx, nil, l.LibraryID, authz.PermLoan); err != nil {
		if errors.Is(err, authz.ErrForbidden) {
			return dbsqlc.Loan{}, ErrNotFound // do not reveal loans of libraries the caller only reads
		}
		return dbsqlc.Loan{}, err
	}
	return l, nil
}

// Return marks a loan as returned (today when on is nil).
func (s *Service) Return(ctx context.Context, id uuid.UUID, on *time.Time) (dbsqlc.Loan, error) {
	if _, err := s.manage(ctx, id); err != nil {
		return dbsqlc.Loan{}, err
	}
	d := today()
	if on != nil {
		d = *on
	}
	l, err := s.q.ReturnLoan(ctx, dbsqlc.ReturnLoanParams{ID: id, ReturnedOn: &d})
	if errors.Is(err, pgx.ErrNoRows) {
		return l, ErrAlreadyReturn
	}
	return l, err
}

// Reschedule changes the due date and notes of a loan.
func (s *Service) Reschedule(ctx context.Context, id uuid.UUID, due *time.Time, notes string) (dbsqlc.Loan, error) {
	if _, err := s.manage(ctx, id); err != nil {
		return dbsqlc.Loan{}, err
	}
	if len(notes) > 5000 {
		return dbsqlc.Loan{}, ErrInvalid
	}
	return s.q.UpdateLoanDue(ctx, dbsqlc.UpdateLoanDueParams{ID: id, DueOn: due, Notes: notes})
}

// List returns the loans of every library where the caller holds loan.write, open ones first.
func (s *Service) List(ctx context.Context, includeReturned bool) ([]dbsqlc.ListLoansInLibrariesRow, error) {
	p, err := principal(ctx, false)
	if err != nil {
		return nil, err
	}
	libs, err := s.q.ListLibrariesForUser(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(libs))
	for _, l := range libs {
		if !p.AllowsLibrary(l.ID) { // a token restricted to some libraries sees only their loans
			continue
		}
		if _, err := s.access.RequirePerm(ctx, nil, l.ID, authz.PermLoan); err != nil {
			continue
		}
		ids = append(ids, l.ID)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	return s.q.ListLoansInLibraries(ctx, dbsqlc.ListLoansInLibrariesParams{Column1: ids, Column2: includeReturned, Limit: 500})
}

// FeedEvents returns calendar events for the open loans of a user. It is used by the calendar
// feed, which authenticates with a feed token instead of a session, so it takes the user ID.
func (s *Service) FeedEvents(ctx context.Context, userID uuid.UUID, locale string) ([]ics.Event, error) {
	// Only loans of libraries where the owner still holds loan.write: a feed token must not keep
	// leaking titles and borrowers after someone left a library.
	libs, err := s.q.ListLibrariesForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	as := authz.With(ctx, &authz.Principal{UserID: userID, Via: "session"})
	ids := make([]uuid.UUID, 0, len(libs))
	for _, l := range libs {
		if _, err := s.access.RequirePerm(as, nil, l.ID, authz.PermLoan); err == nil {
			ids = append(ids, l.ID)
		}
	}
	rows, err := s.q.OpenLoansForFeed(ctx, dbsqlc.OpenLoansForFeedParams{OwnerID: userID, Column2: ids})
	if err != nil {
		return nil, err
	}
	now := time.Now()
	out := make([]ics.Event, 0, len(rows))
	for _, r := range rows {
		if r.DueOn == nil {
			continue
		}
		key := "ics.lent"
		if r.Direction == "borrowed" {
			key = "ics.borrowed"
		}
		who := r.Counterparty
		out = append(out, ics.Event{UID: ics.UIDFor(r.ID.String()), Day: *r.DueOn, Stamp: now, Summary: i18n.T(locale, key, r.Title, who)})
	}
	return out, nil
}
