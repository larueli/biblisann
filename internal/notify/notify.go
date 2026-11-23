// Package notify delivers notifications to the channels a user configured: email, Web Push,
// signed webhooks and ntfy. Delivery to one channel failing never blocks the others.
package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
)

// Message is what gets delivered.
type Message struct {
	Event string // machine name, for example loan.overdue
	Title string
	Body  string
}

// Sender delivers a message to one channel kind.
type Sender interface {
	Kind() string
	Send(ctx context.Context, ch dbsqlc.NotificationChannel, m Message) error
}

// Dispatcher fans a message out to every enabled channel of a user.
type Dispatcher struct {
	q       *dbsqlc.Queries
	senders map[string]Sender
	log     *slog.Logger
}

// NewDispatcher creates a dispatcher. Kinds without a sender (for example email when SMTP is not
// configured) are skipped with a warning.
func NewDispatcher(q *dbsqlc.Queries, log *slog.Logger, senders ...Sender) *Dispatcher {
	d := &Dispatcher{q: q, senders: map[string]Sender{}, log: log}
	for _, s := range senders {
		d.senders[s.Kind()] = s
	}
	return d
}

// Available lists the channel kinds this instance can deliver.
func (d *Dispatcher) Available() []string {
	var out []string
	for _, k := range []string{"email", "webpush", "webhook", "ntfy"} {
		if _, ok := d.senders[k]; ok {
			out = append(out, k)
		}
	}
	return out
}

// Deliver sends m to every enabled channel. It returns an error only when channels exist and
// none accepted the message, so a job queue retries the whole delivery.
func (d *Dispatcher) Deliver(ctx context.Context, userID uuid.UUID, m Message) error {
	channels, err := d.q.ListEnabledChannels(ctx, userID)
	if err != nil {
		return fmt.Errorf("notify: channels: %w", err)
	}
	var errs []error
	delivered, attempted := 0, 0
	for _, ch := range channels {
		s, ok := d.senders[ch.Kind]
		if !ok {
			d.log.WarnContext(ctx, "no sender for channel kind", "kind", ch.Kind)
			continue
		}
		attempted++
		if err := s.Send(ctx, ch, m); err != nil {
			d.log.WarnContext(ctx, "notification delivery failed", "kind", ch.Kind, "channel", ch.ID, "error", err)
			errs = append(errs, fmt.Errorf("%s: %w", ch.Kind, err))
			continue
		}
		delivered++
	}
	if attempted > 0 && delivered == 0 {
		return errors.Join(errs...)
	}
	return nil
}

// clean removes line breaks from values placed in headers.
func clean(s string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(s)
}

// Channels manages a user's channel list.
type Channels struct{ q *dbsqlc.Queries }

// NewChannels creates the manager.
func NewChannels(q *dbsqlc.Queries) *Channels { return &Channels{q: q} }

// MaxChannelsPerUser bounds how many channels one account may register.
const MaxChannelsPerUser = 20

// ErrTooMany is returned when the per-user channel limit is reached.
var ErrTooMany = errors.New("too many notification channels")

// List returns the user's channels.
func (c *Channels) List(ctx context.Context, userID uuid.UUID) ([]dbsqlc.NotificationChannel, error) {
	return c.q.ListChannels(ctx, userID)
}

// Create stores a channel after validating its target.
func (c *Channels) Create(ctx context.Context, userID uuid.UUID, kind, target string, cfg []byte) (dbsqlc.NotificationChannel, error) {
	existing, err := c.q.ListChannels(ctx, userID)
	if err != nil {
		return dbsqlc.NotificationChannel{}, err
	}
	if len(existing) >= MaxChannelsPerUser {
		return dbsqlc.NotificationChannel{}, ErrTooMany
	}
	return c.q.CreateChannel(ctx, dbsqlc.CreateChannelParams{UserID: userID, Kind: kind, Target: target, Config: cfg})
}

// Delete removes a channel and reports how many rows went away.
func (c *Channels) Delete(ctx context.Context, userID, id uuid.UUID) (int64, error) {
	return c.q.DeleteChannel(ctx, dbsqlc.DeleteChannelParams{ID: id, UserID: userID})
}
