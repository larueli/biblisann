package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/larueli/biblisann/internal/account"
	"github.com/larueli/biblisann/internal/api"
	"github.com/larueli/biblisann/internal/auth"
	"github.com/larueli/biblisann/internal/authz"
	"github.com/larueli/biblisann/internal/backup"
	"github.com/larueli/biblisann/internal/catalog"
	"github.com/larueli/biblisann/internal/config"
	"github.com/larueli/biblisann/internal/db"
	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
	"github.com/larueli/biblisann/internal/gc"
	"github.com/larueli/biblisann/internal/hardcover"
	"github.com/larueli/biblisann/internal/hcsync"
	"github.com/larueli/biblisann/internal/httpmw"
	"github.com/larueli/biblisann/internal/importer"
	"github.com/larueli/biblisann/internal/jobs"
	"github.com/larueli/biblisann/internal/library"
	"github.com/larueli/biblisann/internal/lists"
	"github.com/larueli/biblisann/internal/loans"
	"github.com/larueli/biblisann/internal/mcpserver"
	"github.com/larueli/biblisann/internal/notify"
	"github.com/larueli/biblisann/internal/oauth"
	"github.com/larueli/biblisann/internal/oidc"
	"github.com/larueli/biblisann/internal/opds"
	"github.com/larueli/biblisann/internal/passkeys"
	"github.com/larueli/biblisann/internal/providers"
	"github.com/larueli/biblisann/internal/ratelimit"
	"github.com/larueli/biblisann/internal/reading"
	"github.com/larueli/biblisann/internal/safehttp"
	"github.com/larueli/biblisann/internal/scan"
	"github.com/larueli/biblisann/internal/scim"
	"github.com/larueli/biblisann/internal/storage"
	"github.com/larueli/biblisann/internal/telemetry"
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
	if err := CheckSecretKey(ctx, cfg, pool); err != nil {
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
	shutdownTracing, err := telemetry.SetupTracing(ctx, version.Version())
	if err != nil {
		return fmt.Errorf("tracing: %w", err)
	}
	defer func() {
		flush, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownTracing(flush)
	}()
	handler := Wrap(root, cfg, log)
	if telemetry.TracingEnabled() {
		handler = telemetry.Middleware(handler) // outermost: every middleware below sees the span
		log.Info("OpenTelemetry tracing is on")
	}

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
	if cfg.SecretKey != "" {
		key, _ := base64.StdEncoding.DecodeString(cfg.SecretKey.Reveal())
		if err := authSvc.EnableMFA(key); err != nil {
			return nil, nil, err
		}
	}
	covers, err := OpenStorage(context.Background(), cfg, cfg.StorageBackend)
	if err != nil {
		return nil, nil, err
	}
	fetcher := safehttp.New(safehttp.Options{UserAgent: "Biblisann/" + version.Version() + " (+https://github.com/larueli/biblisann)"})
	provs := []providers.Provider{providers.BnF{Fetcher: fetcher}, providers.OpenLibrary{Fetcher: fetcher},
		providers.GoogleBooks{Fetcher: fetcher, APIKey: cfg.GoogleBooksKey.Reveal()},
		providers.LibraryOfCongress(fetcher), providers.K10plus(fetcher)}
	if cfg.AddonProvidersFile != "" {
		addons, err := loadAddons(cfg.AddonProvidersFile, fetcher)
		if err != nil {
			return nil, nil, err
		}
		provs = append(provs, addons...)
	}
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

	var mail auth.Mailer
	mailEnabled := false
	senders := []notify.Sender{notify.Webhook{HTTP: fetcher}, notify.Ntfy{HTTP: fetcher}}
	if cfg.SMTPHost != "" {
		mailer := notify.Email{Config: notify.SMTPConfig{Host: cfg.SMTPHost, Port: cfg.SMTPPort,
			Username: cfg.SMTPUsername, Password: cfg.SMTPPassword.Reveal(), From: cfg.SMTPFrom, Mode: cfg.SMTPMode}}
		senders = append(senders, mailer)
		authSvc.SetMailer(mailer)
		mail = mailer
		mailEnabled = true
	}
	if cfg.VAPIDPublicKey != "" {
		subject := cfg.VAPIDSubject
		if subject == "" {
			subject = cfg.BaseURL.String()
		}
		senders = append(senders, notify.WebPush{HTTP: fetcher.Std(), PublicKey: cfg.VAPIDPublicKey,
			PrivateKey: cfg.VAPIDPrivateKey.Reveal(), Subject: subject})
	}
	oauthSvc := oauth.New(pool, cfg.BaseURL.String())
	dispatcher := notify.NewDispatcher(q, log, senders...)
	dispatcher.EmailDailyCap = cfg.EmailDailyCap
	channels := notify.NewChannels(q)
	if mailEnabled {
		channels.SetMailer(mail, cfg.EmailDailyCap)
	}

	var hc interface {
		Refresh(ctx context.Context, workID uuid.UUID) error
	}
	var hcSyncer jobs.Syncer
	if cfg.HardcoverToken != "" {
		h := hardcover.New(pool, fetcher, cfg.HardcoverToken.Reveal())
		h.RefreshDays = cfg.HardcoverRefreshDays
		h.Interval = time.Minute / time.Duration(cfg.HardcoverPerMinute)
		hc, hcSyncer = h, h
	}
	var hcs *hcsync.Service
	var userSyncer jobs.UserSyncer
	if cfg.SecretKey != "" {
		key, _ := base64.StdEncoding.DecodeString(cfg.SecretKey.Reveal())
		hcs = hcsync.New(pool, key, func(token string) hcsync.Remote { return hardcover.NewUserClient(fetcher, token) }, readingSvc)
		hcs.Interval = time.Minute / time.Duration(cfg.HardcoverPerMinute)
		userSyncer = hcs
	}
	jc, err := jobs.New(pool, dispatcher, log, func(ctx context.Context) error {
		_ = oauthSvc.Purge(ctx)
		if _, err := authSvc.PurgeExpired(ctx); err != nil {
			return err
		}
		opts := gc.Defaults()
		opts.SoftDeleteDays = cfg.GCSoftDeleteDays
		rep, err := gc.Run(ctx, pool, covers, opts, log)
		if err != nil {
			return err
		}
		if rep != (gc.Report{}) {
			log.InfoContext(ctx, "garbage collection", "removed", rep.String())
		}
		return nil
	}, hcSyncer, userSyncer, catSvc)
	if err != nil {
		return nil, nil, err
	}
	var limiter ratelimit.Limiter = ratelimit.NewMemory(10, 6*time.Second)
	if cfg.ValkeyURL != "" {
		v, err := ratelimit.NewValkey(cfg.ValkeyURL.Reveal(), 10, 6*time.Second, log)
		if err != nil {
			return nil, nil, err
		}
		limiter = v
	}
	var quota ratelimit.Meter
	if cfg.APIRateLimit > 0 {
		every := time.Minute / time.Duration(cfg.APIRateLimit)
		quota = ratelimit.NewMemory(cfg.APIRateLimit, every)
		if cfg.ValkeyURL != "" {
			v, err := ratelimit.NewValkey(cfg.ValkeyURL.Reveal(), cfg.APIRateLimit, every, log)
			if err != nil {
				return nil, nil, err
			}
			quota = v
		}
	}
	var passkeySvc *passkeys.Service
	if cfg.Passkeys {
		if ps, err := passkeys.New(pool, cfg.BaseURL, "Biblisann"); err != nil {
			log.Warn("passkeys disabled", "error", err)
		} else {
			passkeySvc = ps
		}
	}
	var oidcSvc *oidc.Service
	if cfg.OIDCIssuer != "" {
		oidcSvc = oidc.New(oidc.Config{Issuer: cfg.OIDCIssuer, ClientID: cfg.OIDCClientID, ClientSecret: cfg.OIDCClientSecret.Reveal(),
			RedirectURL: cfg.BaseURL.String() + "/v1/auth/oidc/callback", Scopes: cfg.OIDCScopes, GroupsClaim: cfg.OIDCGroupsClaim,
			AdminGroup: cfg.OIDCAdminGroup, AutoCreate: cfg.OIDCAutoCreate, Label: cfg.OIDCLabel}, pool, authSvc, fetcher.Std())
	}
	h, _ := api.New(api.Deps{WebPushKey: cfg.VAPIDPublicKey, HCSync: hcs, SourceURL: cfg.SourceURL, Metrics: telemetry.New(version.Version(), pool), MetricsToken: cfg.MetricsToken.Reveal(), MetricsPublic: cfg.MetricsPublic, Account: account.NewService(pool, libSvc, catSvc, listsSvc, loansSvc, readingSvc), Hardcover: hc, Passkeys: passkeySvc, OPDS: opds.New(tokenSvc, libSvc, catSvc, cfg.BaseURL.String()), OIDC: oidcSvc, SCIM: scim.New(pool, tokenSvc, cfg.BaseURL.String()),
		Version: version.Version(), DB: pool, Web: webui.Handler(), Log: log, BaseURL: cfg.BaseURL,
		Auth: authSvc, Mailer: mail, Library: libSvc, Catalog: catSvc, Scan: scanSvc, Reading: readingSvc,
		Lists: listsSvc, Loans: loansSvc,
		MCP: mcpserver.Handler(mcpserver.Deps{Version: version.Version(), Library: libSvc, Scan: scanSvc, Reading: readingSvc, Lists: listsSvc, Loans: loansSvc,
			Charge: func(p *authz.Principal, cost int) (time.Duration, bool) {
				if quota == nil {
					return 0, true
				}
				d := quota.Take(api.QuotaKey(p), cost)
				return d.RetryAfter, d.Allowed
			}}),
		Importer: importer.New(pool, catSvc, libSvc, readingSvc, listsSvc),
		Backup: func(ctx context.Context, w io.Writer) error {
			return backup.Export(ctx, pool, covers, w, version.Version())
		},
		TokenSvc: tokenSvc, Tokens: api.MultiResolver{tokenSvc, oauthSvc}, OAuth: oauthSvc, Notify: dispatcher, Channels: channels,
		Limiter: limiter, Quota: quota, Jobs: jc,
	})
	return h, jc, nil
}

// Wrap applies the shared middleware chain.
func Wrap(h http.Handler, cfg config.Config, log *slog.Logger) http.Handler {
	probe := func(r *http.Request) bool {
		switch r.URL.Path {
		case "/healthz", "/livez", "/readyz", "/startupz", "/health", "/metrics":
			return true
		}
		return false
	}
	return httpmw.Chain(limitBody(h),
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

var addonName = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,30}$`)

// loadAddons reads the addon list. Names must be new and well formed; URLs must be https.
func loadAddons(path string, f providers.Fetcher) ([]providers.Provider, error) {
	data, err := os.ReadFile(path) //nolint:gosec // operator-provided configuration path
	if err != nil {
		return nil, fmt.Errorf("addon providers: %w", err)
	}
	var list []struct {
		Name  string `json:"name"`
		URL   string `json:"url"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("addon providers: invalid JSON: %w", err)
	}
	reserved := map[string]bool{"bnf": true, "openlibrary": true, "google": true, "loc": true, "k10plus": true, "static": true}
	var out []providers.Provider
	for _, a := range list {
		if !addonName.MatchString(a.Name) || reserved[a.Name] || !strings.HasPrefix(a.URL, "https://") {
			return nil, fmt.Errorf("addon providers: invalid entry %q (name must be new, lowercase; url must be https)", a.Name)
		}
		out = append(out, providers.Addon{ProviderName: a.Name, Fetcher: f, URL: a.URL, Token: a.Token})
	}
	return out, nil
}

// limitBody caps request bodies: 10 MiB everywhere, 64 MiB for archive imports.
func limitBody(next http.Handler) http.Handler {
	small, large := http.MaxBytesHandler(next, maxRequestBody), http.MaxBytesHandler(next, 64<<20)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/libraries/import" || r.URL.Path == "/v1/me/import" {
			large.ServeHTTP(w, r)
			return
		}
		small.ServeHTTP(w, r)
	})
}
