package telemetry_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/larueli/biblisann/internal/telemetry"
)

func TestRecordCoverCheck(t *testing.T) {
	m := telemetry.New("test", nil)
	m.RecordCoverCheck(2, 3, 5, time.Unix(1700000000, 0))
	rec := httptest.NewRecorder()
	m.Handler("", true).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rec.Body.String()
	for _, want := range []string{
		`biblisann_cover_files_missing{kind="full"} 2`,
		`biblisann_cover_files_missing{kind="thumb"} 3`,
		"biblisann_cover_check_editions_affected 5",
		"biblisann_cover_check_last_run_timestamp_seconds 1.7e+09",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in metrics output", want)
		}
	}
}
