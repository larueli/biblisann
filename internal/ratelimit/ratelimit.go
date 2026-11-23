// Package ratelimit provides request rate limiting. The in-memory limiter is per process; a
// shared (Valkey) implementation can satisfy the same interface for HA deployments.
package ratelimit

import (
	"sync"
	"time"
)

// Limiter decides whether an action identified by key may proceed now.
type Limiter interface {
	// Allow consumes one token. When it returns false, retryAfter says when to try again.
	Allow(key string) (ok bool, retryAfter time.Duration)
}

// Memory is a token-bucket limiter held in process memory.
type Memory struct {
	rate     float64 // tokens per second
	burst    float64
	now      func() time.Time
	mu       sync.Mutex
	buckets  map[string]*bucket
	lastGC   time.Time
	maxItems int
}

type bucket struct {
	tokens float64
	last   time.Time
}

// NewMemory allows `burst` immediate events, refilled at one per `every`.
func NewMemory(burst int, every time.Duration) *Memory {
	return &Memory{
		rate: 1 / every.Seconds(), burst: float64(burst), now: time.Now,
		buckets: map[string]*bucket{}, maxItems: 100_000,
	}
}

// Allow implements Limiter.
func (m *Memory) Allow(key string) (bool, time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	m.gc(now)
	b, ok := m.buckets[key]
	if !ok {
		if len(m.buckets) >= m.maxItems {
			// Under a key-flooding attack, fail closed rather than grow without bound.
			return false, time.Second
		}
		b = &bucket{tokens: m.burst, last: now}
		m.buckets[key] = b
	}
	b.tokens = min(m.burst, b.tokens+now.Sub(b.last).Seconds()*m.rate)
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	return false, time.Duration((1 - b.tokens) / m.rate * float64(time.Second))
}

// gc drops buckets that have been full for a while. Called with the lock held.
func (m *Memory) gc(now time.Time) {
	if now.Sub(m.lastGC) < time.Minute {
		return
	}
	m.lastGC = now
	idle := time.Duration(m.burst/m.rate*float64(time.Second)) + time.Minute
	for k, b := range m.buckets {
		if now.Sub(b.last) > idle {
			delete(m.buckets, k)
		}
	}
}
