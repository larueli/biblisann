package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// GoogleBooks queries the Google Books volumes API (the anonymous quota is small; set a key with
// BIBLISANN_GOOGLE_BOOKS_KEY for regular use).
type GoogleBooks struct {
	Fetcher Fetcher
	APIKey  string
	BaseURL string // default https://www.googleapis.com/books/v1/volumes
}

// Name implements Provider.
func (GoogleBooks) Name() string { return "google" }

type gbVolume struct {
	Items []struct {
		VolumeInfo struct {
			Title         string   `json:"title"`
			Subtitle      string   `json:"subtitle"`
			Authors       []string `json:"authors"`
			Publisher     string   `json:"publisher"`
			PublishedDate string   `json:"publishedDate"`
			Description   string   `json:"description"`
			PageCount     int      `json:"pageCount"`
			Language      string   `json:"language"`
			ImageLinks    struct {
				Thumbnail string `json:"thumbnail"`
			} `json:"imageLinks"`
		} `json:"volumeInfo"`
	} `json:"items"`
}

// Lookup implements Provider.
func (g GoogleBooks) Lookup(ctx context.Context, isbn13 string) (*Record, error) {
	base := g.BaseURL
	if base == "" {
		base = "https://www.googleapis.com/books/v1/volumes"
	}
	q := url.Values{}
	q.Set("q", "isbn:"+isbn13)
	q.Set("maxResults", "1")
	if g.APIKey != "" {
		q.Set("key", g.APIKey)
	}
	resp, err := g.Fetcher.Get(ctx, base+"?"+q.Encode())
	if err != nil {
		return nil, fmt.Errorf("%w: google: %w", ErrUpstream, err)
	}
	if resp.Status != 200 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%w: google: status %d", ErrUpstream, resp.Status)
	}
	body, err := readBody(resp)
	if err != nil {
		return nil, fmt.Errorf("%w: google: %w", ErrUpstream, err)
	}
	var v gbVolume
	if err := json.Unmarshal(body, &v); err != nil {
		return nil, fmt.Errorf("%w: google: bad JSON", ErrUpstream)
	}
	if len(v.Items) == 0 || strings.TrimSpace(v.Items[0].VolumeInfo.Title) == "" {
		return nil, nil
	}
	i := v.Items[0].VolumeInfo
	r := &Record{Title: clean(i.Title), Subtitle: clean(i.Subtitle), Publisher: clean(i.Publisher), Pages: i.PageCount,
		Description: strings.TrimSpace(i.Description), Language: strings.ToLower(clean(i.Language))}
	for _, a := range i.Authors {
		if a = clean(a); a != "" {
			r.Authors = append(r.Authors, a)
		}
	}
	if y := yearRe.FindString(i.PublishedDate); y != "" {
		r.Year, _ = strconv.Atoi(y)
	}
	if t := i.ImageLinks.Thumbnail; t != "" {
		r.CoverURL = strings.Replace(t, "http://", "https://", 1)
	}
	return r, nil
}

// Addon calls an out-of-process provider over HTTP. The contract is small (docs/dev/addons.md):
//
//	GET {URL}?isbn=<isbn13>   with optional "Authorization: Bearer <token>"
//	200 + JSON {"title": ..., "authors": [...], ...}   the fields of providers.Record
//	404                                                the book is unknown
type Addon struct {
	ProviderName string
	Fetcher      Fetcher
	URL          string
	Token        string
}

// Name implements Provider.
func (a Addon) Name() string { return a.ProviderName }

// Lookup implements Provider.
func (a Addon) Lookup(ctx context.Context, isbn13 string) (*Record, error) {
	u, err := url.Parse(a.URL)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: bad URL", ErrUpstream, a.ProviderName)
	}
	q := u.Query()
	q.Set("isbn", isbn13)
	u.RawQuery = q.Encode()
	var resp *safehttpResponse
	if a.Token != "" {
		if tf, ok := a.Fetcher.(interface {
			GetWithAuth(ctx context.Context, url, bearer string) (*safehttpResponse, error)
		}); ok {
			resp, err = tf.GetWithAuth(ctx, u.String(), a.Token)
		} else {
			return nil, fmt.Errorf("%w: %s: fetcher cannot send credentials", ErrUpstream, a.ProviderName)
		}
	} else {
		resp, err = a.Fetcher.Get(ctx, u.String())
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrUpstream, a.ProviderName, err)
	}
	if resp.Status == 404 {
		_ = resp.Body.Close()
		return nil, nil
	}
	if resp.Status != 200 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%w: %s: status %d", ErrUpstream, a.ProviderName, resp.Status)
	}
	body, err := readBody(resp)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrUpstream, a.ProviderName, err)
	}
	var r Record
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("%w: %s: bad JSON", ErrUpstream, a.ProviderName)
	}
	r.Title, r.Subtitle, r.Publisher = clean(r.Title), clean(r.Subtitle), clean(r.Publisher)
	if r.Title == "" {
		return nil, nil
	}
	// An addon is a third party: only https cover URLs, bounded lists.
	if !strings.HasPrefix(r.CoverURL, "https://") {
		r.CoverURL = ""
	}
	if len(r.Authors) > 50 {
		r.Authors = r.Authors[:50]
	}
	return &r, nil
}
