// Package opds serves a read-only OPDS 1.2 catalog (Atom) of a user's libraries for e-reader and
// catalog apps. Biblisann does not host book files, so entries carry metadata and covers but no
// acquisition links. Access uses a feed token in the path (apps cannot send custom headers
// reliably); the token only opens feeds and can be revoked.
package opds

import (
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/larueli/biblisann/internal/authz"
	"github.com/larueli/biblisann/internal/library"
)

// FeedAuth resolves a feed token to a user.
type FeedAuth interface {
	FeedUser(ctx context.Context, secret string) (uuid.UUID, error)
}

// Covers opens stored covers.
type Covers interface {
	CoverReader(ctx context.Context, id uuid.UUID, thumb bool) (io.ReadCloser, error)
}

// Handler serves /opds.
type Handler struct {
	auth   FeedAuth
	lib    *library.Service
	covers Covers
	base   string
}

// New creates the handler.
func New(a FeedAuth, l *library.Service, c Covers, baseURL string) *Handler {
	return &Handler{auth: a, lib: l, covers: c, base: strings.TrimSuffix(baseURL, "/")}
}

// Routes registers the endpoints.
func (h *Handler) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /opds/{token}/", h.root)
	mux.HandleFunc("GET /opds/{token}/libraries/{id}", h.libraryFeed)
	mux.HandleFunc("GET /opds/{token}/covers/{id}", h.cover)
}

type link struct {
	Rel  string `xml:"rel,attr,omitempty"`
	Href string `xml:"href,attr"`
	Type string `xml:"type,attr,omitempty"`
}

type author struct {
	Name string `xml:"name"`
}

type entry struct {
	Title   string   `xml:"title"`
	ID      string   `xml:"id"`
	Updated string   `xml:"updated"`
	Authors []author `xml:"author"`
	Content *struct {
		Type string `xml:"type,attr"`
		Text string `xml:",chardata"`
	} `xml:"content,omitempty"`
	Links []link `xml:"link"`
}

type feed struct {
	XMLName xml.Name `xml:"http://www.w3.org/2005/Atom feed"`
	Title   string   `xml:"title"`
	ID      string   `xml:"id"`
	Updated string   `xml:"updated"`
	Links   []link   `xml:"link"`
	Entries []entry  `xml:"entry"`
}

const (
	navType = "application/atom+xml;profile=opds-catalog;kind=navigation"
	acqType = "application/atom+xml;profile=opds-catalog;kind=acquisition"
)

func write(w http.ResponseWriter, kind string, f feed) {
	w.Header().Set("Content-Type", kind+"; charset=utf-8")
	w.Header().Set("Cache-Control", "private, max-age=60")
	w.Header().Set("Referrer-Policy", "no-referrer")
	_, _ = io.WriteString(w, xml.Header)
	enc := xml.NewEncoder(w)
	_ = enc.Encode(f)
}

// principal authenticates the path token and returns a read-only context.
func (h *Handler) principal(w http.ResponseWriter, r *http.Request) (context.Context, string, bool) {
	tok := r.PathValue("token")
	uid, err := h.auth.FeedUser(r.Context(), tok)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return nil, "", false
	}
	p := &authz.Principal{UserID: uid, Via: "token", Scopes: []string{"read"}}
	return authz.With(r.Context(), p), tok, true
}

func (h *Handler) root(w http.ResponseWriter, r *http.Request) {
	ctx, tok, ok := h.principal(w, r)
	if !ok {
		return
	}
	libs, err := h.lib.ListLibraries(ctx)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	self := h.base + "/opds/" + tok + "/"
	f := feed{Title: "Biblisann", ID: "urn:biblisann:root", Updated: now,
		Links: []link{{"self", self, navType}, {"start", self, navType}}}
	for _, l := range libs {
		f.Entries = append(f.Entries, entry{Title: l.Name, ID: "urn:uuid:" + l.ID.String(), Updated: now,
			Links: []link{{"subsection", self + "libraries/" + l.ID.String(), acqType}}})
	}
	write(w, navType, f)
}

func (h *Handler) libraryFeed(w http.ResponseWriter, r *http.Request) {
	ctx, tok, ok := h.principal(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	lib, err := h.lib.GetLibrary(ctx, id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	page, err := h.lib.ListInstances(ctx, id, r.URL.Query().Get("cursor"), 50)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	self := h.base + "/opds/" + tok + "/libraries/" + id.String()
	now := time.Now().UTC().Format(time.RFC3339)
	f := feed{Title: lib.Name, ID: "urn:uuid:" + id.String(), Updated: now,
		Links: []link{{"self", self, acqType}, {"start", h.base + "/opds/" + tok + "/", navType}}}
	if page.NextCursor != "" {
		f.Links = append(f.Links, link{"next", self + "?cursor=" + page.NextCursor, acqType})
	}
	for _, i := range page.Items {
		e := entry{Title: i.Title, ID: "urn:uuid:" + i.ID.String(), Updated: i.UpdatedAt.UTC().Format(time.RFC3339)}
		for _, a := range i.Authors {
			e.Authors = append(e.Authors, author{a})
		}
		if i.Isbn13 != nil {
			e.Content = &struct {
				Type string `xml:"type,attr"`
				Text string `xml:",chardata"`
			}{"text", "ISBN " + *i.Isbn13}
		}
		if i.CoverID != nil {
			c := h.base + "/opds/" + tok + "/covers/" + i.CoverID.String()
			e.Links = append(e.Links, link{"http://opds-spec.org/image/thumbnail", c + "?thumb=1", "image/jpeg"}, link{"http://opds-spec.org/image", c, "image/jpeg"})
		}
		f.Entries = append(f.Entries, e)
	}
	write(w, acqType, f)
}

func (h *Handler) cover(w http.ResponseWriter, r *http.Request) {
	ctx, _, ok := h.principal(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	rc, err := h.covers.CoverReader(ctx, id, r.URL.Query().Get("thumb") == "1")
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer func() { _ = rc.Close() }()
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	_, _ = io.Copy(w, rc)
}
