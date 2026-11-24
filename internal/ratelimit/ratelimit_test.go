package ratelimit

import (
	"testing"
	"time"
)

func TestMemory(t *testing.T) {
	now := time.Unix(1000, 0)
	m := NewMemory(3, time.Second)
	m.now = func() time.Time { return now }

	for i := range 3 {
		if ok, _ := m.Allow("a"); !ok {
			t.Fatalf("burst event %d refused", i)
		}
	}
	ok, wait := m.Allow("a")
	if ok || wait <= 0 || wait > time.Second {
		t.Fatalf("4th event: ok=%v wait=%v", ok, wait)
	}
	if ok, _ := m.Allow("b"); !ok {
		t.Error("keys must be independent")
	}
	now = now.Add(1100 * time.Millisecond)
	if ok, _ := m.Allow("a"); !ok {
		t.Error("token should have refilled")
	}
}

func TestMemoryFailsClosedWhenFull(t *testing.T) {
	m := NewMemory(1, time.Hour)
	m.maxItems = 2
	m.Allow("a")
	m.Allow("b")
	if ok, _ := m.Allow("c"); ok {
		t.Error("new keys must be refused when the table is full")
	}
	if ok, _ := m.Allow("a"); ok {
		t.Error("existing exhausted key stays refused")
	}
}

func TestMemoryTakeReportsTheQuota(t *testing.T) {
	m := NewMemory(10, time.Second)
	now := time.Now()
	m.now = func() time.Time { return now }
	d := m.Take("u", 1)
	if !d.Allowed || d.Limit != 10 || d.Remaining != 9 || d.Reset != time.Second {
		t.Fatalf("first take: %+v", d)
	}
	if d := m.Take("u", 8); !d.Allowed || d.Remaining != 1 {
		t.Fatalf("heavy take: %+v", d)
	}
	d = m.Take("u", 5)
	if d.Allowed || d.RetryAfter != 4*time.Second || d.Remaining != 1 {
		t.Fatalf("over quota: %+v", d)
	}
	now = now.Add(4 * time.Second)
	if d := m.Take("u", 5); !d.Allowed {
		t.Fatalf("after waiting: %+v", d)
	}
}
