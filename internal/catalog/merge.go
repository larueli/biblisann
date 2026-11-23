package catalog

import (
	"slices"
	"sort"
	"time"

	"github.com/larueli/biblisann/internal/providers"
)

// Provenance says which provider supplied a field and when.
type Provenance struct {
	Provider  string    `json:"provider"`
	FetchedAt time.Time `json:"fetched_at"`
}

// Draft is merged metadata that is not stored yet.
type Draft struct {
	ISBN13      string                `json:"isbn13,omitempty"`
	ISBN10      string                `json:"isbn10,omitempty"`
	Title       string                `json:"title"`
	Subtitle    string                `json:"subtitle,omitempty"`
	Authors     []string              `json:"authors"`
	Publisher   string                `json:"publisher,omitempty"`
	Year        int                   `json:"published_year,omitempty"`
	Language    string                `json:"language,omitempty"`
	Pages       int                   `json:"pages,omitempty"`
	Description string                `json:"description,omitempty"`
	Series      string                `json:"series_name,omitempty"`
	SeriesPos   *float64              `json:"series_position,omitempty"`
	CoverURL    string                `json:"-"`
	Provenance  map[string]Provenance `json:"provenance"`
}

// Result is one provider's answer.
type Result struct {
	Provider  string
	Record    *providers.Record
	FetchedAt time.Time
}

// Merge combines provider results field by field: the first result with a non-empty value wins.
// Results whose language is in preferred are tried first (stable, so provider order breaks ties).
func Merge(results []Result, preferred []string) *Draft {
	rs := slices.Clone(results)
	rs = slices.DeleteFunc(rs, func(r Result) bool { return r.Record == nil })
	if len(rs) == 0 {
		return nil
	}
	sort.SliceStable(rs, func(i, j int) bool {
		return slices.Contains(preferred, rs[i].Record.Language) && !slices.Contains(preferred, rs[j].Record.Language)
	})
	d := &Draft{Provenance: map[string]Provenance{}}
	set := func(field string, r Result, apply func()) {
		if _, done := d.Provenance[field]; done {
			return
		}
		apply()
		d.Provenance[field] = Provenance{Provider: r.Provider, FetchedAt: r.FetchedAt}
	}
	for _, r := range rs {
		rec := r.Record
		if rec.Title != "" {
			set("title", r, func() { d.Title = rec.Title })
		}
		if rec.Subtitle != "" {
			set("subtitle", r, func() { d.Subtitle = rec.Subtitle })
		}
		if len(rec.Authors) > 0 {
			set("authors", r, func() { d.Authors = slices.Clone(rec.Authors) })
		}
		if rec.Publisher != "" {
			set("publisher", r, func() { d.Publisher = rec.Publisher })
		}
		if rec.Year != 0 {
			set("published_year", r, func() { d.Year = rec.Year })
		}
		if rec.Language != "" {
			set("language", r, func() { d.Language = rec.Language })
		}
		if rec.Pages != 0 {
			set("pages", r, func() { d.Pages = rec.Pages })
		}
		if rec.Description != "" {
			set("description", r, func() { d.Description = rec.Description })
		}
		if rec.Series != "" {
			set("series_name", r, func() { d.Series = rec.Series; d.SeriesPos = rec.SeriesPos })
		}
		if rec.CoverURL != "" {
			set("cover", r, func() { d.CoverURL = rec.CoverURL })
		}
	}
	if d.Authors == nil {
		d.Authors = []string{}
	}
	return d
}
