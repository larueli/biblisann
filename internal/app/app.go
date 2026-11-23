package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/larueli/biblisann/internal/api"
	"github.com/larueli/biblisann/internal/auth"
	"github.com/larueli/biblisann/internal/backup"
	"github.com/larueli/biblisann/internal/catalog"
	"github.com/larueli/biblisann/internal/config"
	"github.com/larueli/biblisann/internal/db"
	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
	"github.com/larueli/biblisann/internal/httpmw"
	"github.com/larueli/biblisann/internal/importer"
	"github.com/larueli/biblisann/internal/jobs"
	"github.com/larueli/biblisann/internal/library"
	"github.com/larueli/biblisann/internal/lists"
	"github.com/larueli/biblisann/internal/loans"
	"github.com/larueli/biblisann/internal/mcpserver"
	"github.com/larueli/biblisann/internal/notify"
	"github.com/larueli/biblisann/internal/providers"
	"github.com/larueli/biblisann/internal/ratelimit"
	"github.com/larueli/biblisann/internal/reading"
	"github.com/larueli/biblisann/internal/safehttp"
	"github.com/larueli/biblisann/internal/scan"
	"github.com/larueli/biblisann/internal/storage"
	"github.com/larueli/biblisann/internal/tokens"
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

	if err := jobs.Migrate(ctx, pool); err != nil {
		return err
	}
	root, jc, err := Build(cfg, log, pool)
	if err != nil {
		return err
	}
	if err := jc.Start(ctx); err != nil {
		return fmt.Errorf("start jobs: %w", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := jc.Stop(stopCtx); err != nil {
			log.Warn("jobs did not stop cleanly", "error", err)
		}
	}()
	handler := Wrap(root, cfg, log)

	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	log.InfoContext(ctx, "listening", "addr", ln.Addr().String(), "version", version.Version())
	return Serve(ctx, ln, handler, cfg.ShutdownTimeout, log)
}

// Build constructs the services and the HTTP handler (without the shared middleware) and the
// background job client (not started).
func Build(cfg config.Config, log *slog.Logger, pool *pgxpool.Pool) (http.Handler, *jobs.Client, error) {
	authSvc, err := auth.NewService(pool, auth.RegistrationMode(cfg.Registration))
	if err != nil {
		return nil, nil, err
	}
	covers, err := OpenStorage(context.Background(), cfg, cfg.StorageBackend)
	if err != nil {
		return nil, nil, err
	}
	fetcher := safehttp.New(safehttp.Options{UserAgent: "Biblisann/" + version.Version() + " (+https://github.com/larueli/biblisann)"})
	provs := []providers.Provider{providers.BnF{Fetcher: fetcher}, providers.OpenLibrary{Fetcher: fetcher}}
	if cfg.StaticProviderFile != "" {
		sp, err := providers.LoadStatic(cfg.StaticProviderFile)
		if err != nil {
			return nil, nil, err
		}
		provs = append(provs, sp)
	}
	catSvc := catalog.NewService(pool, catalog.Config{
		Providers:         provs,
		DefaultProviders:  cfg.Providers,
		DisabledProviders: cfg.DisabledProviders,
		Covers:            covers,
		Fetcher:           fetcher,
		Log:               log,
	})
	libSvc := library.NewService(pool)
	scanSvc := scan.New(catSvc, libSvc)
	readingSvc := reading.NewService(pool)
	listsSvc := lists.NewService(pool, libSvc)
	loansSvc := loans.NewService(pool, libSvc)
	tokenSvc := tokens.NewService(pool)
	q := dbsqlc.New(pool)

	senders := []notify.Sender{notify.Webhook{HTTP: fetcher}, notify.Ntfy{HTTP: fetcher}}
	if cfg.SMTPHost != "" {
		senders = append(senders, notify.Email{Config: notify.SMTPConfig{Host: cfg.SMTPHost, Port: cfg.SMTPPort,
			Username: cfg.SMTPUsername, Password: cfg.SMTPPassword.Reveal(), From: cfg.SMTPFrom, Mode: cfg.SMTPMode}})
	}
	if cfg.VAPIDPublicKey != "" {
		subject := cfg.VAPIDSubject
		if subject == "" {
			subject = cfg.BaseURL.String()
		}
		senders = append(senders, notify.WebPush{HTTP: fetcher.Std(), PublicKey: cfg.VAPIDPublicKey,
			PrivateKey: cfg.VAPIDPrivateKey.Reveal(), Subject: subject})
	}
	dispatcher := notify.NewDispatcher(q, log, senders...)

	jc, err := jobs.New(pool, dispatcher, log, func(ctx context.Context) error {
		_, err := authSvc.PurgeExpired(ctx)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	h, _ := api.New(api.Deps{
		Version: version.Version(), DB: pool, Web: webui.Handler(), Log: log, BaseURL: cfg.BaseURL,
		Auth: authSvc, Library: libSvc, Catalog: catSvc, Scan: scanSvc, Reading: readingSvc,
		Lists: listsSvc, Loans: loansSvc,
		MCP:      mcpserver.Handler(mcpserver.Deps{Version: version.Version(), Library: libSvc, Scan: scanSvc, Reading: readingSvc, Lists: listsSvc, Loans: loansSvc}),
		Importer: importer.New(pool, catSvc, libSvc, readingSvc, listsSvc),
		Backup: func(ctx context.Context, w io.Writer) error {
			return backup.Export(ctx, pool, covers, w, version.Version())
		},
		TokenSvc: tokenSvc, Tokens: tokenSvc, Notify: dispatcher, Channels: notify.NewChannels(q),
		Limiter: ratelimit.NewMemory(10, 6*time.Second),
	})
	return h, jc, nil
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

// OpenStorage opens the cover store of the given kind ("local" or "s3") from the configuration.
func OpenStorage(ctx context.Context, cfg config.Config, kind string) (storage.Store, error) {
	switch kind {
	case "local":
		return storage.NewLocal(cfg.CoversDir)
	case "s3":
		return storage.NewS3(ctx, storage.S3Config{Endpoint: cfg.S3Endpoint, Bucket: cfg.S3Bucket, Region: cfg.S3Region,
			AccessKey: cfg.S3AccessKey, SecretKey: cfg.S3SecretKey.Reveal(), UseSSL: cfg.S3UseSSL, Prefix: cfg.S3Prefix})
	}
	return nil, fmt.Errorf("unknown storage backend %q", kind)
}
