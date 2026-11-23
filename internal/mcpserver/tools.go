package mcpserver

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/larueli/biblisann/internal/authz"
	"github.com/larueli/biblisann/internal/library"
	"github.com/larueli/biblisann/internal/loans"
	"github.com/larueli/biblisann/internal/scan"
)

func isNotFound(err error) bool  { return errors.Is(err, authz.ErrNotFound) }
func isForbidden(err error) bool { return errors.Is(err, authz.ErrForbidden) }

type libraryOut struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

type bookOut struct {
	InstanceID string   `json:"instance_id"`
	WorkID     string   `json:"work_id"`
	Title      string   `json:"title"`
	Authors    []string `json:"authors"`
	ISBN13     string   `json:"isbn13,omitempty"`
	PlaceID    string   `json:"place_id,omitempty"`
	Notes      string   `json:"notes,omitempty"`
}

func (d Deps) readTools(s *mcp.Server, wrap func(context.Context) context.Context) {
	mcp.AddTool(s, &mcp.Tool{Name: "list_libraries", Description: "List the user's libraries with the user's role in each.",
		Annotations: ro()},
		func(c context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, struct {
			Libraries []libraryOut `json:"libraries"`
		}, error) {
			var out struct {
				Libraries []libraryOut `json:"libraries"`
			}
			rows, err := d.Library.ListLibraries(wrap(c))
			if err != nil {
				r, _ := textErr(err)
				return r, out, nil
			}
			out.Libraries = []libraryOut{}
			for _, l := range rows {
				out.Libraries = append(out.Libraries, libraryOut{l.ID.String(), l.Name, l.Role})
			}
			return nil, out, nil
		})

	type searchIn struct {
		LibraryID string `json:"library_id" jsonschema:"ID from list_libraries"`
		Query     string `json:"query" jsonschema:"title, author, publisher or ISBN; accents and small typos are tolerated"`
		Limit     int    `json:"limit,omitempty" jsonschema:"at most 50 (default 20)"`
	}
	type searchOut struct {
		Notice string    `json:"notice"`
		Books  []bookOut `json:"books"`
	}
	mcp.AddTool(s, &mcp.Tool{Name: "search_books",
		Description: "Search the books (copies) of one library. " + UntrustedNotice, Annotations: ro()},
		func(c context.Context, _ *mcp.CallToolRequest, in searchIn) (*mcp.CallToolResult, searchOut, error) {
			out := searchOut{Notice: UntrustedNotice, Books: []bookOut{}}
			id, err := parseID(in.LibraryID)
			if err != nil {
				r, _ := textErr(err)
				return r, out, nil
			}
			limit := in.Limit
			if limit <= 0 || limit > 50 {
				limit = 20
			}
			rows, err := d.Library.SearchInstances(wrap(c), id, in.Query, limit)
			if err != nil {
				r, _ := textErr(err)
				return r, out, nil
			}
			for _, r := range rows {
				b := bookOut{InstanceID: r.ID.String(), WorkID: r.WorkID.String(), Title: r.Title, Authors: r.Authors, Notes: r.Notes}
				if r.Isbn13 != nil {
					b.ISBN13 = *r.Isbn13
				}
				if r.PlaceID != nil {
					b.PlaceID = r.PlaceID.String()
				}
				out.Books = append(out.Books, b)
			}
			return nil, out, nil
		})

	type ownedIn struct {
		ISBN string `json:"isbn" jsonschema:"ISBN-10 or ISBN-13, hyphens allowed"`
	}
	type ownedOut struct {
		Owned   bool     `json:"owned"`
		Matches []string `json:"matches"`
	}
	mcp.AddTool(s, &mcp.Tool{Name: "do_i_own",
		Description: "Does the user already own this book, in any edition, in any library they can read? Answers at work level.",
		Annotations: ro()},
		func(c context.Context, _ *mcp.CallToolRequest, in ownedIn) (*mcp.CallToolResult, ownedOut, error) {
			out := ownedOut{Matches: []string{}}
			m, err := d.Library.Owned(wrap(c), in.ISBN)
			if err != nil {
				r, _ := textErr(err)
				return r, out, nil
			}
			for _, o := range m {
				kind := "another edition of the same work"
				if o.SameEdition {
					kind = "this exact edition"
				}
				out.Matches = append(out.Matches, o.Title+" in "+o.LibraryName+" ("+kind+")")
			}
			out.Owned = len(m) > 0
			return nil, out, nil
		})

	type loansOut struct {
		Notice string   `json:"notice"`
		Loans  []string `json:"loans"`
	}
	mcp.AddTool(s, &mcp.Tool{Name: "list_open_loans",
		Description: "Books lent to others and books borrowed that are not yet returned, with due dates. " + UntrustedNotice, Annotations: ro()},
		func(c context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, loansOut, error) {
			out := loansOut{Notice: UntrustedNotice, Loans: []string{}}
			rows, err := d.Loans.List(wrap(c), false)
			if err != nil {
				r, _ := textErr(err)
				return r, out, nil
			}
			for _, l := range rows {
				due := "no due date"
				if l.DueOn != nil {
					due = "due " + l.DueOn.Format("2006-01-02")
				}
				dir := "lent to"
				if l.Direction == "borrowed" {
					dir = "borrowed from"
				}
				out.Loans = append(out.Loans, l.ID.String()+": "+l.Title+" "+dir+" "+l.Counterparty+", "+due)
			}
			return nil, out, nil
		})

	type missingIn struct {
		LibraryID string `json:"library_id"`
	}
	type missingOut struct {
		Series []struct {
			Name    string `json:"name"`
			Missing []int  `json:"missing_volumes"`
		} `json:"series"`
	}
	mcp.AddTool(s, &mcp.Tool{Name: "missing_volumes", Description: "Series in a library with the volumes the user does not have yet.",
		Annotations: ro()},
		func(c context.Context, _ *mcp.CallToolRequest, in missingIn) (*mcp.CallToolResult, missingOut, error) {
			var out missingOut
			id, err := parseID(in.LibraryID)
			if err != nil {
				r, _ := textErr(err)
				return r, out, nil
			}
			rows, err := d.Lists.Series(wrap(c), id)
			if err != nil {
				r, _ := textErr(err)
				return r, out, nil
			}
			for _, r := range rows {
				if len(r.Missing) == 0 {
					continue
				}
				out.Series = append(out.Series, struct {
					Name    string `json:"name"`
					Missing []int  `json:"missing_volumes"`
				}{r.Name, r.Missing})
			}
			return nil, out, nil
		})
}

func (d Deps) writeTools(s *mcp.Server, wrap func(context.Context) context.Context) {
	type addIn struct {
		LibraryID      string `json:"library_id"`
		ISBN           string `json:"isbn"`
		AllowDuplicate bool   `json:"allow_duplicate,omitempty" jsonschema:"add another copy even if the user already owns this work"`
		RequestID      string `json:"request_id" jsonschema:"a unique string (8+ characters) so retrying never adds the book twice"`
	}
	type addOut struct {
		Status     string `json:"status" jsonschema:"added, already_owned or not_found"`
		InstanceID string `json:"instance_id,omitempty"`
		Title      string `json:"title,omitempty"`
	}
	mcp.AddTool(s, &mcp.Tool{Name: "add_book",
		Description: "Add a book to a library by ISBN. Looks the metadata up, warns instead of duplicating, and is safe to retry with the same request_id.",
		Annotations: additive(true)},
		func(c context.Context, _ *mcp.CallToolRequest, in addIn) (*mcp.CallToolResult, addOut, error) {
			var out addOut
			id, err := parseID(in.LibraryID)
			if err != nil {
				r, _ := textErr(err)
				return r, out, nil
			}
			res, err := d.Scan.Scan(wrap(c), scan.Input{LibraryID: id, ISBN: in.ISBN, AllowDuplicate: in.AllowDuplicate, IdempotencyKey: in.RequestID})
			if err != nil {
				r, _ := textErr(err)
				return r, out, nil
			}
			out.Status = res.Status
			if res.Instance != nil {
				out.InstanceID = res.Instance.ID.String()
			}
			if res.Edition != nil {
				out.Title = res.Edition.Title
			}
			return nil, out, nil
		})

	type statusIn struct {
		WorkID string `json:"work_id"`
		Status string `json:"status" jsonschema:"to_read, reading, read or abandoned"`
	}
	mcp.AddTool(s, &mcp.Tool{Name: "set_reading_status", Description: "Set the user's reading status of a work.",
		Annotations: additive(true)},
		func(c context.Context, _ *mcp.CallToolRequest, in statusIn) (*mcp.CallToolResult, textResult, error) {
			id, err := parseID(in.WorkID)
			if err != nil {
				r, _ := textErr(err)
				return r, textResult{}, nil
			}
			if _, err := d.Reading.SetStatus(wrap(c), id, in.Status, nil, nil); err != nil {
				r, _ := textErr(err)
				return r, textResult{}, nil
			}
			return nil, textResult{Notice: "ok"}, nil
		})

	type moveIn struct {
		InstanceID string `json:"instance_id"`
		PlaceID    string `json:"place_id,omitempty" jsonschema:"empty to say the book has no place"`
	}
	mcp.AddTool(s, &mcp.Tool{Name: "move_book", Description: "Move a copy to another place of the same library.",
		Annotations: additive(true)},
		func(c context.Context, _ *mcp.CallToolRequest, in moveIn) (*mcp.CallToolResult, textResult, error) {
			id, err := parseID(in.InstanceID)
			if err != nil {
				r, _ := textErr(err)
				return r, textResult{}, nil
			}
			cur, err := d.Library.GetInstance(wrap(c), id)
			if err != nil {
				r, _ := textErr(err)
				return r, textResult{}, nil
			}
			inp := library.InstanceInput{EditionID: cur.EditionID, Notes: cur.Notes, AcquiredOn: cur.AcquiredOn}
			if in.PlaceID != "" {
				pid, err := parseID(in.PlaceID)
				if err != nil {
					r, _ := textErr(err)
					return r, textResult{}, nil
				}
				inp.PlaceID = &pid
			}
			if _, err := d.Library.UpdateInstance(wrap(c), id, inp); err != nil {
				r, _ := textErr(err)
				return r, textResult{}, nil
			}
			return nil, textResult{Notice: "ok"}, nil
		})

	type lendIn struct {
		InstanceID   string `json:"instance_id"`
		Counterparty string `json:"counterparty" jsonschema:"who borrows it"`
		DueOn        string `json:"due_on,omitempty" jsonschema:"YYYY-MM-DD"`
	}
	mcp.AddTool(s, &mcp.Tool{Name: "lend_book", Description: "Record that a copy was lent to someone.", Annotations: additive(false)},
		func(c context.Context, _ *mcp.CallToolRequest, in lendIn) (*mcp.CallToolResult, textResult, error) {
			id, err := parseID(in.InstanceID)
			if err != nil {
				r, _ := textErr(err)
				return r, textResult{}, nil
			}
			due, err := dateOrNil(in.DueOn)
			if err != nil {
				r, _ := textErr(err)
				return r, textResult{}, nil
			}
			l, err := d.Loans.Lend(wrap(c), loans.LendInput{InstanceID: id, Counterparty: in.Counterparty, DueOn: due})
			if err != nil {
				r, _ := textErr(err)
				return r, textResult{}, nil
			}
			return nil, textResult{Notice: "lent; loan id " + l.ID.String()}, nil
		})

	type returnIn struct {
		LoanID string `json:"loan_id" jsonschema:"ID from list_open_loans"`
	}
	mcp.AddTool(s, &mcp.Tool{Name: "return_loan", Description: "Mark a loan as returned today.", Annotations: additive(true)},
		func(c context.Context, _ *mcp.CallToolRequest, in returnIn) (*mcp.CallToolResult, textResult, error) {
			id, err := parseID(in.LoanID)
			if err != nil {
				r, _ := textErr(err)
				return r, textResult{}, nil
			}
			if _, err := d.Loans.Return(wrap(c), id, nil); err != nil {
				r, _ := textErr(err)
				return r, textResult{}, nil
			}
			return nil, textResult{Notice: "returned"}, nil
		})
}
