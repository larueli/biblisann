package api

import (
	"context"
	"io"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
	"github.com/larueli/biblisann/internal/ics"
	"github.com/larueli/biblisann/internal/loans"
)

type contactBody struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Email string    `json:"email,omitempty"`
	Notes string    `json:"notes,omitempty"`
}

type loanBody struct {
	ID           uuid.UUID  `json:"id"`
	LibraryID    uuid.UUID  `json:"library_id"`
	Direction    string     `json:"direction" enum:"lent,borrowed"`
	InstanceID   *uuid.UUID `json:"instance_id,omitempty"`
	ContactID    *uuid.UUID `json:"contact_id,omitempty"`
	Counterparty string     `json:"counterparty"`
	Title        string     `json:"title"`
	LentOn       Date       `json:"lent_on"`
	DueOn        *Date      `json:"due_on,omitempty"`
	ReturnedOn   *Date      `json:"returned_on,omitempty"`
	Notes        string     `json:"notes,omitempty"`
}

func toLoanBody(l dbsqlc.Loan) loanBody {
	return loanBody{l.ID, l.LibraryID, l.Direction, l.InstanceID, l.ContactID, l.Counterparty, l.Title, Date{l.LentOn}, toDate(l.DueOn), toDate(l.ReturnedOn), l.Notes}
}

func (s *Server) registerLoans(a huma.API) {
	type idPath struct {
		ID uuid.UUID `path:"id"`
	}
	Register(a, huma.Operation{OperationID: "list-contacts", Method: http.MethodGet, Path: "/v1/contacts",
		Summary: "My contacts (people I lend books to)", Tags: []string{"loans"}}, PermAuthenticated,
		func(ctx context.Context, _ *struct{}) (*struct{ Body []contactBody }, error) {
			rows, err := s.Loans.Contacts(ctx)
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			out := make([]contactBody, len(rows))
			for i, c := range rows {
				out[i] = contactBody{c.ID, c.Name, c.Email, c.Notes}
			}
			return &struct{ Body []contactBody }{out}, nil
		})
	Register(a, huma.Operation{OperationID: "create-contact", Method: http.MethodPost, Path: "/v1/contacts",
		Summary: "Add a contact", Tags: []string{"loans"}, DefaultStatus: http.StatusCreated}, PermAuthenticated,
		func(ctx context.Context, in *struct {
			Body struct {
				Name  string `json:"name" minLength:"1" maxLength:"200"`
				Email string `json:"email,omitempty" maxLength:"320"`
				Notes string `json:"notes,omitempty" maxLength:"5000"`
			}
		}) (*struct{ Body contactBody }, error) {
			c, err := s.Loans.CreateContact(ctx, in.Body.Name, in.Body.Email, in.Body.Notes)
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			return &struct{ Body contactBody }{contactBody{c.ID, c.Name, c.Email, c.Notes}}, nil
		})
	Register(a, huma.Operation{OperationID: "delete-contact", Method: http.MethodDelete, Path: "/v1/contacts/{id}",
		Summary: "Delete a contact", Tags: []string{"loans"}, DefaultStatus: http.StatusNoContent}, PermAuthenticated,
		func(ctx context.Context, in *idPath) (*struct{}, error) {
			return nil, mapError(s.Log, s.Loans.DeleteContact(ctx, in.ID))
		})

	Register(a, huma.Operation{OperationID: "list-loans", Method: http.MethodGet, Path: "/v1/loans",
		Summary: "My loans: lent and borrowed, open first", Tags: []string{"loans"}}, PermAuthenticated,
		func(ctx context.Context, in *struct {
			All bool `query:"all" doc:"Include returned loans."`
		}) (*struct{ Body []loanBody }, error) {
			rows, err := s.Loans.List(ctx, in.All)
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			out := make([]loanBody, len(rows))
			for i, r := range rows {
				out[i] = toLoanBody(dbsqlc.Loan{ID: r.ID, LibraryID: r.LibraryID, OwnerID: r.OwnerID, Direction: r.Direction, InstanceID: r.InstanceID,
					ContactID: r.ContactID, Counterparty: r.Counterparty, Title: r.Title, LentOn: r.LentOn, DueOn: r.DueOn, ReturnedOn: r.ReturnedOn, Notes: r.Notes})
			}
			return &struct{ Body []loanBody }{out}, nil
		})
	Register(a, huma.Operation{OperationID: "lend-instance", Method: http.MethodPost, Path: "/v1/instances/{id}/lend",
		Summary: "Lend one of my copies", Tags: []string{"loans"}, DefaultStatus: http.StatusCreated, Errors: []int{409, 422}}, PermLibraryEdit,
		func(ctx context.Context, in *struct {
			ID   uuid.UUID `path:"id"`
			Body struct {
				ContactID    *uuid.UUID `json:"contact_id,omitempty"`
				Counterparty string     `json:"counterparty,omitempty" maxLength:"200" doc:"Name, when there is no contact."`
				DueOn        *Date      `json:"due_on,omitempty"`
				Notes        string     `json:"notes,omitempty" maxLength:"5000"`
			}
		}) (*struct{ Body loanBody }, error) {
			l, err := s.Loans.Lend(ctx, loans.LendInput{InstanceID: in.ID, ContactID: in.Body.ContactID, Counterparty: in.Body.Counterparty,
				DueOn: toTime(in.Body.DueOn), Notes: in.Body.Notes})
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			return &struct{ Body loanBody }{toLoanBody(l)}, nil
		})
	Register(a, huma.Operation{OperationID: "borrow-book", Method: http.MethodPost, Path: "/v1/libraries/{id}/borrowed",
		Summary: "Record a book I borrowed from a person or a library", Tags: []string{"loans"}, DefaultStatus: http.StatusCreated}, PermLibraryEdit,
		func(ctx context.Context, in *struct {
			ID   uuid.UUID `path:"id"`
			Body struct {
				Title        string `json:"title" minLength:"1" maxLength:"1000"`
				Counterparty string `json:"counterparty" minLength:"1" maxLength:"200"`
				DueOn        *Date  `json:"due_on,omitempty"`
				Notes        string `json:"notes,omitempty" maxLength:"5000"`
			}
		}) (*struct{ Body loanBody }, error) {
			l, err := s.Loans.Borrow(ctx, loans.BorrowInput{LibraryID: in.ID, Title: in.Body.Title, Counterparty: in.Body.Counterparty,
				DueOn: toTime(in.Body.DueOn), Notes: in.Body.Notes})
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			return &struct{ Body loanBody }{toLoanBody(l)}, nil
		})
	Register(a, huma.Operation{OperationID: "return-loan", Method: http.MethodPost, Path: "/v1/loans/{id}/return",
		Summary: "Mark a loan as returned", Tags: []string{"loans"}, Errors: []int{404, 409}}, PermAuthenticated,
		func(ctx context.Context, in *struct {
			ID   uuid.UUID `path:"id"`
			Body struct {
				On *Date `json:"on,omitempty" doc:"Defaults to today."`
			}
		}) (*struct{ Body loanBody }, error) {
			l, err := s.Loans.Return(ctx, in.ID, toTime(in.Body.On))
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			return &struct{ Body loanBody }{toLoanBody(l)}, nil
		})
	Register(a, huma.Operation{OperationID: "update-loan", Method: http.MethodPatch, Path: "/v1/loans/{id}",
		Summary: "Change the due date or notes of a loan", Tags: []string{"loans"}}, PermAuthenticated,
		func(ctx context.Context, in *struct {
			ID   uuid.UUID `path:"id"`
			Body struct {
				DueOn *Date  `json:"due_on,omitempty"`
				Notes string `json:"notes,omitempty" maxLength:"5000"`
			}
		}) (*struct{ Body loanBody }, error) {
			l, err := s.Loans.Reschedule(ctx, in.ID, toTime(in.Body.DueOn), in.Body.Notes)
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			return &struct{ Body loanBody }{toLoanBody(l)}, nil
		})
}

// serveLoansFeed serves the iCalendar feed of due dates. It authenticates with a feed token in
// the query string because calendar apps cannot send headers; the token only opens this feed.
func (s *Server) serveLoansFeed(w http.ResponseWriter, r *http.Request) {
	if s.TokenSvc == nil || s.Loans == nil {
		writeProblem(w, http.StatusNotFound, "not found")
		return
	}
	if s.Limiter != nil {
		if ok, _ := s.Limiter.Allow("feed|" + clientKey(r)); !ok {
			writeProblem(w, http.StatusTooManyRequests, "too many requests")
			return
		}
	}
	uid, err := s.TokenSvc.FeedUser(r.Context(), r.URL.Query().Get("token"))
	if err != nil {
		writeProblem(w, http.StatusNotFound, "not found")
		return
	}
	events, err := s.Loans.FeedEvents(r.Context(), uid, r.URL.Query().Get("lang"))
	if err != nil {
		s.Log.Error("feed", "error", err)
		writeProblem(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("Referrer-Policy", "no-referrer")
	_, _ = io.WriteString(w, ics.Render("Biblisann loans", events)) //nolint:gosec // text/calendar; every value is RFC 5545 escaped
}
