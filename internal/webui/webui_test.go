package webui

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestHandler(t *testing.T) {
	root := fstest.MapFS{
		"index.html":      {Data: []byte("<html>app</html>")},
		"assets/app-1.js": {Data: []byte("js")},
		"sw.js":           {Data: []byte("sw")},
	}
	h := NewHandler(root)
	tests := []struct {
		name, method, path string
		status             int
		cache              string
	}{
		{"root", "GET", "/", 200, "no-cache"},
		{"index file", "GET", "/index.html", 200, "no-cache"},
		{"spa route falls back", "GET", "/library/42", 200, "no-cache"},
		{"hashed asset immutable", "GET", "/assets/app-1.js", 200, "public, max-age=31536000, immutable"},
		{"service worker revalidates", "GET", "/sw.js", 200, "no-cache"},
		{"missing file is 404", "GET", "/assets/missing.js", 404, ""},
		{"traversal is cleaned to the SPA fallback", "GET", "/../../etc/passwd", 200, "no-cache"},
		{"post refused", "POST", "/", 405, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d", rec.Code, tt.status)
			}
			if tt.cache != "" && rec.Header().Get("Cache-Control") != tt.cache {
				t.Errorf("Cache-Control = %q, want %q", rec.Header().Get("Cache-Control"), tt.cache)
			}
		})
	}
}

func TestNotBuilt(t *testing.T) {
	rec := httptest.NewRecorder()
	NewHandler(fstest.MapFS{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}
