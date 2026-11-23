package providers

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// BnF queries the Bibliothèque nationale de France SRU service and reads UNIMARC.
type BnF struct {
	Fetcher Fetcher
	BaseURL string // default https://catalogue.bnf.fr/api/SRU
}

// Name implements Provider.
func (BnF) Name() string { return "bnf" }

type sruResponse struct {
	Records []struct {
		Data struct {
			Record unimarc `xml:"record"`
		} `xml:"recordData"`
	} `xml:"records>record"`
}

type unimarc struct {
	Data []struct {
		Tag string `xml:"tag,attr"`
		Sub []struct {
			Code  string `xml:"code,attr"`
			Value string `xml:",chardata"`
		} `xml:"subfield"`
	} `xml:"datafield"`
}

func (u unimarc) sub(tag, code string) []string {
	var out []string
	for _, f := range u.Data {
		if f.Tag != tag {
			continue
		}
		for _, s := range f.Sub {
			if s.Code == code {
				out = append(out, clean(s.Value))
			}
		}
	}
	return out
}

func (u unimarc) first(tag, code string) string {
	if v := u.sub(tag, code); len(v) > 0 {
		return v[0]
	}
	return ""
}

var pagesRe = regexp.MustCompile(`(\d{1,5})\s*(p\.|pages|p\b)`)

// Lookup implements Provider.
func (b BnF) Lookup(ctx context.Context, isbn13 string) (*Record, error) {
	base := b.BaseURL
	if base == "" {
		base = "https://catalogue.bnf.fr/api/SRU"
	}
	q := url.Values{}
	q.Set("version", "1.2")
	q.Set("operation", "searchRetrieve")
	q.Set("query", fmt.Sprintf(`bib.isbn adj "%s"`, isbn13))
	q.Set("recordSchema", "unimarcxchange")
	q.Set("maximumRecords", "1")
	resp, err := b.Fetcher.Get(ctx, base+"?"+q.Encode())
	if err != nil {
		return nil, fmt.Errorf("%w: bnf: %w", ErrUpstream, err)
	}
	if resp.Status != 200 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%w: bnf: status %d", ErrUpstream, resp.Status)
	}
	body, err := readBody(resp)
	if err != nil {
		return nil, fmt.Errorf("%w: bnf: %w", ErrUpstream, err)
	}
	return parseBnF(body)
}

func parseBnF(body []byte) (*Record, error) {
	var sru sruResponse
	dec := xml.NewDecoder(strings.NewReader(string(body)))
	dec.Strict = false
	// encoding/xml never resolves external entities, so XXE is not possible.
	if err := dec.Decode(&sru); err != nil {
		return nil, fmt.Errorf("%w: bnf: bad XML", ErrUpstream)
	}
	if len(sru.Records) == 0 {
		return nil, nil
	}
	m := sru.Records[0].Data.Record
	title := m.first("200", "a")
	if title == "" {
		return nil, nil
	}
	r := &Record{
		Title:     title,
		Subtitle:  m.first("200", "e"),
		Publisher: m.first("210", "c"),
		Language:  iso2(m.first("101", "a")),
	}
	if y := yearRe.FindString(m.first("210", "d")); y != "" {
		r.Year, _ = strconv.Atoi(y)
	}
	if pm := pagesRe.FindStringSubmatch(m.first("215", "a")); pm != nil {
		r.Pages, _ = strconv.Atoi(pm[1])
	}
	r.Description = m.first("330", "a")
	r.Series = m.first("225", "a")
	if v := m.first("225", "v"); v != "" {
		if n, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			r.SeriesPos = &n
		}
	}
	// 700/701/702: personal names, $a surname and $b forename.
	for _, f := range m.Data {
		if f.Tag != "700" && f.Tag != "701" && f.Tag != "702" {
			continue
		}
		var last, first string
		for _, s := range f.Sub {
			switch s.Code {
			case "a":
				last = clean(s.Value)
			case "b":
				first = clean(s.Value)
			}
		}
		if name := strings.TrimSpace(first + " " + last); name != "" {
			r.Authors = append(r.Authors, name)
		}
	}
	return r, nil
}

// iso2 maps the common ISO 639-2 codes used by UNIMARC to ISO 639-1.
func iso2(code string) string {
	switch strings.ToLower(code) {
	case "fre", "fra":
		return "fr"
	case "eng":
		return "en"
	case "ger", "deu":
		return "de"
	case "spa":
		return "es"
	case "ita":
		return "it"
	case "por":
		return "pt"
	case "dut", "nld":
		return "nl"
	case "lat":
		return "la"
	}
	return ""
}
