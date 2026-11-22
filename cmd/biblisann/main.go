// Command biblisann runs the Biblisann server and its maintenance subcommands.
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/larueli/biblisann/internal/api"
	"github.com/larueli/biblisann/internal/app"
	"github.com/larueli/biblisann/internal/config"
	"github.com/larueli/biblisann/internal/db"
	"github.com/larueli/biblisann/internal/version"
)

// specVersion is the placeholder version in the exported spec: the committed file must not change
// with every build. The running server serves the real version at /openapi.json.
const specVersion = "0.0.0"

const usage = `usage: biblisann [command]

commands:
  serve        run the server (default)
  migrate      apply database migrations and exit
  healthcheck  probe the local /healthz endpoint (for container health checks)
  openapi      print the OpenAPI 3.1 document as JSON
  version      print the version
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	cmd := "serve"
	if len(args) > 0 {
		cmd = args[0]
	}
	var err error
	switch cmd {
	case "serve":
		err = serve(ctx, stderr)
	case "migrate":
		err = migrate(ctx, stderr)
	case "healthcheck":
		err = healthcheck(ctx)
	case "openapi":
		var spec []byte
		if spec, err = api.OpenAPIJSON(specVersion); err == nil {
			_, err = fmt.Fprintln(stdout, string(spec))
		}
	case "version":
		_, err = fmt.Fprintln(stdout, version.Version())
	case "help", "-h", "--help":
		_, _ = fmt.Fprint(stdout, usage)
	default:
		_, _ = fmt.Fprint(stderr, usage)
		return 2
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

func newLogger(cfg config.Config, w io.Writer) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.LogLevel}
	if cfg.LogFormat == "text" {
		return slog.New(slog.NewTextHandler(w, opts))
	}
	return slog.New(slog.NewJSONHandler(w, opts))
}

func serve(ctx context.Context, stderr io.Writer) error {
	cfg, err := config.Load(config.Sources{}, config.Options{RequireDatabase: true})
	if err != nil {
		return err
	}
	log := newLogger(cfg, stderr)
	log.Info("starting", "config", cfg)
	return app.Run(ctx, cfg, log)
}

func migrate(ctx context.Context, stderr io.Writer) error {
	cfg, err := config.Load(config.Sources{}, config.Options{RequireDatabase: true})
	if err != nil {
		return err
	}
	log := newLogger(cfg, stderr)
	pool, err := db.Open(ctx, cfg.DatabaseURL.Reveal())
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		return err
	}
	log.Info("migrations applied")
	return nil
}

// healthcheck probes the server on the loopback interface. Distroless images have no curl.
func healthcheck(ctx context.Context) error {
	cfg, err := config.Load(config.Sources{}, config.Options{})
	if err != nil {
		return err
	}
	_, port, err := net.SplitHostPort(cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("invalid listen address: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+net.JoinHostPort("127.0.0.1", port)+"/healthz", http.NoBody)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req) //nolint:forbidigo,bodyclose // fixed loopback URL, not user input
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unhealthy: status %d", resp.StatusCode)
	}
	return nil
}
