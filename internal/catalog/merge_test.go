package catalog

import (
	"testing"
	"time"

	"github.com/larueli/biblisann/internal/providers"
)

func TestMergePerFieldProvenance(t *testing.T) {
	now := time.Now()
	d := Merge([]Result{
		{"bnf", &providers.Record{Title: "Le Petit Prince", Authors: []string{"A"}, Language: "fr"}, now},
		{"openlibrary", &providers.Record{Title: "The Little Prince", Pages: 93, CoverURL: "https://c/x.jpg"}, now},
		{"none", nil, now},
	}, nil)
	if d.Title != "Le Petit Prince" || d.Provenance["title"].Provider != "bnf" {
		t.Errorf("title: %+v", d)
	}
	if d.Pages != 93 || d.Provenance["pages"].Provider != "openlibrary" {
		t.Errorf("pages should come from the second provider: %+v", d)
	}
	if d.CoverURL == "" || d.Provenance["cover"].Provider != "openlibrary" {
		t.Errorf("cover: %+v", d)
	}
	if _, ok := d.Provenance["subtitle"]; ok {
		t.Error("no provenance for empty fields")
	}
}

func TestMergePreferredLanguageFirst(t *testing.T) {
	d := Merge([]Result{
		{"openlibrary", &providers.Record{Title: "English title", Language: "en"}, time.Now()},
		{"bnf", &providers.Record{Title: "Titre français", Language: "fr"}, time.Now()},
	}, []string{"fr"})
	if d.Title != "Titre français" {
		t.Errorf("title = %q", d.Title)
	}
}

func TestMergeNothing(t *testing.T) {
	if Merge(nil, nil) != nil || Merge([]Result{{"x", nil, time.Now()}}, nil) != nil {
		t.Error("no record means no draft")
	}
}

func TestResolveSettings(t *testing.T) {
	known := []string{"bnf", "openlibrary", "google"}
	req := &LookupSettings{Providers: []string{"google", "bnf"}}
	lib := &LookupSettings{Providers: []string{"openlibrary"}, Languages: []string{"fr"}}
	usr := &LookupSettings{Languages: []string{"en"}}

	got := ResolveSettings(known, nil, req, lib, usr, nil)
	if len(got.Providers) != 2 || got.Providers[0] != "google" || got.Languages[0] != "fr" {
		t.Errorf("request overrides providers, library supplies languages: %+v", got)
	}
	got = ResolveSettings(known, []string{"google"}, req, lib)
	if len(got.Providers) != 1 || got.Providers[0] != "bnf" {
		t.Errorf("admin-disabled provider must not come back: %+v", got)
	}
	got = ResolveSettings(known, nil, &LookupSettings{Providers: []string{"evil", "bnf", "bnf"}})
	if len(got.Providers) != 1 || got.Providers[0] != "bnf" {
		t.Errorf("unknown and duplicate providers dropped: %+v", got)
	}
	if got := ResolveSettings(known, nil); got.Providers != nil {
		t.Errorf("nothing set: %+v", got)
	}
}
