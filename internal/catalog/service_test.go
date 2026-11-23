package catalog_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/larueli/biblisann/internal/auth"
	"github.com/larueli/biblisann/internal/catalog"
	"github.com/larueli/biblisann/internal/dbtest"
	"github.com/larueli/biblisann/internal/providers"
	"github.com/larueli/biblisann/internal/safehttp"
	"github.com/larueli/biblisann/internal/storage"
)

type fakeProvider struct {
	name  string
	rec   *providers.Record
	calls atomic.Int32
}

func (f *fakeProvider) Name() string { return f.name }
func (f *fakeProvider) Lookup(context.Context, string) (*providers.Record, error) {
	f.calls.Add(1)
	return f.rec, nil
}

type fakeFetcher struct{ body []byte }

func (f fakeFetcher) Get(context.Context, string) (*safehttp.Response, error) {
	return &safehttp.Response{Status: 200, Body: io.NopCloser(bytes.NewReader(f.body))}, nil
}

func tinyPNG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 20, 30))
	for x := range 20 {
		for y := range 30 {
			img.Set(x, y, color.RGBA{R: 10, G: 120, B: 200, A: 255})
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

func setup(t *testing.T, ps ...providers.Provider) (*catalog.Service, uuid.UUID) {
	t.Helper()
	pool := dbtest.Pool(t)
	authSvc, _ := auth.NewService(pool, auth.RegistrationOpen)
	u, err := authSvc.Register(context.Background(), "a@example.org", "tulips in the garden", "A")
	if err != nil {
		t.Fatal(err)
	}
	covers, _ := storage.NewLocal(t.TempDir())
	svc := catalog.NewService(pool, catalog.Config{Providers: ps, Covers: covers,
		Fetcher: fakeFetcher{tinyPNG()}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	return svc, u.ID
}

func TestLookupCachesAndCreatesEdition(t *testing.T) {
	ctx := context.Background()
	bnf := &fakeProvider{name: "bnf", rec: &providers.Record{Title: "Le Petit Prince", Authors: []string{"Antoine de Saint-Exupéry"}, Language: "fr"}}
	ol := &fakeProvider{name: "openlibrary", rec: &providers.Record{Title: "The Little Prince", Pages: 93, CoverURL: "https://covers.example/x.jpg"}}
	svc, uid := setup(t, bnf, ol)

	res, err := svc.Lookup(ctx, "978-2-07-061275-8")
	if err != nil {
		t.Fatal(err)
	}
	if res.Draft == nil || res.Draft.Title != "Le Petit Prince" || res.Draft.Pages != 93 || res.Draft.ISBN13 != "9782070612758" {
		t.Fatalf("draft: %+v", res.Draft)
	}
	if _, err := svc.Lookup(ctx, "9782070612758"); err != nil {
		t.Fatal(err)
	}
	if bnf.calls.Load() != 1 || ol.calls.Load() != 1 {
		t.Errorf("provider cache not used: %d %d", bnf.calls.Load(), ol.calls.Load())
	}

	// Per-request provider selection: only openlibrary.
	res, _ = svc.Lookup(ctx, "9782070612758", &catalog.LookupSettings{Providers: []string{"openlibrary"}})
	if res.Draft.Title != "The Little Prince" {
		t.Errorf("request override ignored: %+v", res.Draft)
	}

	d := res.Draft
	e, created, err := svc.Create(ctx, uid, catalog.EditionInput{ISBN: d.ISBN13, Title: d.Title, Authors: d.Authors, Pages: d.Pages}, d.CoverURL, d.Provenance)
	if err != nil || !created {
		t.Fatalf("create: %v %v", created, err)
	}
	if e.CoverID == nil {
		t.Error("cover should have been fetched and stored")
	} else if rc, err := svc.CoverReader(ctx, *e.CoverID, true); err != nil {
		t.Errorf("cover reader: %v", err)
	} else {
		_ = rc.Close()
	}

	// Second lookup is answered locally, no provider call.
	before := bnf.calls.Load() + ol.calls.Load()
	res, _ = svc.Lookup(ctx, "9782070612758")
	if res.Existing == nil || bnf.calls.Load()+ol.calls.Load() != before {
		t.Error("existing edition must short-circuit external lookups")
	}
	if _, created, _ := svc.Create(ctx, uid, catalog.EditionInput{ISBN: "9782070612758", Title: "x"}, "", nil); created {
		t.Error("same ISBN must reuse the edition")
	}
}

func TestCreateManualWithoutISBNAndWorkReuse(t *testing.T) {
	svc, uid := setup(t)
	ctx := context.Background()
	a, c1, err := svc.Create(ctx, uid, catalog.EditionInput{Title: "Zine", Authors: []string{"Me"}}, "", nil)
	if err != nil || !c1 {
		t.Fatal(err)
	}
	b, c2, err := svc.Create(ctx, uid, catalog.EditionInput{Title: "ZINE", Authors: []string{"Me"}, Publisher: "Second press"}, "", nil)
	if err != nil || !c2 {
		t.Fatal(err)
	}
	if a.ID == b.ID || a.WorkID != b.WorkID {
		t.Error("two ISBN-less editions of the same work: distinct editions, same work")
	}
	if same, created, _ := svc.Create(ctx, uid, catalog.EditionInput{Title: "zine", Authors: []string{"Me"}}, "", nil); created || same.ID != a.ID {
		t.Error("an identical manual entry must reuse the edition")
	}
	if _, _, err := svc.Create(ctx, uid, catalog.EditionInput{Title: "  "}, "", nil); err == nil {
		t.Error("empty title accepted")
	}
	if _, err := svc.Lookup(ctx, "not-an-isbn"); err == nil {
		t.Error("invalid isbn accepted")
	}
	if strings.TrimSpace(a.SearchText) == "" {
		t.Error("search text must be populated")
	}
}
