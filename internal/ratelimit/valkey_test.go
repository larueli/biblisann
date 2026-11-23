package ratelimit

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestValkeyGCRA(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: needs Docker")
	}
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()
	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{Started: true, ContainerRequest: testcontainers.ContainerRequest{
		Image: "valkey/valkey:8-alpine", ExposedPorts: []string{"6379/tcp"}, Cmd: []string{"valkey-server", "--save", "", "--maxmemory", "32mb"},
		HostConfigModifier: func(hc *container.HostConfig) { hc.Memory = 96 << 20; hc.MemorySwap = 96 << 20 },
		WaitingFor:         wait.ForListeningPort("6379/tcp").WithStartupTimeout(60 * time.Second)}})
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, ctr)
	ep, _ := ctr.PortEndpoint(ctx, "6379/tcp", "")
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	// two "replicas" share the budget
	a, err := NewValkey("redis://"+ep, 3, time.Second, log)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewValkey("redis://"+ep, 3, time.Second, log)
	defer func() { _ = a.Close(); _ = b.Close() }()
	if err := a.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	allowed := 0
	for i := range 6 {
		l := a
		if i%2 == 1 {
			l = b
		}
		if ok, _ := l.Allow("login|1.2.3.4"); ok {
			allowed++
		}
	}
	if allowed != 3 {
		t.Errorf("burst shared across replicas: %d allowed, want 3", allowed)
	}
	if ok, wait := a.Allow("login|1.2.3.4"); ok || wait <= 0 {
		t.Errorf("must be limited: %v %v", ok, wait)
	}
	if ok, _ := a.Allow("login|other"); !ok {
		t.Error("keys are independent")
	}
	time.Sleep(1100 * time.Millisecond)
	if ok, _ := a.Allow("login|1.2.3.4"); !ok {
		t.Error("a token must have come back")
	}
	// unreachable server: falls back to the local limiter instead of failing open or closed
	dead, _ := NewValkey("redis://127.0.0.1:1", 1, time.Hour, log)
	if ok, _ := dead.Allow("k"); !ok {
		t.Error("first request through the fallback")
	}
	if ok, _ := dead.Allow("k"); ok {
		t.Error("fallback must still limit")
	}
}
