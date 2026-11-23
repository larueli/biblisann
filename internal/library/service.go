package library

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/larueli/biblisann/internal/authz"
	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
)

// Errors returned to callers.
var (
	ErrNotFound            = authz.ErrNotFound
	ErrForbidden           = authz.ErrForbidden
	ErrInvalid             = errors.New("invalid input")
	ErrIdempotencyConflict = errors.New("idempotency key reused with a different request")
)

// Service is the library service.
type Service struct {
	pool *pgxpool.Pool
	q    *dbsqlc.Queries
}

// NewService creates the service.
func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool, q: dbsqlc.New(pool)} }

// Require checks that the caller has at least need in the library and returns the principal.
// A caller with no membership gets ErrNotFound, so library IDs cannot be probed.
func (s *Service) Require(ctx context.Context, q *dbsqlc.Queries, libraryID uuid.UUID, need authz.Role) (*authz.Principal, error) {
	p, err := authz.From(ctx)
	if err != nil {
		return nil, err
	}
	scope := "read"
	if need >= authz.RoleEdit {
		scope = "write"
	}
	if !p.HasScope(scope) && (scope != "read" || !p.HasScope("write")) {
		return nil, ErrForbidden
	}
	if q == nil {
		q = s.q
	}
	role, err := q.GetLibraryRole(ctx, dbsqlc.GetLibraryRoleParams{LibraryID: libraryID, UserID: p.UserID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("library: role: %w", err)
	}
	if !authz.Allows(authz.ParseRole(role), need) {
		return nil, ErrForbidden
	}
	return p, nil
}

// CreateLibrary creates a library and makes the caller its administrator.
func (s *Service) CreateLibrary(ctx context.Context, name string) (dbsqlc.Library, error) {
	p, err := authz.From(ctx)
	if err != nil {
		return dbsqlc.Library{}, err
	}
	if !p.HasScope("write") {
		return dbsqlc.Library{}, ErrForbidden
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 200 {
		return dbsqlc.Library{}, ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return dbsqlc.Library{}, fmt.Errorf("library: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	l, err := q.CreateLibrary(ctx, name)
	if err != nil {
		return l, fmt.Errorf("library: create: %w", err)
	}
	if err := q.AddLibraryMember(ctx, dbsqlc.AddLibraryMemberParams{LibraryID: l.ID, UserID: p.UserID, Role: "admin"}); err != nil {
		return l, fmt.Errorf("library: member: %w", err)
	}
	return l, tx.Commit(ctx)
}

// ListLibraries returns the libraries the caller belongs to.
func (s *Service) ListLibraries(ctx context.Context) ([]dbsqlc.ListLibrariesForUserRow, error) {
	p, err := authz.From(ctx)
	if err != nil {
		return nil, err
	}
	if !p.HasScope("read") && !p.HasScope("write") {
		return nil, ErrForbidden
	}
	return s.q.ListLibrariesForUser(ctx, p.UserID)
}

// GetLibrary returns a library the caller can read, with the caller's role.
func (s *Service) GetLibrary(ctx context.Context, id uuid.UUID) (dbsqlc.Library, error) {
	if _, err := s.Require(ctx, nil, id, authz.RoleRead); err != nil {
		return dbsqlc.Library{}, err
	}
	return s.q.GetLibrary(ctx, id)
}

// UpdateLibrary renames a library and replaces its settings (administrators only).
func (s *Service) UpdateLibrary(ctx context.Context, id uuid.UUID, name string, settings json.RawMessage) (dbsqlc.Library, error) {
	if _, err := s.Require(ctx, nil, id, authz.RoleAdmin); err != nil {
		return dbsqlc.Library{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 200 || !json.Valid(settings) || len(settings) > 16<<10 {
		return dbsqlc.Library{}, ErrInvalid
	}
	return s.q.UpdateLibrary(ctx, dbsqlc.UpdateLibraryParams{ID: id, Name: name, Settings: settings})
}

// ---- places

// PlaceInput describes a place.
type PlaceInput struct {
	ParentID *uuid.UUID
	Name     string
	Kind     string
	Position int
}

func validPlace(in PlaceInput) bool {
	n := strings.TrimSpace(in.Name)
	return n != "" && len(n) <= 200 && len(in.Kind) <= 40
}

// CreatePlace adds a place (editors and administrators).
func (s *Service) CreatePlace(ctx context.Context, libraryID uuid.UUID, in PlaceInput) (dbsqlc.Place, error) {
	if _, err := s.Require(ctx, nil, libraryID, authz.RoleEdit); err != nil {
		return dbsqlc.Place{}, err
	}
	if !validPlace(in) {
		return dbsqlc.Place{}, ErrInvalid
	}
	if err := s.checkParent(ctx, libraryID, uuid.Nil, in.ParentID); err != nil {
		return dbsqlc.Place{}, err
	}
	kind := in.Kind
	if kind == "" {
		kind = "place"
	}
	return s.q.CreatePlace(ctx, dbsqlc.CreatePlaceParams{LibraryID: libraryID, ParentID: in.ParentID,
		Name: strings.TrimSpace(in.Name), Kind: kind, Position: int32(in.Position)})
}

// checkParent makes sure the parent is in the same library and the move creates no cycle.
func (s *Service) checkParent(ctx context.Context, libraryID, self uuid.UUID, parent *uuid.UUID) error {
	for cur, depth := parent, 0; cur != nil; depth++ {
		if depth > 20 || (self != uuid.Nil && *cur == self) {
			return ErrInvalid
		}
		p, err := s.q.GetPlace(ctx, *cur)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && p.LibraryID != libraryID) {
			return ErrInvalid
		}
		if err != nil {
			return fmt.Errorf("library: parent: %w", err)
		}
		cur = p.ParentID
	}
	return nil
}

// ListPlaces returns the places of a library.
func (s *Service) ListPlaces(ctx context.Context, libraryID uuid.UUID) ([]dbsqlc.Place, error) {
	if _, err := s.Require(ctx, nil, libraryID, authz.RoleRead); err != nil {
		return nil, err
	}
	return s.q.ListPlaces(ctx, libraryID)
}

// UpdatePlace changes a place.
func (s *Service) UpdatePlace(ctx context.Context, placeID uuid.UUID, in PlaceInput) (dbsqlc.Place, error) {
	pl, err := s.q.GetPlace(ctx, placeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return pl, ErrNotFound
	}
	if err != nil {
		return pl, err
	}
	if _, err := s.Require(ctx, nil, pl.LibraryID, authz.RoleEdit); err != nil {
		return dbsqlc.Place{}, err
	}
	if !validPlace(in) {
		return dbsqlc.Place{}, ErrInvalid
	}
	if err := s.checkParent(ctx, pl.LibraryID, placeID, in.ParentID); err != nil {
		return dbsqlc.Place{}, err
	}
	kind := in.Kind
	if kind == "" {
		kind = pl.Kind
	}
	return s.q.UpdatePlace(ctx, dbsqlc.UpdatePlaceParams{ID: placeID, Name: strings.TrimSpace(in.Name), Kind: kind,
		Position: int32(in.Position), ParentID: in.ParentID})
}

// DeletePlace removes a place (its copies become "no place").
func (s *Service) DeletePlace(ctx context.Context, placeID uuid.UUID) error {
	pl, err := s.q.GetPlace(ctx, placeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err := s.Require(ctx, nil, pl.LibraryID, authz.RoleEdit); err != nil {
		return err
	}
	return s.q.DeletePlace(ctx, placeID)
}

// ---- instances

// InstanceInput describes a copy.
type InstanceInput struct {
	EditionID  uuid.UUID
	PlaceID    *uuid.UUID
	Kind       string
	Condition  string
	Notes      string
	AcquiredOn *time.Time
}

func (in InstanceInput) validate() error {
	if in.Kind == "" {
		in.Kind = "print"
	}
	if (in.Kind != "print" && in.Kind != "ebook") || len(in.Condition) > 40 || len(in.Notes) > 10000 {
		return ErrInvalid
	}
	return nil
}

func (s *Service) checkPlace(ctx context.Context, q *dbsqlc.Queries, libraryID uuid.UUID, placeID *uuid.UUID) error {
	if placeID == nil {
		return nil
	}
	pl, err := q.GetPlace(ctx, *placeID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && pl.LibraryID != libraryID) {
		return ErrInvalid
	}
	return err
}

// AddInstance adds a copy to a library. A non-empty idemKey makes the call safe to replay: the
// same key with the same input returns the original copy, a different input is an error.
func (s *Service) AddInstance(ctx context.Context, libraryID uuid.UUID, in InstanceInput, idemKey string) (dbsqlc.Instance, error) {
	p, err := s.Require(ctx, nil, libraryID, authz.RoleEdit)
	if err != nil {
		return dbsqlc.Instance{}, err
	}
	if err := in.validate(); err != nil {
		return dbsqlc.Instance{}, err
	}
	if in.Kind == "" {
		in.Kind = "print"
	}
	cond := in.Condition
	if cond == "" {
		cond = "ok"
	}
	hash := sha256.Sum256(fmt.Appendf(nil, "add|%s|%v", libraryID, in))

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return dbsqlc.Instance{}, fmt.Errorf("library: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	if idemKey != "" {
		if len(idemKey) < 8 || len(idemKey) > 128 {
			return dbsqlc.Instance{}, ErrInvalid
		}
		// Serialize concurrent replays of the same key.
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", p.UserID.String()+idemKey); err != nil {
			return dbsqlc.Instance{}, fmt.Errorf("library: lock: %w", err)
		}
		prev, err := q.GetIdempotency(ctx, dbsqlc.GetIdempotencyParams{UserID: p.UserID, Key: idemKey})
		if err == nil {
			if string(prev.RequestHash) != string(hash[:]) {
				return dbsqlc.Instance{}, ErrIdempotencyConflict
			}
			var inst dbsqlc.Instance
			if err := json.Unmarshal(prev.ResponseBody, &inst); err != nil {
				return dbsqlc.Instance{}, fmt.Errorf("library: stored response: %w", err)
			}
			return inst, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return dbsqlc.Instance{}, fmt.Errorf("library: idempotency: %w", err)
		}
	}
	if err := s.checkPlace(ctx, q, libraryID, in.PlaceID); err != nil {
		return dbsqlc.Instance{}, err
	}
	if _, err := q.GetEdition(ctx, in.EditionID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return dbsqlc.Instance{}, ErrInvalid
		}
		return dbsqlc.Instance{}, err
	}
	inst, err := q.CreateInstance(ctx, dbsqlc.CreateInstanceParams{LibraryID: libraryID, EditionID: in.EditionID,
		PlaceID: in.PlaceID, Kind: in.Kind, Condition: cond, Notes: in.Notes, AcquiredOn: in.AcquiredOn, CreatedBy: &p.UserID})
	if err != nil {
		return inst, fmt.Errorf("library: create instance: %w", err)
	}
	if err := q.AddChange(ctx, dbsqlc.AddChangeParams{LibraryID: libraryID, Entity: "instance", EntityID: inst.ID, Op: "upsert"}); err != nil {
		return inst, fmt.Errorf("library: change: %w", err)
	}
	if idemKey != "" {
		body, _ := json.Marshal(inst)
		if err := q.PutIdempotency(ctx, dbsqlc.PutIdempotencyParams{UserID: p.UserID, Key: idemKey,
			RequestHash: hash[:], ResponseStatus: 201, ResponseBody: body}); err != nil {
			return inst, fmt.Errorf("library: store idempotency: %w", err)
		}
	}
	return inst, tx.Commit(ctx)
}

func (s *Service) instanceFor(ctx context.Context, id uuid.UUID, need authz.Role) (dbsqlc.Instance, error) {
	inst, err := s.q.GetInstance(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return inst, ErrNotFound
	}
	if err != nil {
		return inst, fmt.Errorf("library: instance: %w", err)
	}
	if _, err := s.Require(ctx, nil, inst.LibraryID, need); err != nil {
		return dbsqlc.Instance{}, err
	}
	return inst, nil
}

// GetInstance returns one copy.
func (s *Service) GetInstance(ctx context.Context, id uuid.UUID) (dbsqlc.Instance, error) {
	return s.instanceFor(ctx, id, authz.RoleRead)
}

// UpdateInstance changes place, kind, condition, notes or acquisition date.
func (s *Service) UpdateInstance(ctx context.Context, id uuid.UUID, in InstanceInput) (dbsqlc.Instance, error) {
	cur, err := s.instanceFor(ctx, id, authz.RoleEdit)
	if err != nil {
		return cur, err
	}
	if err := in.validate(); err != nil {
		return cur, err
	}
	if err := s.checkPlace(ctx, s.q, cur.LibraryID, in.PlaceID); err != nil {
		return cur, err
	}
	kind, cond := in.Kind, in.Condition
	if kind == "" {
		kind = cur.Kind
	}
	if cond == "" {
		cond = cur.Condition
	}
	return s.withChange(ctx, cur.LibraryID, id, "upsert", func(q *dbsqlc.Queries) (dbsqlc.Instance, error) {
		return q.UpdateInstance(ctx, dbsqlc.UpdateInstanceParams{ID: id, PlaceID: in.PlaceID, Kind: kind,
			Condition: cond, Notes: in.Notes, AcquiredOn: in.AcquiredOn})
	})
}

// DeleteInstance soft-deletes a copy (it can be restored).
func (s *Service) DeleteInstance(ctx context.Context, id uuid.UUID) error {
	cur, err := s.instanceFor(ctx, id, authz.RoleEdit)
	if err != nil {
		return err
	}
	_, err = s.withChange(ctx, cur.LibraryID, id, "delete", func(q *dbsqlc.Queries) (dbsqlc.Instance, error) {
		_, err := q.SoftDeleteInstance(ctx, id)
		return cur, err
	})
	return err
}

// RestoreInstance undoes a soft delete.
func (s *Service) RestoreInstance(ctx context.Context, id uuid.UUID) error {
	var libID uuid.UUID
	if err := s.pool.QueryRow(ctx, "SELECT library_id FROM instances WHERE id = $1 AND deleted_at IS NOT NULL", id).Scan(&libID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if _, err := s.Require(ctx, nil, libID, authz.RoleEdit); err != nil {
		return err
	}
	_, err := s.withChange(ctx, libID, id, "upsert", func(q *dbsqlc.Queries) (dbsqlc.Instance, error) {
		_, err := q.RestoreInstance(ctx, id)
		return dbsqlc.Instance{}, err
	})
	return err
}

func (s *Service) withChange(ctx context.Context, libID, id uuid.UUID, op string, fn func(*dbsqlc.Queries) (dbsqlc.Instance, error)) (dbsqlc.Instance, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return dbsqlc.Instance{}, fmt.Errorf("library: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	inst, err := fn(q)
	if err != nil {
		return inst, fmt.Errorf("library: write: %w", err)
	}
	if err := q.AddChange(ctx, dbsqlc.AddChangeParams{LibraryID: libID, Entity: "instance", EntityID: id, Op: op}); err != nil {
		return inst, fmt.Errorf("library: change: %w", err)
	}
	return inst, tx.Commit(ctx)
}

// ReplayedInstance returns the copy created by an earlier AddInstance call that used the same
// idempotency key, or nil when the key is unknown. It lets callers answer a replay without
// re-evaluating state that the first call changed (for example the "already owned" check).
func (s *Service) ReplayedInstance(ctx context.Context, idemKey string) (*dbsqlc.Instance, error) {
	p, err := authz.From(ctx)
	if err != nil {
		return nil, err
	}
	prev, err := s.q.GetIdempotency(ctx, dbsqlc.GetIdempotencyParams{UserID: p.UserID, Key: idemKey})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("library: idempotency: %w", err)
	}
	var inst dbsqlc.Instance
	if err := json.Unmarshal(prev.ResponseBody, &inst); err != nil {
		return nil, fmt.Errorf("library: stored response: %w", err)
	}
	return &inst, nil
}
