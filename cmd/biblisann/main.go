// Command biblisann runs the Biblisann server and its maintenance subcommands.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/larueli/biblisann/internal/api"
	"github.com/larueli/biblisann/internal/app"
	"github.com/larueli/biblisann/internal/backup"
	"github.com/larueli/biblisann/internal/config"
	"github.com/larueli/biblisann/internal/db"
	"github.com/larueli/biblisann/internal/gc"
	"github.com/larueli/biblisann/internal/storage"
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
  backup       write a logical backup (tar) to stdout
  restore      load a backup from stdin into an empty instance
  migrate-covers <from> <to>  copy cover files between local and s3
  gc           remove unreferenced data now (also runs daily in the server)
  vapid        generate Web Push (VAPID) keys
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
	case "backup":
		err = backupCmd(ctx, stdout)
	case "restore":
		err = restoreCmd(ctx, os.Stdin)
	case "migrate-covers":
		err = migrateCovers(ctx, args[1:])
	case "gc":
		err = gcCmd(ctx, stdout)
	case "vapid":
		var priv, pub string
		if priv, pub, err = webpush.GenerateVAPIDKeys(); err == nil {
			_, err = fmt.Fprintf(stdout, "BIBLISANN_VAPID_PUBLIC_KEY=%s\nBIBLISANN_VAPID_PRIVATE_KEY=%s\n", pub, priv)
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

func openPool(ctx context.Context) (config.Config, *pgxpool.Pool, error) {
	cfg, err := config.Load(config.Sources{}, config.Options{RequireDatabase: true})
	if err != nil {
		return cfg, nil, err
	}
	pool, err := db.Open(ctx, cfg.DatabaseURL.Reveal())
	return cfg, pool, err
}

func backupCmd(ctx context.Context, out io.Writer) error {
	cfg, pool, err := openPool(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()
	covers, err := app.OpenStorage(ctx, cfg, cfg.StorageBackend)
	if err != nil {
		return err
	}
	return backup.Export(ctx, pool, covers, out, version.Version())
}

func restoreCmd(ctx context.Context, in io.Reader) error {
	cfg, pool, err := openPool(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		return err
	}
	covers, err := app.OpenStorage(ctx, cfg, cfg.StorageBackend)
	if err != nil {
		return err
	}
	return backup.Restore(ctx, pool, covers, in)
}

// migrateCovers copies every cover file from one backend to the other. Switch
// BIBLISANN_STORAGE_BACKEND afterwards. It can be repeated safely.
func migrateCovers(ctx context.Context, args []string) error {
	if len(args) != 2 {
		return errors.New("usage: biblisann migrate-covers <local|s3> <local|s3>")
	}
	cfg, err := config.Load(config.Sources{}, config.Options{})
	if err != nil {
		return err
	}
	src, err := app.OpenStorage(ctx, cfg, args[0])
	if err != nil {
		return err
	}
	dst, err := app.OpenStorage(ctx, cfg, args[1])
	if err != nil {
		return err
	}
	n, err := storage.Copy(ctx, src, dst)
	if err != nil {
		return fmt.Errorf("copied %d files before failing: %w", n, err)
	}
	_, _ = fmt.Fprintf(os.Stderr, "copied %d files from %s to %s\n", n, args[0], args[1])
	return nil
}

func gcCmd(ctx context.Context, out io.Writer) error {
	cfg, pool, err := openPool(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()
	covers, err := app.OpenStorage(ctx, cfg, cfg.StorageBackend)
	if err != nil {
		return err
	}
	opts := gc.Defaults()
	opts.SoftDeleteDays = cfg.GCSoftDeleteDays
	rep, err := gc.Run(ctx, pool, covers, opts, newLogger(cfg, os.Stderr))
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, rep.String())
	return err
}
