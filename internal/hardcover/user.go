package hardcover

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// ErrUnauthorized means Hardcover rejected the user's token.
var ErrUnauthorized = errors.New("hardcover: token rejected")

// RemoteBook is one entry of a user's Hardcover library.
type RemoteBook struct {
	UserBookID int64
	BookID     int64
	StatusID   int
	Rating     *float64 // 0..5
	UpdatedAt  time.Time
	Title      string
	ISBNs      []string
}

// UserClient talks to Hardcover with one user's own API token (their reading status and ratings),
// as opposed to Service, which uses the instance token for community ratings.
type UserClient struct {
	http  Doer
	token string
	url   string
}

// NewUserClient creates a client for a user token.
func NewUserClient(d Doer, token string) *UserClient {
	return &UserClient{http: d, token: token, url: endpoint}
}

func (c *UserClient) do(ctx context.Context, query string, vars map[string]any, out any) error {
	body, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("hardcover: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	switch {
	case resp.Status == http.StatusUnauthorized || resp.Status == http.StatusForbidden:
		return ErrUnauthorized
	case resp.Status == http.StatusTooManyRequests:
		after := time.Minute
		if n, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && n > 0 && n <= 3600 {
			after = time.Duration(n) * time.Second
		}
		return &RateLimitedError{After: after}
	case resp.Status != http.StatusOK:
		return fmt.Errorf("hardcover: status %d", resp.Status)
	}
	var env struct {
		Data   json.RawMessage            `json:"data"`
		Errors []struct{ Message string } `json:"errors"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return errors.New("hardcover: unexpected response")
	}
	if len(env.Errors) > 0 {
		// Hasura reports a bad token as a GraphQL error rather than an HTTP status.
		for _, e := range env.Errors {
			if bytes.Contains([]byte(e.Message), []byte("JWT")) || bytes.Contains([]byte(e.Message), []byte("Unauthorized")) {
				return ErrUnauthorized
			}
		}
		return errors.New("hardcover: the request was refused")
	}
	return json.Unmarshal(env.Data, out)
}

// firstUser decodes data.me, which Hardcover returns as a list with one element.
func firstUser[T any](raw json.RawMessage) (T, bool) {
	var zero T
	var list []T
	if json.Unmarshal(raw, &list) == nil && len(list) > 0 {
		return list[0], true
	}
	var one T
	if json.Unmarshal(raw, &one) == nil {
		return one, true
	}
	return zero, false
}

// Me checks the token and returns the Hardcover user ID.
func (c *UserClient) Me(ctx context.Context) (int64, error) {
	var out struct {
		Me json.RawMessage `json:"me"`
	}
	if err := c.do(ctx, `query { me { id } }`, nil, &out); err != nil {
		return 0, err
	}
	u, ok := firstUser[struct {
		ID int64 `json:"id"`
	}](out.Me)
	if !ok || u.ID == 0 {
		return 0, ErrUnauthorized
	}
	return u.ID, nil
}

// UserBooks returns one page of the user's library.
func (c *UserClient) UserBooks(ctx context.Context, offset, limit int) ([]RemoteBook, error) {
	const q = `query($limit: Int!, $offset: Int!) { me { user_books(limit: $limit, offset: $offset, order_by: {id: asc}) {
	  id book_id status_id rating updated_at book { title editions(where: {isbn_13: {_is_null: false}}, limit: 3) { isbn_13 } } } } }`
	var out struct {
		Me json.RawMessage `json:"me"`
	}
	if err := c.do(ctx, q, map[string]any{"limit": limit, "offset": offset}, &out); err != nil {
		return nil, err
	}
	type ub struct {
		ID        int64     `json:"id"`
		BookID    int64     `json:"book_id"`
		StatusID  int       `json:"status_id"`
		Rating    *float64  `json:"rating"`
		UpdatedAt time.Time `json:"updated_at"`
		Book      struct {
			Title    string `json:"title"`
			Editions []struct {
				ISBN13 string `json:"isbn_13"`
			} `json:"editions"`
		} `json:"book"`
	}
	u, ok := firstUser[struct {
		UserBooks []ub `json:"user_books"`
	}](out.Me)
	if !ok {
		return nil, errors.New("hardcover: unexpected response")
	}
	res := make([]RemoteBook, 0, len(u.UserBooks))
	for _, b := range u.UserBooks {
		rb := RemoteBook{UserBookID: b.ID, BookID: b.BookID, StatusID: b.StatusID, Rating: b.Rating, UpdatedAt: b.UpdatedAt, Title: b.Book.Title}
		for _, e := range b.Book.Editions {
			rb.ISBNs = append(rb.ISBNs, e.ISBN13)
		}
		res = append(res, rb)
	}
	return res, nil
}

// BookIDByISBN returns Hardcover's book ID for an ISBN-13, or 0 when unknown.
func (c *UserClient) BookIDByISBN(ctx context.Context, isbn13 string) (int64, error) {
	var out struct {
		Editions []struct {
			BookID int64 `json:"book_id"`
		} `json:"editions"`
	}
	if err := c.do(ctx, `query($isbn: String!) { editions(where: {isbn_13: {_eq: $isbn}}, limit: 1) { book_id } }`, map[string]any{"isbn": isbn13}, &out); err != nil {
		return 0, err
	}
	if len(out.Editions) == 0 {
		return 0, nil
	}
	return out.Editions[0].BookID, nil
}

// Upsert creates (userBookID 0) or updates the user's entry for a book and returns its ID.
func (c *UserClient) Upsert(ctx context.Context, userBookID, bookID int64, statusID int, rating *float64) (int64, error) {
	obj := map[string]any{"status_id": statusID}
	if rating != nil {
		obj["rating"] = *rating
	}
	if userBookID == 0 {
		obj["book_id"] = bookID
		var out struct {
			Insert struct {
				Error    *string `json:"error"`
				UserBook *struct {
					ID int64 `json:"id"`
				} `json:"user_book"`
			} `json:"insert_user_book"`
		}
		if err := c.do(ctx, `mutation($object: UserBookCreateInput!) { insert_user_book(object: $object) { error user_book { id } } }`, map[string]any{"object": obj}, &out); err != nil {
			return 0, err
		}
		if out.Insert.UserBook == nil || (out.Insert.Error != nil && *out.Insert.Error != "") {
			return 0, errors.New("hardcover: the entry was not created")
		}
		return out.Insert.UserBook.ID, nil
	}
	var out struct {
		Update struct {
			Error *string `json:"error"`
		} `json:"update_user_book"`
	}
	if err := c.do(ctx, `mutation($id: Int!, $object: UserBookUpdateInput!) { update_user_book(id: $id, object: $object) { error } }`, map[string]any{"id": userBookID, "object": obj}, &out); err != nil {
		return 0, err
	}
	if out.Update.Error != nil && *out.Update.Error != "" {
		return 0, errors.New("hardcover: the entry was not updated")
	}
	return userBookID, nil
}
