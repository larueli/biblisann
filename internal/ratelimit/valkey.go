package ratelimit

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// gcra is the generic cell rate algorithm: atomic, one key per limited subject, no clock skew
// between replicas because the server's TIME is used.
var gcra = redis.NewScript(`
local t = redis.call('TIME')
local now = t[1] * 1000 + math.floor(t[2] / 1000)
local interval = tonumber(ARGV[1])
local burst = tonumber(ARGV[2])
local tat = tonumber(redis.call('GET', KEYS[1]))
if not tat or tat < now then tat = now end
local allowAt = tat - (burst - 1) * interval
if now < allowAt then
  return {0, allowAt - now}
end
redis.call('SET', KEYS[1], tat + interval, 'PX', burst * interval + 1000)
return {1, 0}
`)

// Valkey is a limiter shared by all replicas (works with Valkey and Redis). When the server is
// unreachable it falls back to the per-process limiter, so a cache outage never opens the door
// completely nor locks everybody out.
type Valkey struct {
	c        *redis.Client
	interval time.Duration
	burst    int
	prefix   string
	fallback *Memory
	log      *slog.Logger
}

// NewValkey connects lazily using a redis:// or rediss:// URL.
func NewValkey(url string, burst int, every time.Duration, log *slog.Logger) (*Valkey, error) {
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("ratelimit: invalid valkey URL") // the URL may hold a password
	}
	opt.DialTimeout, opt.ReadTimeout, opt.WriteTimeout = 2*time.Second, time.Second, time.Second
	return &Valkey{c: redis.NewClient(opt), interval: every, burst: burst, prefix: "bb:rl:", fallback: NewMemory(burst, every), log: log}, nil
}

// Close releases the connection pool.
func (v *Valkey) Close() error { return v.c.Close() }

// Ping checks connectivity (used at startup to warn early).
func (v *Valkey) Ping(ctx context.Context) error { return v.c.Ping(ctx).Err() }

// Allow implements Limiter.
func (v *Valkey) Allow(key string) (bool, time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	res, err := gcra.Run(ctx, v.c, []string{v.prefix + key}, v.interval.Milliseconds(), v.burst).Int64Slice()
	if err != nil || len(res) != 2 {
		v.log.Warn("valkey rate limit unavailable, using the local limiter", "error", err)
		return v.fallback.Allow(key)
	}
	if res[0] == 1 {
		return true, 0
	}
	return false, time.Duration(res[1]) * time.Millisecond
}
