package api

import (
	"bytes"
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/larueli/biblisann/internal/authz"
)

func (s *Server) registerExtras(a huma.API) {
	type inventoryBody struct {
		Found      []string `json:"found" nullable:"false" doc:"Titles expected here and scanned."`
		Missing    []string `json:"missing" nullable:"false" doc:"Titles expected here but not scanned."`
		Unexpected []string `json:"unexpected" nullable:"false" doc:"Scanned ISBNs that belong to no copy expected here."`
		Unscanable []string `json:"unscannable" nullable:"false" doc:"Copies without ISBN: check them by eye."`
	}
	Register(a, huma.Operation{OperationID: "inventory", Method: http.MethodPost, Path: "/v1/places/{id}/inventory",
		Summary: "Inventory mode: compare scanned ISBNs with what a place (and the places below it) should contain",
		Tags:    []string{"places"}, Errors: []int{404, 422}}, PermLibraryRead,
		func(ctx context.Context, in *struct {
			ID   uuid.UUID `path:"id"`
			Body struct {
				ISBNs []string `json:"isbns" maxItems:"5000"`
			}
		}) (*struct{ Body inventoryBody }, error) {
			r, err := s.Library.Inventory(ctx, in.ID, in.Body.ISBNs)
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			return &struct{ Body inventoryBody }{inventoryBody{nonNil(r.Found), nonNil(r.Missing), nonNil(r.Unexpected), nonNil(r.Unscanable)}}, nil
		})

	type reportBody struct {
		Rows            int `json:"rows"`
		EditionsCreated int `json:"editions_created"`
		InstancesAdded  int `json:"instances_added"`
		ReadingStates   int `json:"reading_states"`
		Grades          int `json:"grades"`
		WishlistAdded   int `json:"wishlist_added"`
		Skipped         int `json:"skipped"`
		Errors          []struct {
			Row     int    `json:"row"`
			Message string `json:"message"`
		} `json:"errors" nullable:"false"`
	}
	Register(a, huma.Operation{OperationID: "import-goodreads", Method: http.MethodPost, Path: "/v1/libraries/{id}/import/goodreads",
		Summary:     "Import a Goodreads CSV export into a library (no metadata provider is queried)",
		Description: "Send the CSV file as the request body with Content-Type text/csv. Re-importing the same file does not duplicate copies.",
		Tags:        []string{"import"}, Errors: []int{413, 422}}, PermLibraryEdit,
		func(ctx context.Context, in *struct {
			ID      uuid.UUID `path:"id"`
			RawBody []byte    `contentType:"text/csv"`
		}) (*struct{ Body reportBody }, error) {
			if s.Importer == nil {
				return nil, huma.Error503ServiceUnavailable("import is not available")
			}
			rep, err := s.Importer.Goodreads(ctx, in.ID, bytes.NewReader(in.RawBody))
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			out := reportBody{Rows: rep.Rows, EditionsCreated: rep.EditionsCreated, InstancesAdded: rep.InstancesAdded,
				ReadingStates: rep.ReadingStates, Grades: rep.Grades, WishlistAdded: rep.WishlistAdded, Skipped: rep.Skipped}
			out.Errors = []struct {
				Row     int    `json:"row"`
				Message string `json:"message"`
			}{}
			for _, e := range rep.Errors {
				out.Errors = append(out.Errors, struct {
					Row     int    `json:"row"`
					Message string `json:"message"`
				}{e.Row, e.Message})
			}
			return &struct{ Body reportBody }{out}, nil
		})
}

// serveBackup streams a logical backup to an instance administrator. The archive contains
// password and token hashes: it must be stored like the database itself.
func (s *Server) serveBackup(w http.ResponseWriter, r *http.Request) {
	p, err := authz.From(r.Context())
	if err != nil {
		writeProblem(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if !p.IsAdmin || p.Via != "session" || s.Backup == nil {
		writeProblem(w, http.StatusNotFound, "not found") // never confirm the endpoint to others
		return
	}
	if r.Method != http.MethodGet {
		writeProblem(w, http.StatusMethodNotAllowed, "use GET")
		return
	}
	w.Header().Set("Content-Type", "application/x-tar")
	w.Header().Set("Content-Disposition", `attachment; filename="biblisann-backup-`+time.Now().UTC().Format("20060102-150405")+`.tar"`)
	w.Header().Set("Cache-Control", "no-store")
	if err := s.Backup(r.Context(), w); err != nil {
		s.Log.Error("backup failed", "error", err) // headers are sent: the client sees a truncated archive
	}
}
