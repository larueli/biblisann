package providers

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// SRUMarc queries an SRU server that answers MARCXML (Library of Congress, K10plus, many national
// libraries). Query is a format string with one %s for the ISBN-13.
type SRUMarc struct {
	ProviderName string
	Fetcher      Fetcher
	Endpoint     string
	Query        string // for example `bath.isbn=%s`
}

// LibraryOfCongress returns the Library of Congress catalogue provider.
func LibraryOfCongress(f Fetcher) SRUMarc {
	return SRUMarc{ProviderName: "loc", Fetcher: f, Endpoint: "https://lx2.loc.gov/sru/lcdb", Query: "bath.isbn=%s"}
}

// K10plus returns the K10plus (German union catalogue) provider.
func K10plus(f Fetcher) SRUMarc {
	return SRUMarc{ProviderName: "k10plus", Fetcher: f, Endpoint: "https://sru.k10plus.de/k10plus", Query: "pica.isb=%s"}
}

// Name implements Provider.
func (s SRUMarc) Name() string { return s.ProviderName }

// Lookup implements Provider.
func (s SRUMarc) Lookup(ctx context.Context, isbn13 string) (*Record, error) {
	q := url.Values{}
	q.Set("version", "1.1")
	q.Set("operation", "searchRetrieve")
	q.Set("query", fmt.Sprintf(s.Query, isbn13))
	q.Set("maximumRecords", "1")
	q.Set("recordSchema", "marcxml")
	resp, err := s.Fetcher.Get(ctx, s.Endpoint+"?"+q.Encode())
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrUpstream, s.ProviderName, err)
	}
	if resp.Status != 200 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%w: %s: status %d", ErrUpstream, s.ProviderName, resp.Status)
	}
	body, err := readBody(resp)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrUpstream, s.ProviderName, err)
	}
	return parseMarc(body)
}

func parseMarc(body []byte) (*Record, error) {
	var sru sruResponse
	dec := xml.NewDecoder(strings.NewReader(string(body)))
	dec.Strict = false
	if err := dec.Decode(&sru); err != nil {
		return nil, fmt.Errorf("%w: bad XML", ErrUpstream)
	}
	if len(sru.Records) == 0 {
		return nil, nil
	}
	m := sru.Records[0].Data.Record
	trim := func(s string) string { return clean(strings.Trim(strings.TrimSpace(s), " /:;,.")) }
	title := trim(m.first("245", "a"))
	if title == "" {
		return nil, nil
	}
	r := &Record{Title: title, Subtitle: trim(m.first("245", "b")), Description: m.first("520", "a"), Language: iso2(m.first("041", "a"))}
	pub := m.first("264", "b")
	date := m.first("264", "c")
	if pub == "" {
		pub, date = m.first("260", "b"), m.first("260", "c")
	}
	r.Publisher = trim(pub)
	if y := yearRe.FindString(date); y != "" {
		r.Year, _ = strconv.Atoi(y)
	}
	if pm := pagesRe.FindStringSubmatch(m.first("300", "a")); pm != nil {
		r.Pages, _ = strconv.Atoi(pm[1])
	}
	r.Series = trim(m.first("490", "a"))
	if v := trim(m.first("490", "v")); v != "" {
		if n, err := strconv.ParseFloat(strings.TrimLeft(v, "v.bd no "), 64); err == nil {
			r.SeriesPos = &n
		}
	}
	for _, f := range m.Data {
		if f.Tag != "100" && f.Tag != "700" {
			continue
		}
		for _, s := range f.Sub {
			if s.Code == "a" {
				if name := personName(trim(s.Value)); name != "" {
					r.Authors = append(r.Authors, name)
				}
				break
			}
		}
	}
	return r, nil
}

// personName turns "Herbert, Frank" into "Frank Herbert".
func personName(s string) string {
	if last, first, ok := strings.Cut(s, ","); ok {
		return strings.TrimSpace(strings.TrimSpace(first) + " " + strings.TrimSpace(last))
	}
	return s
}
