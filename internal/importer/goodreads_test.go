package importer_test

import (
	"context"
	"strings"
	"testing"

	"github.com/larueli/biblisann/internal/auth"
	"github.com/larueli/biblisann/internal/authz"
	"github.com/larueli/biblisann/internal/catalog"
	"github.com/larueli/biblisann/internal/dbtest"
	"github.com/larueli/biblisann/internal/importer"
	"github.com/larueli/biblisann/internal/library"
	"github.com/larueli/biblisann/internal/lists"
	"github.com/larueli/biblisann/internal/reading"
)

const csvFile = "\ufeffBook Id,Title,Author,Additional Authors,ISBN,ISBN13,My Rating,Publisher,Number of Pages,Year Published,Date Read,Exclusive Shelf,My Review,Owned Copies\n" +
	`1,"Dune","Frank Herbert","","=""0441172717""","=""9780441172719""",5,Ace,412,1990,2024/03/05,read,"Superb, truly",1` + "\n" +
	`2,"The Hobbit","J.R.R. Tolkien","","=""""","=""""",0,,,,,to-read,,0` + "\n" +
	`3,"",,,,,,,,,,,,` + "\n" +
	`4,"Odd book","Someone","A, B","=""bad""","=""123""",3,,,,,currently-reading,,0` + "\n"

func TestGoodreads(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)
	a, _ := auth.NewService(pool, auth.RegistrationOpen)
	u, _ := a.Register(ctx, "me@example.org", "tulips in the garden", "Me")
	uctx := authz.With(ctx, &authz.Principal{UserID: u.ID, Via: "session"})
	lib := library.NewService(pool)
	l, _ := lib.CreateLibrary(uctx, "Home")
	rd := reading.NewService(pool)
	im := importer.New(pool, catalog.NewService(pool, catalog.Config{}), lib, rd, lists.NewService(pool, lib))

	rep, err := im.Goodreads(uctx, l.ID, strings.NewReader(csvFile))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Rows != 4 || rep.EditionsCreated != 3 || rep.InstancesAdded != 2 || rep.WishlistAdded != 1 || rep.Skipped != 1 || rep.Grades != 2 {
		t.Errorf("report: %+v", rep)
	}
	page, _ := lib.ListInstances(uctx, l.ID, "", 10)
	if len(page.Items) != 2 {
		t.Errorf("instances: %d", len(page.Items))
	}
	st, _ := rd.Stats(uctx)
	if st.ByStatus["read"] != 1 || st.ByStatus["to_read"] != 1 || st.ByStatus["reading"] != 1 || st.AverageGrade != 8 {
		t.Errorf("stats: %+v", st)
	}

	// importing the same file again adds no copy
	rep2, err := im.Goodreads(uctx, l.ID, strings.NewReader(csvFile))
	if err != nil || rep2.InstancesAdded != 0 || rep2.EditionsCreated != 0 {
		t.Errorf("re-import: %+v %v", rep2, err)
	}
	// not a Goodreads file
	if _, err := im.Goodreads(uctx, l.ID, strings.NewReader("a,b\n1,2\n")); err == nil {
		t.Error("wrong format accepted")
	}
	// read-only members cannot import
	bob, _ := a.Register(ctx, "bob@example.org", "tulips in the garden", "Bob")
	bctx := authz.With(ctx, &authz.Principal{UserID: bob.ID, Via: "session"})
	if _, err := im.Goodreads(bctx, l.ID, strings.NewReader(csvFile)); err == nil {
		t.Error("non-member import accepted")
	}
}
