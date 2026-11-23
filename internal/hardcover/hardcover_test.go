package hardcover

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/larueli/biblisann/internal/safehttp"
)

type fake struct {
	status int
	body   string
	req    *http.Request
}

func (f *fake) Do(r *http.Request) (*safehttp.Response, error) {
	f.req = r
	return &safehttp.Response{Status: f.status, Body: io.NopCloser(strings.NewReader(f.body))}, nil
}

func TestFetch(t *testing.T) {
	f := &fake{status: 200, body: `{"data":{"editions":[{"book":{"slug":"dune","rating":4.31,"ratings_count":1200}}]}}`}
	s := &Service{http: f, token: "tok", url: endpoint}
	r, v, link, err := s.Fetch(context.Background(), "9780441172719")
	if err != nil || r != 4.31 || v != 1200 || link != "https://hardcover.app/books/dune" {
		t.Fatalf("%v %v %v %v", r, v, link, err)
	}
	if f.req.Header.Get("Authorization") != "Bearer tok" || f.req.Method != http.MethodPost {
		t.Errorf("request %v", f.req.Header)
	}
	for name, body := range map[string]string{"no editions": `{"data":{"editions":[]}}`, "no rating": `{"data":{"editions":[{"book":{"slug":"x"}}]}}`} {
		s = &Service{http: &fake{status: 200, body: body}, url: endpoint}
		if _, _, _, err := s.Fetch(context.Background(), "x"); !errors.Is(err, ErrNoData) {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, f := range map[string]*fake{"500": {status: 500}, "graphql error": {status: 200, body: `{"errors":[{"message":"x"}]}`}, "garbage": {status: 200, body: `<html>`}} {
		if _, _, _, err := (&Service{http: f, url: endpoint}).Fetch(context.Background(), "x"); err == nil || errors.Is(err, ErrNoData) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
