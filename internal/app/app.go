package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/larueli/biblisann/internal/api"
	"github.com/larueli/biblisann/internal/config"
	"github.com/larueli/biblisann/internal/db"
	"github.com/larueli/biblisann/internal/httpmw"
	"github.com/larueli/biblisann/internal/version"
	"github.com/larueli/biblisann/internal/webui"
)

const maxRequestBody = 10 << 20

// Run starts the server and blocks until ctx is cancelled and the server has drained.
func Run(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	pool, err := db.Open(ctx, cfg.DatabaseURL.Reveal())
	if err != nil {
		return err
	}
	defer pool.Close()

	if cfg.MigrateOnStart {
		log.InfoContext(ctx, "applying migrations")
		if err := db.Migrate(ctx, pool); err != nil {
			return err
		}
	}

	root, _ := api.New(api.Deps{Version: version.Version(), DB: pool, Web: webui.Handler()})
	handler := Wrap(root, cfg, log)

	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	log.InfoContext(ctx, "listening", "addr", ln.Addr().String(), "version", version.Version())
	return Serve(ctx, ln, handler, cfg.ShutdownTimeout, log)
}

// Wrap applies the shared middleware chain.
func Wrap(h http.Handler, cfg config.Config, log *slog.Logger) http.Handler {
	probe := func(r *http.Request) bool { return r.URL.Path == "/healthz" || r.URL.Path == "/readyz" }
	return httpmw.Chain(http.MaxBytesHandler(h, maxRequestBody),
		httpmw.WithRequestID(),
		httpmw.WithClientIP(cfg.TrustedProxies),
		httpmw.AccessLog(log, probe),
		httpmw.Recover(log),
		httpmw.SecurityHeaders(cfg.BaseURL.Scheme == "https"),
	)
}

// Serve serves h on ln until ctx is cancelled, then drains in-flight requests for at most
// shutdownTimeout.
func Serve(ctx context.Context, ln net.Listener, h http.Handler, shutdownTimeout time.Duration, log *slog.Logger) error {
	srv := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
		BaseContext:       func(net.Listener) context.Context { return context.WithoutCancel(ctx) },
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()

	select {
	case err := <-errc:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}
	log.Info("shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		_ = srv.Close()
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}
