package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/larueli/biblisann/internal/auth"
	"github.com/larueli/biblisann/internal/authz"
	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
	"github.com/larueli/biblisann/internal/dbtest"
	"github.com/larueli/biblisann/internal/library"
	"github.com/larueli/biblisann/internal/loans"
)

func TestReminderRequiresCurrentPermission(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)
	a, err := auth.NewService(pool, auth.RegistrationOpen)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := a.CreateUser(ctx, "owner@example.org", "tulips in the garden", "Owner", true)
	if err != nil {
		t.Fatal(err)
	}
	member, err := a.CreateUser(ctx, "member@example.org", "tulips in the garden", "Member", false)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := dbsqlc.New(pool).VerifyUserEmail(ctx, dbsqlc.VerifyUserEmailParams{ID: member.ID, Email: member.Email}); err != nil || n != 1 {
		t.Fatalf("verify fixture address: %d %v", n, err)
	}
	ownerCtx := authz.With(ctx, &authz.Principal{UserID: owner.ID})
	memberCtx := authz.With(ctx, &authz.Principal{UserID: member.ID})
	lib := library.NewService(pool)
	l, err := lib.CreateLibrary(ownerCtx, "Library")
	if err != nil {
		t.Fatal(err)
	}
	if err := lib.ShareWithUser(ownerCtx, l.ID, member.Email, library.Grant{Role: "edit"}); err != nil {
		t.Fatal(err)
	}
	loan, err := loans.NewService(pool, lib).Borrow(memberCtx, loans.BorrowInput{LibraryID: l.ID, Title: "Book", Counterparty: "Library"})
	if err != nil {
		t.Fatal(err)
	}
	q := dbsqlc.New(pool)
	check := func(want bool) {
		t.Helper()
		got, err := reminderAllowed(ctx, q, lib, member.ID, loan.ID)
		if err != nil || got != want {
			t.Fatalf("allowed = %v, error = %v; want %v", got, err, want)
		}
	}
	check(true)
	now := time.Now()
	if err := q.SetUserDisabled(ctx, dbsqlc.SetUserDisabledParams{ID: member.ID, DisabledAt: &now}); err != nil {
		t.Fatal(err)
	}
	check(false)
	if err := q.SetUserDisabled(ctx, dbsqlc.SetUserDisabledParams{ID: member.ID}); err != nil {
		t.Fatal(err)
	}
	if err := lib.UnshareUser(ownerCtx, l.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	check(false)
}
