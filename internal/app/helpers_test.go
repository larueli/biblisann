package app

import (
	"net/http"
	"net/http/httptest"
)

func newRecorderServer(h http.Handler) http.Header {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	return rec.Header()
}
