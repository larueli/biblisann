package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/larueli/biblisann/internal/lists"
)

type listBody struct {
	ID        uuid.UUID  `json:"id"`
	Name      string     `json:"name"`
	Kind      string     `json:"kind" enum:"custom,wishlist"`
	LibraryID *uuid.UUID `json:"library_id,omitempty" doc:"Set for a list shared through a library; unset for a personal list."`
	CreatedAt time.Time  `json:"created_at"`
}

type listItemBody struct {
	ID             uuid.UUID  `json:"id"`
	EditionID      *uuid.UUID `json:"edition_id,omitempty"`
	Title          string     `json:"title"`
	Authors        []string   `json:"authors" nullable:"false"`
	ISBN13         *string    `json:"isbn13,omitempty"`
	SeriesName     string     `json:"series_name,omitempty"`
	SeriesPosition *float64   `json:"series_position,omitempty"`
	Note           string     `json:"note,omitempty"`
	InLibrary      bool       `json:"in_library" doc:"I own this book, or another edition of it."`
}

func (s *Server) registerLists(a huma.API) {
	type idPath struct {
		ID uuid.UUID `path:"id"`
	}
	Register(a, huma.Operation{OperationID: "list-lists", Method: http.MethodGet, Path: "/v1/lists",
		Summary: "My lists (the default wishlist first) and the lists of my libraries", Tags: []string{"lists"}}, PermAuthenticated,
		func(ctx context.Context, _ *struct{}) (*struct{ Body []listBody }, error) {
			rows, err := s.Lists.All(ctx)
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			out := make([]listBody, len(rows))
			for i, l := range rows {
				out[i] = listBody{l.ID, l.Name, l.Kind, l.LibraryID, l.CreatedAt}
			}
			return &struct{ Body []listBody }{out}, nil
		})
	Register(a, huma.Operation{OperationID: "create-list", Method: http.MethodPost, Path: "/v1/lists",
		Summary: "Create a list (personal, or shared through a library)", Tags: []string{"lists"}, DefaultStatus: http.StatusCreated}, PermAuthenticated,
		func(ctx context.Context, in *struct {
			Body struct {
				Name      string     `json:"name" minLength:"1" maxLength:"200"`
				LibraryID *uuid.UUID `json:"library_id,omitempty"`
			}
		}) (*struct{ Body listBody }, error) {
			l, err := s.Lists.Create(ctx, in.Body.Name, in.Body.LibraryID)
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			return &struct{ Body listBody }{listBody{l.ID, l.Name, l.Kind, l.LibraryID, l.CreatedAt}}, nil
		})
	Register(a, huma.Operation{OperationID: "delete-list", Method: http.MethodDelete, Path: "/v1/lists/{id}",
		Summary: "Delete a list (not the wishlist)", Tags: []string{"lists"}, DefaultStatus: http.StatusNoContent}, PermAuthenticated,
		func(ctx context.Context, in *idPath) (*struct{}, error) {
			return nil, mapError(s.Log, s.Lists.Delete(ctx, in.ID))
		})
	Register(a, huma.Operation{OperationID: "list-items", Method: http.MethodGet, Path: "/v1/lists/{id}/items",
		Summary: "Items of a list, flagged when I already own them", Tags: []string{"lists"}}, PermAuthenticated,
		func(ctx context.Context, in *idPath) (*struct{ Body []listItemBody }, error) {
			rows, err := s.Lists.Items(ctx, in.ID)
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			out := make([]listItemBody, len(rows))
			for i, r := range rows {
				out[i] = listItemBody{r.ID, r.EditionID, r.Title, nonNil(r.Authors), r.Isbn13, r.SeriesName, r.SeriesPosition, r.Note, r.InLibrary}
			}
			return &struct{ Body []listItemBody }{out}, nil
		})
	Register(a, huma.Operation{OperationID: "add-list-item", Method: http.MethodPost, Path: "/v1/lists/{id}/items",
		Summary: "Add a book to a list (it does not have to be in a library)", Tags: []string{"lists"}, DefaultStatus: http.StatusCreated}, PermAuthenticated,
		func(ctx context.Context, in *struct {
			ID   uuid.UUID `path:"id"`
			Body struct {
				EditionID *uuid.UUID `json:"edition_id,omitempty" doc:"When set, title, authors and ISBN come from the edition."`
				Title     string     `json:"title,omitempty" maxLength:"1000"`
				Authors   []string   `json:"authors,omitempty" maxItems:"50"`
				ISBN13    string     `json:"isbn13,omitempty" maxLength:"13"`
				Note      string     `json:"note,omitempty" maxLength:"2000"`
			}
		}) (*struct{ Body listItemBody }, error) {
			it, err := s.Lists.AddItem(ctx, in.ID, lists.ItemInput{EditionID: in.Body.EditionID, Title: in.Body.Title,
				Authors: in.Body.Authors, ISBN13: in.Body.ISBN13, Note: in.Body.Note})
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			return &struct{ Body listItemBody }{listItemBody{it.ID, it.EditionID, it.Title, nonNil(it.Authors), it.Isbn13, it.SeriesName, it.SeriesPosition, it.Note, false}}, nil
		})
	Register(a, huma.Operation{OperationID: "remove-list-item", Method: http.MethodDelete, Path: "/v1/list-items/{id}",
		Summary: "Remove an item from its list", Tags: []string{"lists"}, DefaultStatus: http.StatusNoContent}, PermAuthenticated,
		func(ctx context.Context, in *idPath) (*struct{}, error) {
			return nil, mapError(s.Log, s.Lists.RemoveItem(ctx, in.ID))
		})

	type seriesBody struct {
		Name    string    `json:"name"`
		Owned   []float64 `json:"owned" nullable:"false"`
		Missing []int     `json:"missing" nullable:"false" doc:"Whole-number volumes below the highest owned one that I do not have."`
	}
	Register(a, huma.Operation{OperationID: "library-series", Method: http.MethodGet, Path: "/v1/libraries/{id}/series",
		Summary: "Series in a library with their missing volumes", Tags: []string{"lists"}}, PermLibraryRead,
		func(ctx context.Context, in *idPath) (*struct{ Body []seriesBody }, error) {
			rows, err := s.Lists.Series(ctx, in.ID)
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			out := make([]seriesBody, len(rows))
			for i, r := range rows {
				miss := r.Missing
				if miss == nil {
					miss = []int{}
				}
				out[i] = seriesBody{r.Name, r.Owned, miss}
			}
			return &struct{ Body []seriesBody }{out}, nil
		})
	Register(a, huma.Operation{OperationID: "add-missing-volumes", Method: http.MethodPost, Path: "/v1/libraries/{id}/series/missing-to-list",
		Summary: "Add all missing volumes of a series to a list in one action", Tags: []string{"lists"}}, PermLibraryEdit,
		func(ctx context.Context, in *struct {
			ID   uuid.UUID `path:"id"`
			Body struct {
				Series string    `json:"series" minLength:"1" maxLength:"300"`
				ListID uuid.UUID `json:"list_id"`
			}
		}) (*struct {
			Body struct {
				Added int `json:"added"`
			}
		}, error) {
			n, err := s.Lists.AddMissingToList(ctx, in.ID, in.Body.Series, in.Body.ListID)
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			out := &struct {
				Body struct {
					Added int `json:"added"`
				}
			}{}
			out.Body.Added = n
			return out, nil
		})
}
