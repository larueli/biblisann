package catalog

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/larueli/biblisann/internal/authz"
)

func TestAdministrationIsDeniedByDefault(t *testing.T) {
	s := &Service{} // the check runs before any database access
	id := uuid.New()
	cases := map[string]context.Context{
		"anonymous":      context.Background(),
		"member session": authz.With(context.Background(), &authz.Principal{UserID: uuid.New(), Via: "session"}),
		"admin token":    authz.With(context.Background(), &authz.Principal{UserID: uuid.New(), IsAdmin: true, Via: "token"}),
	}
	for name, ctx := range cases {
		if _, err := s.Update(ctx, id, EditionPatch{}); err == nil {
			t.Errorf("%s: Update allowed", name)
		}
		if _, err := s.SetSyncExcluded(ctx, id, true); err == nil {
			t.Errorf("%s: SetSyncExcluded allowed", name)
		}
		if _, err := s.Refresh(ctx, id); err == nil {
			t.Errorf("%s: Refresh allowed", name)
		}
		if err := s.Delete(ctx, id); err == nil {
			t.Errorf("%s: Delete allowed", name)
		}
	}
	if err := s.Delete(cases["member session"], id); !errors.Is(err, authz.ErrNotFound) {
		t.Errorf("a member must be told 404, got %v", err)
	}
}
