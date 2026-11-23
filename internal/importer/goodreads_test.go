package importer_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

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

const storyGraphCSV = "Title,Authors,Contributors,ISBN/UID,Format,Read Status,Date Added,Last Date Read,Dates Read,Read Count,Moods,Pace,Star Rating,Review,Owned?\n" +
	`Dune,Frank Herbert,,9780441172719,paperback,read,2024/01/01,2024/03/05,2024/02/01-2024/03/05,1,,,4.75,"Superb, truly",Yes` + "\n" +
	`Hobbit,J.R.R. Tolkien,,,,to-read,2024/01/01,,,0,,,0,,No` + "\n" +
	`Quit book,"A, B",,,,did-not-finish,2024/01/01,,,0,,,2,,No` + "\n" +
	`Reading now,C,,,,currently-reading,2024/01/01,,,0,,,0,,Yes` + "\n"

const libraryThingTSV = "Book Id\tTitle\tPrimary Author\tPublication\tDate\tReview\tRating\tPage Count\tDate Read\tISBN\n" +
	"1\tDune\tHerbert, Frank\tAce, 1990\t1990\tGreat\t4.5\t412\t2024-03-05\t[0441172717]\n" +
	"2\tUnrated\tDoe, Jane\tSome Press (London), 2001\t2001\t\t0\t\t\t[]\n"

func setup(t *testing.T) (context.Context, *importer.Importer, *library.Service, *reading.Service, uuid.UUID) {
	ctx := context.Background()
	pool := dbtest.Pool(t)
	a, _ := auth.NewService(pool, auth.RegistrationOpen)
	u, _ := a.Register(ctx, "me@example.org", "tulips in the garden", "Me")
	uctx := authz.With(ctx, &authz.Principal{UserID: u.ID, Via: "session"})
	lib := library.NewService(pool)
	l, _ := lib.CreateLibrary(uctx, "Home")
	rd := reading.NewService(pool)
	return uctx, importer.New(pool, catalog.NewService(pool, catalog.Config{}), lib, rd, lists.NewService(pool, lib)), lib, rd, l.ID
}

func TestStoryGraph(t *testing.T) {
	ctx, im, lib, rd, libID := setup(t)
	rep, err := im.StoryGraph(ctx, libID, strings.NewReader(storyGraphCSV))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Rows != 4 || rep.EditionsCreated != 4 || rep.InstancesAdded != 2 || rep.WishlistAdded != 1 {
		t.Errorf("report: %+v", rep)
	}
	st, _ := rd.Stats(ctx)
	if st.ByStatus["read"] != 1 || st.ByStatus["to_read"] != 1 || st.ByStatus["abandoned"] != 1 || st.ByStatus["reading"] != 1 {
		t.Errorf("stats: %+v", st)
	}
	// 4.75 stars rounds to 10, 2 stars to 4: average 7
	if st.AverageGrade != 7 || st.Graded != 2 {
		t.Errorf("grades: %+v", st)
	}
	page, _ := lib.ListInstances(ctx, libID, "", 10)
	if len(page.Items) != 2 {
		t.Errorf("instances %d", len(page.Items))
	}
	if rep2, _ := im.StoryGraph(ctx, libID, strings.NewReader(storyGraphCSV)); rep2.InstancesAdded != 0 || rep2.EditionsCreated != 0 {
		t.Errorf("re-import: %+v", rep2)
	}
	if _, err := im.StoryGraph(ctx, libID, strings.NewReader("Title,Author\nx,y\n")); err == nil {
		t.Error("a Goodreads-looking file must not pass as StoryGraph")
	}
}

func TestLibraryThing(t *testing.T) {
	ctx, im, lib, rd, libID := setup(t)
	rep, err := im.LibraryThing(ctx, libID, strings.NewReader(libraryThingTSV))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Rows != 2 || rep.EditionsCreated != 2 || rep.InstancesAdded != 2 || rep.Grades != 1 {
		t.Errorf("report: %+v", rep)
	}
	page, _ := lib.ListInstances(ctx, libID, "", 10)
	var dune bool
	for _, i := range page.Items {
		if i.Title == "Dune" && len(i.Authors) == 1 && i.Authors[0] == "Frank Herbert" && i.Isbn13 != nil && *i.Isbn13 == "9780441172719" {
			dune = true
		}
	}
	if !dune {
		t.Errorf("Dune not imported with author and ISBN-13: %+v", page.Items)
	}
	if st, _ := rd.Stats(ctx); st.ByStatus["read"] != 1 || st.AverageGrade != 9 {
		t.Errorf("stats %+v", st)
	}
}
