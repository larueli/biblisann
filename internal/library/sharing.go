package library

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/larueli/biblisann/internal/authz"
	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
)

// ErrLastAdmin is returned when a change would leave a library without administrator.
var ErrLastAdmin = errors.New("a library needs at least one administrator")

// ErrConflict is returned for duplicates (for example a group name).
var ErrConflict = errors.New("already exists")

// Grant is a role, or a custom profile when Role is "custom".
type Grant struct {
	Role      string
	ProfileID *uuid.UUID
}

func (s *Service) checkGrant(ctx context.Context, libraryID uuid.UUID, g Grant) error {
	switch g.Role {
	case "read", "edit", "admin":
		if g.ProfileID != nil {
			return ErrInvalid
		}
	case "custom":
		if g.ProfileID == nil {
			return ErrInvalid
		}
		pr, err := s.q.GetProfile(ctx, *g.ProfileID)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && pr.LibraryID != libraryID) {
			return ErrInvalid
		}
		return err
	default:
		return ErrInvalid
	}
	return nil
}

// ShareWithUser grants a registered user (by email) access to a library.
func (s *Service) ShareWithUser(ctx context.Context, libraryID uuid.UUID, email string, g Grant) error {
	if _, err := s.RequirePerm(ctx, nil, libraryID, authz.PermShare); err != nil {
		return err
	}
	if err := s.checkGrant(ctx, libraryID, g); err != nil {
		return err
	}
	u, err := s.q.GetUserByEmail(ctx, strings.TrimSpace(email))
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrInvalid
	}
	if err != nil {
		return err
	}
	if g.Role != "admin" {
		if err := s.keepAnAdmin(ctx, libraryID, u.ID); err != nil {
			return err
		}
	}
	return s.q.ShareWithUser(ctx, dbsqlc.ShareWithUserParams{LibraryID: libraryID, UserID: u.ID, Role: g.Role, ProfileID: g.ProfileID})
}

// keepAnAdmin fails when userID is the library's only direct administrator.
func (s *Service) keepAnAdmin(ctx context.Context, libraryID, userID uuid.UUID) error {
	role, err := s.q.GetLibraryRole(ctx, dbsqlc.GetLibraryRoleParams{LibraryID: libraryID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) || role != "admin" {
		return nil
	}
	n, err := s.q.CountLibraryAdmins(ctx, libraryID)
	if err != nil {
		return err
	}
	if n <= 1 {
		return ErrLastAdmin
	}
	return nil
}

// UnshareUser removes a user's direct access.
func (s *Service) UnshareUser(ctx context.Context, libraryID, userID uuid.UUID) error {
	if _, err := s.RequirePerm(ctx, nil, libraryID, authz.PermShare); err != nil {
		return err
	}
	if err := s.keepAnAdmin(ctx, libraryID, userID); err != nil {
		return err
	}
	n, err := s.q.UnshareUser(ctx, dbsqlc.UnshareUserParams{LibraryID: libraryID, UserID: userID})
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ShareWithGroup grants a group access to a library.
func (s *Service) ShareWithGroup(ctx context.Context, libraryID, groupID uuid.UUID, g Grant) error {
	if _, err := s.RequirePerm(ctx, nil, libraryID, authz.PermShare); err != nil {
		return err
	}
	if err := s.checkGrant(ctx, libraryID, g); err != nil {
		return err
	}
	if _, err := s.q.GetGroup(ctx, groupID); errors.Is(err, pgx.ErrNoRows) {
		return ErrInvalid
	}
	return s.q.ShareWithGroup(ctx, dbsqlc.ShareWithGroupParams{LibraryID: libraryID, GroupID: groupID, Role: g.Role, ProfileID: g.ProfileID})
}

// UnshareGroup removes a group's access.
func (s *Service) UnshareGroup(ctx context.Context, libraryID, groupID uuid.UUID) error {
	if _, err := s.RequirePerm(ctx, nil, libraryID, authz.PermShare); err != nil {
		return err
	}
	n, err := s.q.UnshareGroup(ctx, dbsqlc.UnshareGroupParams{LibraryID: libraryID, GroupID: groupID})
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Sharing lists who has access to a library.
type Sharing struct {
	Users  []dbsqlc.ListLibraryMembersRow
	Groups []dbsqlc.ListLibraryGroupSharesRow
}

// ListSharing returns members and group shares (administrators and sharers only).
func (s *Service) ListSharing(ctx context.Context, libraryID uuid.UUID) (Sharing, error) {
	if _, err := s.RequirePerm(ctx, nil, libraryID, authz.PermShare); err != nil {
		return Sharing{}, err
	}
	users, err := s.q.ListLibraryMembers(ctx, libraryID)
	if err != nil {
		return Sharing{}, err
	}
	groups, err := s.q.ListLibraryGroupShares(ctx, libraryID)
	return Sharing{Users: users, Groups: groups}, err
}

// ---- profiles

func validPerms(perms []string) bool {
	if len(perms) == 0 || len(perms) > len(authz.AllPermissions) {
		return false
	}
	for _, p := range perms {
		if !authz.ValidPermission(p) {
			return false
		}
	}
	return true
}

// CreateProfile defines a custom permission profile for a library (administrators only).
func (s *Service) CreateProfile(ctx context.Context, libraryID uuid.UUID, name string, perms []string) (dbsqlc.PermissionProfile, error) {
	if _, err := s.RequirePerm(ctx, nil, libraryID, authz.PermAdmin); err != nil {
		return dbsqlc.PermissionProfile{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 || !validPerms(perms) {
		return dbsqlc.PermissionProfile{}, ErrInvalid
	}
	pr, err := s.q.CreateProfile(ctx, dbsqlc.CreateProfileParams{LibraryID: libraryID, Name: name, Permissions: perms})
	return pr, mapUnique(err)
}

// ListProfiles returns the library's profiles.
func (s *Service) ListProfiles(ctx context.Context, libraryID uuid.UUID) ([]dbsqlc.PermissionProfile, error) {
	if _, err := s.RequirePerm(ctx, nil, libraryID, authz.PermShare); err != nil {
		return nil, err
	}
	return s.q.ListProfiles(ctx, libraryID)
}

// DeleteProfile removes a profile and every grant that used it.
func (s *Service) DeleteProfile(ctx context.Context, profileID uuid.UUID) error {
	pr, err := s.q.GetProfile(ctx, profileID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err := s.RequirePerm(ctx, nil, pr.LibraryID, authz.PermAdmin); err != nil {
		return err
	}
	return s.q.DeleteProfile(ctx, profileID)
}

func mapUnique(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrConflict
	}
	if err != nil {
		return fmt.Errorf("library: %w", err)
	}
	return nil
}

// ---- groups (instance administrators only)

func requireInstanceAdmin(ctx context.Context) error {
	p, err := authz.From(ctx)
	if err != nil {
		return err
	}
	if !p.IsAdmin || !p.HasScope("write") {
		return ErrForbidden
	}
	return nil
}

// CreateGroup creates a user group.
func (s *Service) CreateGroup(ctx context.Context, name string) (dbsqlc.Group, error) {
	if err := requireInstanceAdmin(ctx); err != nil {
		return dbsqlc.Group{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 200 {
		return dbsqlc.Group{}, ErrInvalid
	}
	g, err := s.q.CreateGroup(ctx, dbsqlc.CreateGroupParams{Name: name})
	return g, mapUnique(err)
}

// ListGroups lists groups; every signed-in user may see them to share libraries.
func (s *Service) ListGroups(ctx context.Context) ([]dbsqlc.Group, error) {
	if _, err := authz.From(ctx); err != nil {
		return nil, err
	}
	return s.q.ListGroups(ctx)
}

// DeleteGroup removes a group and its shares.
func (s *Service) DeleteGroup(ctx context.Context, id uuid.UUID) error {
	if err := requireInstanceAdmin(ctx); err != nil {
		return err
	}
	return s.q.DeleteGroup(ctx, id)
}

// SetGroupMember adds or removes a user (by email) in a group.
func (s *Service) SetGroupMember(ctx context.Context, groupID uuid.UUID, email string, member bool) error {
	if err := requireInstanceAdmin(ctx); err != nil {
		return err
	}
	u, err := s.q.GetUserByEmail(ctx, strings.TrimSpace(email))
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrInvalid
	}
	if err != nil {
		return err
	}
	if member {
		return s.q.AddGroupMember(ctx, dbsqlc.AddGroupMemberParams{GroupID: groupID, UserID: u.ID})
	}
	return s.q.RemoveGroupMember(ctx, dbsqlc.RemoveGroupMemberParams{GroupID: groupID, UserID: u.ID})
}

// GroupMembers lists the members of a group (instance administrators).
func (s *Service) GroupMembers(ctx context.Context, groupID uuid.UUID) ([]dbsqlc.ListGroupMembersRow, error) {
	if err := requireInstanceAdmin(ctx); err != nil {
		return nil, err
	}
	return s.q.ListGroupMembers(ctx, groupID)
}
