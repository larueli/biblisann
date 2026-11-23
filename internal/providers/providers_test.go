package providers

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/larueli/biblisann/internal/safehttp"
)

type fakeFetcher struct {
	status int
	body   string
	url    string
}

func (f *fakeFetcher) Get(_ context.Context, u string) (*safehttp.Response, error) {
	f.url = u
	return &safehttp.Response{Status: f.status, Body: io.NopCloser(strings.NewReader(f.body))}, nil
}

const bnfXML = `<?xml version="1.0"?>
<srw:searchRetrieveResponse xmlns:srw="http://www.loc.gov/zing/srw/">
 <srw:records><srw:record><srw:recordData>
  <mxc:record xmlns:mxc="info:lc/xmlns/marcxchange-v2">
   <mxc:datafield tag="101"><mxc:subfield code="a">fre</mxc:subfield></mxc:datafield>
   <mxc:datafield tag="200"><mxc:subfield code="a">Le Petit Prince</mxc:subfield><mxc:subfield code="e">conte</mxc:subfield></mxc:datafield>
   <mxc:datafield tag="210"><mxc:subfield code="c">Gallimard</mxc:subfield><mxc:subfield code="d">DL 2007</mxc:subfield></mxc:datafield>
   <mxc:datafield tag="215"><mxc:subfield code="a">1 vol. (93 p.)</mxc:subfield></mxc:datafield>
   <mxc:datafield tag="225"><mxc:subfield code="a">Folio</mxc:subfield><mxc:subfield code="v">3</mxc:subfield></mxc:datafield>
   <mxc:datafield tag="700"><mxc:subfield code="a">Saint-Exupéry</mxc:subfield><mxc:subfield code="b">Antoine de</mxc:subfield></mxc:datafield>
  </mxc:record>
 </srw:recordData></srw:record></srw:records>
</srw:searchRetrieveResponse>`

func TestBnF(t *testing.T) {
	f := &fakeFetcher{status: 200, body: bnfXML}
	r, err := BnF{Fetcher: f}.Lookup(context.Background(), "9782070612758")
	if err != nil || r == nil {
		t.Fatalf("lookup: %v %v", r, err)
	}
	if r.Title != "Le Petit Prince" || r.Subtitle != "conte" || r.Publisher != "Gallimard" || r.Year != 2007 ||
		r.Pages != 93 || r.Language != "fr" || r.Series != "Folio" || r.SeriesPos == nil || *r.SeriesPos != 3 {
		t.Errorf("unexpected record %+v", r)
	}
	if len(r.Authors) != 1 || r.Authors[0] != "Antoine de Saint-Exupéry" {
		t.Errorf("authors = %v", r.Authors)
	}
	if !strings.Contains(f.url, "9782070612758") || !strings.HasPrefix(f.url, "https://catalogue.bnf.fr/") {
		t.Errorf("url = %s", f.url)
	}
}

func TestBnFNotFoundAndErrors(t *testing.T) {
	empty := `<srw:searchRetrieveResponse xmlns:srw="x"><srw:records/></srw:searchRetrieveResponse>`
	if r, err := (BnF{Fetcher: &fakeFetcher{status: 200, body: empty}}).Lookup(context.Background(), "x"); r != nil || err != nil {
		t.Errorf("not found: %v %v", r, err)
	}
	if _, err := (BnF{Fetcher: &fakeFetcher{status: 503}}).Lookup(context.Background(), "x"); err == nil {
		t.Error("503 must be an error")
	}
	if _, err := (BnF{Fetcher: &fakeFetcher{status: 200, body: "<<<"}}).Lookup(context.Background(), "x"); err == nil {
		t.Error("garbage must be an error")
	}
	// External entities are never expanded.
	xxe := `<?xml version="1.0"?><!DOCTYPE a [<!ENTITY e SYSTEM "file:///etc/passwd">]><a>&e;</a>`
	r, _ := (BnF{Fetcher: &fakeFetcher{status: 200, body: xxe}}).Lookup(context.Background(), "x")
	if r != nil {
		t.Error("xxe payload must not yield a record")
	}
}

func TestOpenLibrary(t *testing.T) {
	body := `{"ISBN:9780306406157": {"title": "  A  Book\n", "subtitle": "sub", "authors": [{"name": "Jane Doe"}],
	  "publishers": [{"name": "ACME"}], "publish_date": "March 3, 1999", "number_of_pages": 321}}`
	f := &fakeFetcher{status: 200, body: body}
	r, err := OpenLibrary{Fetcher: f}.Lookup(context.Background(), "9780306406157")
	if err != nil || r == nil {
		t.Fatal(r, err)
	}
	if r.Title != "A Book" || r.Year != 1999 || r.Pages != 321 || r.Publisher != "ACME" || r.Authors[0] != "Jane Doe" {
		t.Errorf("record %+v", r)
	}
	if !strings.HasSuffix(strings.Split(r.CoverURL, "?")[0], "9780306406157-L.jpg") {
		t.Errorf("cover %s", r.CoverURL)
	}
	if r, _ := (OpenLibrary{Fetcher: &fakeFetcher{status: 200, body: "{}"}}).Lookup(context.Background(), "9780306406157"); r != nil {
		t.Error("empty object means not found")
	}
}

func TestStatic(t *testing.T) {
	path := t.TempDir() + "/books.json"
	if err := os.WriteFile(path, []byte(`{"9780306406157": {"title": "  Fixture  Book ", "authors": ["A"], "year": 2001}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := LoadStatic(path)
	if err != nil {
		t.Fatal(err)
	}
	r, _ := s.Lookup(context.Background(), "9780306406157")
	if r == nil || r.Title != "Fixture Book" || r.Year != 2001 {
		t.Errorf("record %+v", r)
	}
	if r, _ := s.Lookup(context.Background(), "9780000000002"); r != nil {
		t.Error("unknown isbn")
	}
	if _, err := LoadStatic(t.TempDir() + "/missing.json"); err == nil {
		t.Error("missing file must fail")
	}
}

const marcXML = `<?xml version="1.0"?>
<zs:searchRetrieveResponse xmlns:zs="http://docs.oasis-open.org/ns/search-ws/sruResponse">
 <zs:records><zs:record><zs:recordData>
  <record xmlns="http://www.loc.gov/MARC21/slim">
   <datafield tag="041" ind1=" " ind2=" "><subfield code="a">eng</subfield></datafield>
   <datafield tag="100" ind1="1" ind2=" "><subfield code="a">Herbert, Frank,</subfield></datafield>
   <datafield tag="245" ind1="1" ind2="0"><subfield code="a">Dune /</subfield><subfield code="b">the first book :</subfield></datafield>
   <datafield tag="264" ind1=" " ind2="1"><subfield code="b">Ace,</subfield><subfield code="c">1990.</subfield></datafield>
   <datafield tag="300" ind1=" " ind2=" "><subfield code="a">412 pages ;</subfield></datafield>
   <datafield tag="490" ind1="1" ind2=" "><subfield code="a">Dune chronicles ;</subfield><subfield code="v">1</subfield></datafield>
   <datafield tag="700" ind1="1" ind2=" "><subfield code="a">Doe, Jane</subfield></datafield>
  </record>
 </zs:recordData></zs:record></zs:records>
</zs:searchRetrieveResponse>`

func TestMarcProviders(t *testing.T) {
	f := &fakeFetcher{status: 200, body: marcXML}
	r, err := K10plus(f).Lookup(context.Background(), "9780441172719")
	if err != nil || r == nil {
		t.Fatal(r, err)
	}
	if r.Title != "Dune" || r.Subtitle != "the first book" || r.Publisher != "Ace" || r.Year != 1990 || r.Pages != 412 ||
		r.Language != "en" || r.Series != "Dune chronicles" || r.SeriesPos == nil || *r.SeriesPos != 1 {
		t.Errorf("record %+v", r)
	}
	if len(r.Authors) != 2 || r.Authors[0] != "Frank Herbert" || r.Authors[1] != "Jane Doe" {
		t.Errorf("authors %v", r.Authors)
	}
	if !strings.Contains(f.url, "sru.k10plus.de") || !strings.Contains(f.url, "pica.isb%3D9780441172719") {
		t.Errorf("url %s", f.url)
	}
	if LibraryOfCongress(f).Name() != "loc" {
		t.Error("loc name")
	}
	empty := &fakeFetcher{status: 200, body: `<zs:searchRetrieveResponse xmlns:zs="x"><zs:records/></zs:searchRetrieveResponse>`}
	if r, _ := LibraryOfCongress(empty).Lookup(context.Background(), "x"); r != nil {
		t.Error("empty result")
	}
}

func TestGoogleBooks(t *testing.T) {
	body := `{"items":[{"volumeInfo":{"title":"Dune","authors":["Frank Herbert"],"publisher":"Ace","publishedDate":"1990-09-01",
	  "pageCount":412,"language":"EN","description":"Sand.","imageLinks":{"thumbnail":"http://books.google.com/x.jpg"}}}]}`
	f := &fakeFetcher{status: 200, body: body}
	r, err := GoogleBooks{Fetcher: f, APIKey: "k"}.Lookup(context.Background(), "9780441172719")
	if err != nil || r == nil || r.Title != "Dune" || r.Year != 1990 || r.Language != "en" || r.CoverURL != "https://books.google.com/x.jpg" {
		t.Fatalf("%+v %v", r, err)
	}
	if !strings.Contains(f.url, "isbn%3A9780441172719") || !strings.Contains(f.url, "key=k") {
		t.Errorf("url %s", f.url)
	}
	if r, _ := (GoogleBooks{Fetcher: &fakeFetcher{status: 200, body: `{}`}}).Lookup(context.Background(), "x"); r != nil {
		t.Error("no items means not found")
	}
}

type authFetcher struct {
	fakeFetcher
	bearer string
}

func (a *authFetcher) GetWithAuth(ctx context.Context, u, bearer string) (*safehttp.Response, error) {
	a.bearer = bearer
	return a.Get(ctx, u)
}

func TestAddon(t *testing.T) {
	body := `{"title": " My  Addon Book ", "authors": ["A"], "cover_url": "http://insecure/x.jpg", "year": 2020}`
	f := &authFetcher{fakeFetcher: fakeFetcher{status: 200, body: body}}
	r, err := Addon{ProviderName: "mine", Fetcher: f, URL: "https://addon.example/lookup?x=1", Token: "tok"}.Lookup(context.Background(), "9780441172719")
	if err != nil || r == nil || r.Title != "My Addon Book" || r.Year != 2020 {
		t.Fatalf("%+v %v", r, err)
	}
	if r.CoverURL != "" {
		t.Error("non-https cover URL from a third party must be dropped")
	}
	if f.bearer != "tok" || !strings.Contains(f.url, "isbn=9780441172719") || !strings.Contains(f.url, "x=1") {
		t.Errorf("bearer %q url %s", f.bearer, f.url)
	}
	nf := &fakeFetcher{status: 404}
	if r, err := (Addon{ProviderName: "mine", Fetcher: nf, URL: "https://addon.example/l"}).Lookup(context.Background(), "x"); r != nil || err != nil {
		t.Errorf("404: %v %v", r, err)
	}
	// credentials are never sent through a fetcher that cannot do it safely
	if _, err := (Addon{ProviderName: "mine", Fetcher: nf, URL: "https://addon.example/l", Token: "t"}).Lookup(context.Background(), "x"); err == nil {
		t.Error("token with a plain fetcher must fail")
	}
}
