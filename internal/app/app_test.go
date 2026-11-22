package app

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/larueli/biblisann/internal/config"
)

func TestServeGracefulShutdown(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		_, _ = io.WriteString(w, "done")
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	served := make(chan error, 1)
	go func() { served <- Serve(ctx, ln, h, 5*time.Second, log) }()

	respc := make(chan string, 1)
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String()) //nolint:forbidigo,noctx // test against loopback
		if err != nil {
			respc <- err.Error()
			return
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		respc <- string(b)
	}()
	<-started
	cancel() // shutdown requested while a request is in flight
	time.Sleep(50 * time.Millisecond)
	close(release)

	if got := <-respc; got != "done" {
		t.Errorf("in-flight request was not drained: %q", got)
	}
	if err := <-served; err != nil {
		t.Errorf("Serve returned %v", err)
	}
}

func TestWrapAppliesMiddleware(t *testing.T) {
	cfg, err := config.Load(config.Sources{LookupEnv: func(string) (string, bool) { return "", false }}, config.Options{})
	if err != nil {
		t.Fatal(err)
	}
	h := Wrap(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	hdr := newRecorderServer(h)
	if hdr.Get("X-Content-Type-Options") != "nosniff" || hdr.Get("X-Request-ID") == "" {
		t.Error("middleware chain not applied")
	}
}
