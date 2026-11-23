package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/larueli/biblisann/internal/authz"
	"github.com/larueli/biblisann/internal/i18n"
	"github.com/larueli/biblisann/internal/notify"
	"github.com/larueli/biblisann/internal/tokens"
)

type tokenBody struct {
	ID         uuid.UUID   `json:"id"`
	Name       string      `json:"name"`
	Kind       string      `json:"kind" enum:"pat,feed,scim"`
	Scopes     []string    `json:"scopes" nullable:"false"`
	LibraryIDs []uuid.UUID `json:"library_ids,omitempty" doc:"When set, the token only reaches these libraries."`
	ExpiresAt  *time.Time  `json:"expires_at,omitempty"`
	LastUsed   *time.Time  `json:"last_used,omitempty"`
	CreatedAt  time.Time   `json:"created_at"`
}

type channelBody struct {
	ID      uuid.UUID `json:"id"`
	Kind    string    `json:"kind" enum:"email,webpush,webhook,ntfy"`
	Target  string    `json:"target"`
	Enabled bool      `json:"enabled"`
	Secret  string    `json:"secret,omitempty" doc:"Webhook signing secret, returned once at creation."`
}

func (s *Server) registerTokens(a huma.API) {
	Register(a, huma.Operation{OperationID: "list-tokens", Method: http.MethodGet, Path: "/v1/me/tokens",
		Summary: "My access tokens (secrets are never shown again)", Tags: []string{"tokens"}}, PermAuthenticated,
		func(ctx context.Context, _ *struct{}) (*struct{ Body []tokenBody }, error) {
			rows, err := s.TokenSvc.List(ctx)
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			out := make([]tokenBody, len(rows))
			for i, r := range rows {
				out[i] = tokenBody{r.ID, r.Name, r.Kind, r.Scopes, r.LibraryIds, r.ExpiresAt, r.LastUsed, r.CreatedAt}
			}
			return &struct{ Body []tokenBody }{out}, nil
		})
	Register(a, huma.Operation{OperationID: "create-token", Method: http.MethodPost, Path: "/v1/me/tokens",
		Summary:     "Create a personal access token (for scripts and MCP clients) or a calendar feed token",
		Description: "The secret is returned once. A token never exceeds your own rights. Use scope read for read-only access.",
		Tags:        []string{"tokens"}, DefaultStatus: http.StatusCreated, Errors: []int{403, 422}}, PermAuthenticated,
		func(ctx context.Context, in *struct {
			Body struct {
				Name       string      `json:"name" minLength:"1" maxLength:"100"`
				Kind       string      `json:"kind,omitempty" enum:"pat,feed,scim" default:"pat"`
				Scopes     []string    `json:"scopes,omitempty" doc:"read and/or write (personal access tokens)."`
				LibraryIDs []uuid.UUID `json:"library_ids,omitempty"`
				ExpiresAt  *time.Time  `json:"expires_at,omitempty"`
			}
		}) (*struct {
			Body struct {
				tokenBody
				Token string `json:"token" doc:"The secret. Shown once."`
			}
		}, error) {
			kind := in.Body.Kind
			if kind == "" {
				kind = tokens.KindPAT
			}
			c, err := s.TokenSvc.Create(ctx, kind, in.Body.Name, in.Body.Scopes, in.Body.LibraryIDs, in.Body.ExpiresAt)
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			out := &struct {
				Body struct {
					tokenBody
					Token string `json:"token" doc:"The secret. Shown once."`
				}
			}{}
			m := c.Meta
			out.Body.tokenBody = tokenBody{m.ID, m.Name, m.Kind, m.Scopes, m.LibraryIds, m.ExpiresAt, nil, m.CreatedAt}
			out.Body.Token = c.Token
			return out, nil
		})
	Register(a, huma.Operation{OperationID: "revoke-token", Method: http.MethodDelete, Path: "/v1/me/tokens/{id}",
		Summary: "Revoke a token", Tags: []string{"tokens"}, DefaultStatus: http.StatusNoContent}, PermAuthenticated,
		func(ctx context.Context, in *struct {
			ID uuid.UUID `path:"id"`
		}) (*struct{}, error) {
			return nil, mapError(s.Log, s.TokenSvc.Revoke(ctx, in.ID))
		})

	// notification channels
	Register(a, huma.Operation{OperationID: "list-channels", Method: http.MethodGet, Path: "/v1/me/notification-channels",
		Summary: "Where I get notified, and which kinds this server can deliver", Tags: []string{"notifications"}}, PermAuthenticated,
		func(ctx context.Context, _ *struct{}) (*struct {
			Body struct {
				Available []string      `json:"available" nullable:"false"`
				Channels  []channelBody `json:"channels" nullable:"false"`
			}
		}, error) {
			p, err := userID(ctx)
			if err != nil {
				return nil, err
			}
			rows, err := s.Channels.List(ctx, p)
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			out := &struct {
				Body struct {
					Available []string      `json:"available" nullable:"false"`
					Channels  []channelBody `json:"channels" nullable:"false"`
				}
			}{}
			out.Body.Available = []string{}
			if s.Notify != nil {
				out.Body.Available = s.Notify.Available()
			}
			out.Body.Channels = []channelBody{}
			for _, r := range rows {
				out.Body.Channels = append(out.Body.Channels, channelBody{ID: r.ID, Kind: r.Kind, Target: r.Target, Enabled: r.Enabled})
			}
			return out, nil
		})
	Register(a, huma.Operation{OperationID: "create-channel", Method: http.MethodPost, Path: "/v1/me/notification-channels",
		Summary: "Add a notification channel", Tags: []string{"notifications"}, DefaultStatus: http.StatusCreated, Errors: []int{422}}, PermAuthenticated,
		func(ctx context.Context, in *struct {
			Body struct {
				Kind   string          `json:"kind" enum:"email,webpush,webhook,ntfy"`
				Target string          `json:"target" maxLength:"2000" doc:"Email address, push endpoint, webhook URL or ntfy topic URL."`
				Config json.RawMessage `json:"config,omitempty" doc:"webpush: {p256dh, auth}. ntfy: {token}. Never returned."`
			}
		}) (*struct{ Body channelBody }, error) {
			p, err := userID(ctx)
			if err != nil {
				return nil, err
			}
			if !notify.ValidTarget(in.Body.Kind, in.Body.Target) {
				return nil, huma.Error422UnprocessableEntity("invalid target for this kind of channel")
			}
			cfg := map[string]any{}
			if len(in.Body.Config) > 0 {
				if json.Unmarshal(in.Body.Config, &cfg) != nil || len(in.Body.Config) > 4096 {
					return nil, huma.Error422UnprocessableEntity("invalid config")
				}
			}
			secret := ""
			if in.Body.Kind == "webhook" {
				b := make([]byte, 24)
				_, _ = rand.Read(b)
				secret = base64.RawURLEncoding.EncodeToString(b)
				cfg["secret"] = secret
			}
			raw, _ := json.Marshal(cfg)
			ch, err := s.Channels.Create(ctx, p, in.Body.Kind, in.Body.Target, raw)
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			return &struct{ Body channelBody }{channelBody{ch.ID, ch.Kind, ch.Target, ch.Enabled, secret}}, nil
		})
	Register(a, huma.Operation{OperationID: "delete-channel", Method: http.MethodDelete, Path: "/v1/me/notification-channels/{id}",
		Summary: "Remove a notification channel", Tags: []string{"notifications"}, DefaultStatus: http.StatusNoContent}, PermAuthenticated,
		func(ctx context.Context, in *struct {
			ID uuid.UUID `path:"id"`
		}) (*struct{}, error) {
			p, err := userID(ctx)
			if err != nil {
				return nil, err
			}
			n, err := s.Channels.Delete(ctx, p, in.ID)
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			if n == 0 {
				return nil, huma.Error404NotFound("not found")
			}
			return nil, nil
		})
	Register(a, huma.Operation{OperationID: "test-notification", Method: http.MethodPost, Path: "/v1/me/notification-channels/test",
		Summary: "Send a test notification to all my enabled channels", Tags: []string{"notifications"}, DefaultStatus: http.StatusNoContent,
		Errors: []int{502}}, PermAuthenticated,
		func(ctx context.Context, _ *struct{}) (*struct{}, error) {
			sess := SessionFrom(ctx)
			lang := ""
			if sess != nil {
				lang = sess.Locale
			}
			if s.Notify == nil {
				return nil, huma.Error503ServiceUnavailable("notifications are not configured")
			}
			p, err := userID(ctx)
			if err != nil {
				return nil, err
			}
			if err := s.Notify.Deliver(ctx, p, notify.Message{Event: "test", Title: i18n.T(lang, "test.title"), Body: i18n.T(lang, "test.body")}); err != nil {
				s.Log.Warn("test notification failed", "error", err)
				return nil, huma.NewError(http.StatusBadGateway, "delivery failed")
			}
			return nil, nil
		})
}

func userID(ctx context.Context) (uuid.UUID, error) {
	p, err := authz.From(ctx)
	if err != nil {
		return uuid.Nil, huma.Error401Unauthorized("authentication required")
	}
	return p.UserID, nil
}
