package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/larueli/biblisann/internal/library"
)

type syncBody struct {
	Cursor  int64                `json:"cursor" doc:"Store this and send it back as ?cursor= next time."`
	HasMore bool                 `json:"has_more" doc:"true: call again immediately with the new cursor."`
	Entries []library.IndexEntry `json:"entries" nullable:"false" doc:"Upserts, and tombstones (deleted=true) to remove locally."`
}

func (s *Server) registerSync(a huma.API) {
	Register(a, huma.Operation{OperationID: "sync-index", Method: http.MethodGet, Path: "/v1/libraries/{id}/sync",
		Summary: "Offline index: changes since a cursor, with tombstones", Tags: []string{"sync"}}, PermLibraryRead,
		func(ctx context.Context, in *struct {
			ID     uuid.UUID `path:"id"`
			Cursor int64     `query:"cursor" minimum:"0" doc:"0 for a full download."`
			Limit  int       `query:"limit" minimum:"1" maximum:"200" default:"200"`
		}) (*struct{ Body syncBody }, error) {
			idx, err := s.Library.SyncIndex(ctx, in.ID, in.Cursor, in.Limit)
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			if idx.Entries == nil {
				idx.Entries = []library.IndexEntry{}
			}
			return &struct{ Body syncBody }{syncBody{Cursor: idx.Cursor, HasMore: idx.HasMore, Entries: idx.Entries}}, nil
		})
}
