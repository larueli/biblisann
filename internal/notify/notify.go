// Package notify delivers notifications to the channels a user configured: email, Web Push,
// signed webhooks and ntfy. Delivery to one channel failing never blocks the others.
package notify

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

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
	// EmailDailyCap limits the mails one user can trigger per day (0 means DefaultEmailDailyCap).
	EmailDailyCap int
}

// DefaultEmailDailyCap is the daily mail limit per user when none is configured.
const DefaultEmailDailyCap = 20

func (d *Dispatcher) emailCap() int {
	if d.EmailDailyCap > 0 {
		return d.EmailDailyCap
	}
	return DefaultEmailDailyCap
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
		if ch.Kind == "email" {
			if _, err := d.q.TakeEmailQuota(ctx, dbsqlc.TakeEmailQuotaParams{UserID: userID, Sent: int32(d.emailCap())}); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					d.log.WarnContext(ctx, "daily email cap reached, message skipped", "channel", ch.ID)
					continue // a retry would not help before tomorrow
				}
				return fmt.Errorf("notify: email quota: %w", err)
			}
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

// Mailer sends a plain-text email (the confirmation code of an email channel).
type Mailer interface {
	SendMail(ctx context.Context, to, subject, body string) error
}

// Channels manages a user's channel list.
type Channels struct {
	q      *dbsqlc.Queries
	mailer Mailer
	cap    int
}

// NewChannels creates the manager.
func NewChannels(q *dbsqlc.Queries) *Channels { return &Channels{q: q} }

// SetMailer enables email channels: confirmation codes go out through it. emailDailyCap is the
// per-user daily mail limit shared with notifications (0: DefaultEmailDailyCap).
func (c *Channels) SetMailer(m Mailer, emailDailyCap int) {
	c.mailer, c.cap = m, emailDailyCap
}

// Errors of the confirmation flow.
var (
	ErrNoMailer      = errors.New("this server cannot send email")
	ErrDailyCap      = errors.New("daily email limit reached, try again tomorrow")
	ErrBadCode       = errors.New("invalid or expired code")
	ErrTooManyTries  = errors.New("too many wrong codes: ask for a new one")
	ErrAlreadyActive = errors.New("channel already confirmed")
)

const (
	codeLife        = 30 * time.Minute
	maxCodeAttempts = 5
)

func codeHash(channelID uuid.UUID, code string) []byte {
	h := sha256.Sum256([]byte(channelID.String() + ":" + strings.TrimSpace(code)))
	return h[:]
}

// SendCode creates a fresh six-digit code for an unconfirmed email channel and mails it to the
// address. It counts toward the user's daily mail cap, so it cannot be used to spam an address.
func (c *Channels) SendCode(ctx context.Context, userID, id uuid.UUID) error {
	if c.mailer == nil {
		return ErrNoMailer
	}
	ch, err := c.q.GetChannel(ctx, dbsqlc.GetChannelParams{ID: id, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if ch.Kind != "email" || ch.ConfirmedAt != nil {
		return ErrAlreadyActive
	}
	limit := c.cap
	if limit <= 0 {
		limit = DefaultEmailDailyCap
	}
	if _, err := c.q.TakeEmailQuota(ctx, dbsqlc.TakeEmailQuotaParams{UserID: userID, Sent: int32(limit)}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrDailyCap
		}
		return err
	}
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return fmt.Errorf("notify: code: %w", err)
	}
	code := fmt.Sprintf("%06d", n.Int64())
	exp := time.Now().Add(codeLife)
	if err := c.q.SetChannelCode(ctx, dbsqlc.SetChannelCodeParams{ID: id, UserID: userID, ConfirmHash: codeHash(id, code), ConfirmExpires: &exp}); err != nil {
		return err
	}
	body := "Your Biblisann confirmation code is " + code + ".\n\nIt works for 30 minutes. If you did not ask for it, ignore this message: no notification will be sent to this address.\n"
	return c.mailer.SendMail(ctx, ch.Target, "Confirm your email for Biblisann notifications", body)
}

// Confirm checks the code and activates the channel.
func (c *Channels) Confirm(ctx context.Context, userID, id uuid.UUID, code string) error {
	attempts, err := c.q.BumpChannelAttempts(ctx, dbsqlc.BumpChannelAttemptsParams{ID: id, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound // unknown, or already confirmed
	}
	if err != nil {
		return err
	}
	if attempts > maxCodeAttempts {
		return ErrTooManyTries
	}
	n, err := c.q.ConfirmChannel(ctx, dbsqlc.ConfirmChannelParams{ID: id, UserID: userID, ConfirmHash: codeHash(id, code)})
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrBadCode
	}
	return nil
}

// ErrNotFound is returned for an unknown channel.
var ErrNotFound = errors.New("channel not found")

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
	ch, err := c.q.CreateChannel(ctx, dbsqlc.CreateChannelParams{UserID: userID, Kind: kind, Target: target, Config: cfg})
	if err != nil {
		return ch, err
	}
	return ch, nil
}

// Delete removes a channel and reports how many rows went away.
func (c *Channels) Delete(ctx context.Context, userID, id uuid.UUID) (int64, error) {
	return c.q.DeleteChannel(ctx, dbsqlc.DeleteChannelParams{ID: id, UserID: userID})
}
