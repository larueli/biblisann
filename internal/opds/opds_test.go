package opds_test

import (
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/larueli/biblisann/internal/auth"
	"github.com/larueli/biblisann/internal/authz"
	"github.com/larueli/biblisann/internal/catalog"
	"github.com/larueli/biblisann/internal/dbtest"
	"github.com/larueli/biblisann/internal/library"
	"github.com/larueli/biblisann/internal/opds"
	"github.com/larueli/biblisann/internal/tokens"
)

func get(t *testing.T, url string) (int, string, string) {
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), resp.Header.Get("Content-Type")
}

func TestOPDS(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)
	a, _ := auth.NewService(pool, auth.RegistrationOpen)
	u, _ := a.Register(ctx, "me@example.org", "tulips in the garden", "Me")
	uctx := authz.With(ctx, &authz.Principal{UserID: u.ID, Via: "session"})
	lib := library.NewService(pool)
	cat := catalog.NewService(pool, catalog.Config{})
	l, _ := lib.CreateLibrary(uctx, "Home & <Garden>")
	ed, _, _ := cat.Create(ctx, u.ID, catalog.EditionInput{ISBN: "9782070612758", Title: "Le Petit <b>Prince</b>", Authors: []string{"A & B"}}, "", nil)
	if _, err := lib.AddInstance(uctx, l.ID, library.InstanceInput{EditionID: ed.ID}, ""); err != nil {
		t.Fatal(err)
	}
	tk := tokens.NewService(pool)
	feed, _ := tk.Create(uctx, tokens.KindFeed, "opds", nil, nil, nil)
	pat, _ := tk.Create(uctx, tokens.KindPAT, "x", []string{"read"}, nil, nil)

	mux := http.NewServeMux()
	opds.New(tk, lib, cat, "https://books.example.org").Routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	st, body, ct := get(t, srv.URL+"/opds/"+feed.Token+"/")
	if st != 200 || !strings.Contains(ct, "kind=navigation") || !strings.Contains(body, "Home &amp; &lt;Garden&gt;") {
		t.Fatalf("root %d %s %s", st, ct, body)
	}
	st, body, ct = get(t, srv.URL+"/opds/"+feed.Token+"/libraries/"+l.ID.String())
	if st != 200 || !strings.Contains(ct, "kind=acquisition") {
		t.Fatalf("library %d %s", st, body)
	}
	if !strings.Contains(body, "Le Petit &lt;b&gt;Prince&lt;/b&gt;") || strings.Contains(body, "<b>") {
		t.Errorf("titles must be escaped: %s", body)
	}
	var parsed struct {
		Entries []struct{ Title string } `xml:"entry"`
	}
	if err := xml.Unmarshal([]byte(body), &parsed); err != nil || len(parsed.Entries) != 1 {
		t.Errorf("not valid Atom: %v", err)
	}
	// a personal access token must not open the catalog, and unknown tokens learn nothing
	for _, tok := range []string{pat.Token, "bb_feed_wrong", "x"} {
		if st, _, _ := get(t, srv.URL+"/opds/"+tok+"/"); st != 404 {
			t.Errorf("token %q: %d", tok, st)
		}
	}
	// another user's library is invisible even with a valid token
	b, _ := a.CreateUser(ctx, "b@example.org", "tulips in the garden", "B", false)
	bctx := authz.With(ctx, &authz.Principal{UserID: b.ID, Via: "session"})
	bf, _ := tk.Create(bctx, tokens.KindFeed, "opds", nil, nil, nil)
	if st, _, _ := get(t, srv.URL+"/opds/"+bf.Token+"/libraries/"+l.ID.String()); st != 404 {
		t.Errorf("foreign library: %d", st)
	}
}
