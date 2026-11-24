package config

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Config is the validated service configuration.
type Config struct {
	ListenAddr      string
	BaseURL         *url.URL
	TrustedProxies  []netip.Prefix
	DatabaseURL     Secret
	LogLevel        slog.Level
	LogFormat       string // "json" or "text"
	ShutdownTimeout time.Duration
	MigrateOnStart  bool
	// Registration is "first-user", "open" or "closed".
	Registration string
	CoversDir    string
	// Providers is the default ordered list of metadata providers; DisabledProviders are off
	// for everyone (ADR 0013).
	Providers         []string
	DisabledProviders []string
	// StaticProviderFile is an optional JSON file of ISBN -> metadata served by the "static"
	// provider (air-gapped instances, curated data, tests).
	StaticProviderFile string
	// AddonProvidersFile lists out-of-process providers: [{"name":"x","url":"https://...","token":"..."}].
	// It holds secrets: keep it out of the repository (its path is plain configuration).
	AddonProvidersFile string
	GoogleBooksKey     Secret
	HardcoverToken     Secret
	// SourceURL is where the exact source of this running version can be obtained (AGPL-3.0 section 13).
	// Forks that modify the code must point it at their own source.
	SourceURL string
	// MetricsToken protects /metrics (bearer). MetricsPublic removes the authentication (the operator
	// protects the route, for example at the reverse proxy). With neither, /metrics does not exist.
	MetricsToken  Secret
	MetricsPublic bool
	// GCSoftDeleteDays: how long a deleted copy can be restored before it is purged.
	GCSoftDeleteDays int
	// HardcoverRefreshDays: recheck a rated book after this many days. HardcoverPerMinute: request
	// budget (Hardcover allows 60 per minute).
	HardcoverRefreshDays int
	HardcoverPerMinute   int
	// ValkeyURL (redis:// or rediss://, may hold a password) shares rate limits across replicas.
	ValkeyURL Secret

	// Single sign-on (optional): enabled when OIDCIssuer is set.
	OIDCIssuer       string
	OIDCClientID     string
	OIDCClientSecret Secret
	OIDCScopes       []string
	OIDCGroupsClaim  string
	OIDCAdminGroup   string
	OIDCAutoCreate   bool
	OIDCLabel        string

	// Passkeys enables WebAuthn sign-in (set false when sign-in is delegated to OIDC).
	Passkeys bool

	// SecretKey (base64, 32 bytes) seals secrets at rest (TOTP). Without it two-factor is off.
	// Generate with: openssl rand -base64 32
	SecretKey Secret

	// Cover storage: "local" (CoversDir) or "s3".
	StorageBackend string
	S3Endpoint     string
	S3Bucket       string
	S3Region       string
	S3AccessKey    string
	S3SecretKey    Secret
	S3UseSSL       bool
	S3Prefix       string

	// Email notifications (optional): enabled when SMTPHost is set.
	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword Secret
	SMTPFrom     string
	// EmailDailyCap is the most mails one user can trigger per day (notifications and confirmation codes).
	EmailDailyCap int
	SMTPMode      string // starttls, tls or none

	// Web Push (optional): generate keys with `biblisann vapid`.
	VAPIDPublicKey  string
	VAPIDPrivateKey Secret
	VAPIDSubject    string
}

// Options select which values are mandatory for the command being run.
type Options struct {
	RequireDatabase bool
}

// Load reads and validates the configuration. All problems are reported together.
func Load(src Sources, opts Options) (Config, error) {
	r := newReader(src)
	r.loadConfigFile()

	c := Config{
		ListenAddr:           r.str("LISTEN_ADDR", ":8080"),
		BaseURL:              r.baseURL("BASE_URL", "http://localhost:8080"),
		TrustedProxies:       r.prefixes("TRUSTED_PROXIES"),
		LogLevel:             r.level("LOG_LEVEL", slog.LevelInfo),
		LogFormat:            r.oneOf("LOG_FORMAT", "json", "json", "text"),
		ShutdownTimeout:      r.duration("SHUTDOWN_TIMEOUT", 20*time.Second),
		MigrateOnStart:       r.boolean("MIGRATE_ON_START", true),
		Registration:         r.oneOf("REGISTRATION", "first-user", "first-user", "open", "closed"),
		CoversDir:            r.str("COVERS_DIR", "data/covers"),
		Providers:            r.list("PROVIDERS", []string{"bnf", "openlibrary"}),
		DisabledProviders:    r.list("DISABLED_PROVIDERS", nil),
		StaticProviderFile:   r.str("STATIC_PROVIDER_FILE", ""),
		AddonProvidersFile:   r.str("ADDON_PROVIDERS_FILE", ""),
		Passkeys:             r.boolean("PASSKEYS", true),
		GCSoftDeleteDays:     r.integer("GC_SOFT_DELETE_DAYS", 90),
		MetricsPublic:        r.boolean("METRICS_PUBLIC", false),
		SourceURL:            r.httpsURL("SOURCE_URL", "https://github.com/larueli/biblisann"),
		HardcoverRefreshDays: r.integer("HARDCOVER_REFRESH_DAYS", 3),
		HardcoverPerMinute:   r.integer("HARDCOVER_PER_MINUTE", 50),
		OIDCIssuer:           r.str("OIDC_ISSUER", ""),
		OIDCClientID:         r.str("OIDC_CLIENT_ID", ""),
		OIDCScopes:           r.list("OIDC_SCOPES", nil),
		OIDCGroupsClaim:      r.str("OIDC_GROUPS_CLAIM", "groups"),
		OIDCAdminGroup:       r.str("OIDC_ADMIN_GROUP", ""),
		OIDCAutoCreate:       r.boolean("OIDC_AUTO_CREATE", true),
		OIDCLabel:            r.str("OIDC_LABEL", ""),
		StorageBackend:       r.oneOf("STORAGE_BACKEND", "local", "local", "s3"),
		S3Endpoint:           r.str("S3_ENDPOINT", ""),
		S3Bucket:             r.str("S3_BUCKET", ""),
		S3Region:             r.str("S3_REGION", ""),
		S3AccessKey:          r.str("S3_ACCESS_KEY", ""),
		S3UseSSL:             r.boolean("S3_USE_SSL", true),
		S3Prefix:             r.str("S3_PREFIX", ""),
		SMTPHost:             r.str("SMTP_HOST", ""),
		SMTPPort:             r.integer("SMTP_PORT", 587),
		SMTPUsername:         r.str("SMTP_USERNAME", ""),
		SMTPFrom:             r.str("SMTP_FROM", ""),
		EmailDailyCap:        r.integer("EMAIL_DAILY_CAP", 20),
		SMTPMode:             r.oneOf("SMTP_MODE", "starttls", "starttls", "tls", "none"),
		VAPIDPublicKey:       r.str("VAPID_PUBLIC_KEY", ""),
		VAPIDSubject:         r.str("VAPID_SUBJECT", ""),
	}
	if v, ok := r.lookup("DATABASE_URL", true); ok {
		c.DatabaseURL = Secret(v)
	}
	if v, ok := r.lookup("SMTP_PASSWORD", true); ok {
		c.SMTPPassword = Secret(v)
	}
	if v, ok := r.lookup("OIDC_CLIENT_SECRET", true); ok {
		c.OIDCClientSecret = Secret(v)
	}
	if c.OIDCIssuer != "" && (c.OIDCClientID == "" || c.OIDCClientSecret == "") {
		r.fail("%sOIDC_ISSUER needs OIDC_CLIENT_ID and OIDC_CLIENT_SECRET (or OIDC_CLIENT_SECRET_FILE)", prefix)
	}
	if v, ok := r.lookup("METRICS_TOKEN", true); ok {
		c.MetricsToken = Secret(strings.TrimSpace(v))
	}
	if v, ok := r.lookup("HARDCOVER_TOKEN", true); ok {
		c.HardcoverToken = Secret(v)
	}
	if v, ok := r.lookup("VALKEY_URL", true); ok {
		c.ValkeyURL = Secret(v)
	}
	if v, ok := r.lookup("GOOGLE_BOOKS_KEY", true); ok {
		c.GoogleBooksKey = Secret(v)
	}
	if v, ok := r.lookup("SECRET_KEY", true); ok {
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(v))
		if err != nil || len(raw) != 32 {
			r.fail("%sSECRET_KEY: must be base64 of exactly 32 bytes (openssl rand -base64 32)", prefix)
		}
		c.SecretKey = Secret(v)
	}
	if v, ok := r.lookup("S3_SECRET_KEY", true); ok {
		c.S3SecretKey = Secret(v)
	}
	if c.StorageBackend == "s3" && (c.S3Endpoint == "" || c.S3Bucket == "" || c.S3AccessKey == "" || c.S3SecretKey == "") {
		r.fail("%sSTORAGE_BACKEND=s3 needs S3_ENDPOINT, S3_BUCKET, S3_ACCESS_KEY and S3_SECRET_KEY (or S3_SECRET_KEY_FILE)", prefix)
	}
	if v, ok := r.lookup("VAPID_PRIVATE_KEY", true); ok {
		c.VAPIDPrivateKey = Secret(v)
	}
	if c.SMTPHost != "" && c.SMTPFrom == "" {
		r.fail("%sSMTP_FROM is required when %sSMTP_HOST is set", prefix, prefix)
	}
	if (c.VAPIDPublicKey == "") != (c.VAPIDPrivateKey == "") {
		r.fail("%sVAPID_PUBLIC_KEY and %sVAPID_PRIVATE_KEY must be set together", prefix, prefix)
	}
	if opts.RequireDatabase && c.DatabaseURL == "" {
		r.fail("%sDATABASE_URL (or %sDATABASE_URL_FILE) is required", prefix, prefix)
	}
	if err := r.err(); err != nil {
		return Config{}, fmt.Errorf("invalid configuration: %w", err)
	}
	return c, nil
}

// LogValue summarizes the configuration for the startup log. Secrets stay redacted.
func (c Config) LogValue() slog.Value {
	proxies := make([]string, len(c.TrustedProxies))
	for i, p := range c.TrustedProxies {
		proxies[i] = p.String()
	}
	return slog.GroupValue(
		slog.String("listen_addr", c.ListenAddr),
		slog.String("base_url", c.BaseURL.String()),
		slog.Any("trusted_proxies", proxies),
		slog.Any("database_url", c.DatabaseURL),
		slog.String("log_level", c.LogLevel.String()),
		slog.String("log_format", c.LogFormat),
		slog.Duration("shutdown_timeout", c.ShutdownTimeout),
		slog.Bool("migrate_on_start", c.MigrateOnStart),
		slog.String("registration", c.Registration),
		slog.String("covers_dir", c.CoversDir),
		slog.Any("providers", c.Providers),
	)
}

func (r *reader) str(name, def string) string {
	if v, ok := r.lookup(name, false); ok {
		return strings.TrimSpace(v)
	}
	return def
}

func (r *reader) oneOf(name, def string, allowed ...string) string {
	v := strings.ToLower(r.str(name, def))
	for _, a := range allowed {
		if v == a {
			return v
		}
	}
	r.fail("%s%s: must be one of %s", prefix, name, strings.Join(allowed, ", "))
	return def
}

func (r *reader) boolean(name string, def bool) bool {
	v, ok := r.lookup(name, false)
	if !ok {
		return def
	}
	b, err := strconv.ParseBool(strings.TrimSpace(v))
	if err != nil {
		r.fail("%s%s: must be true or false", prefix, name)
		return def
	}
	return b
}

func (r *reader) duration(name string, def time.Duration) time.Duration {
	v, ok := r.lookup(name, false)
	if !ok {
		return def
	}
	d, err := time.ParseDuration(strings.TrimSpace(v))
	if err != nil || d <= 0 {
		r.fail("%s%s: must be a positive duration such as 30s", prefix, name)
		return def
	}
	return d
}

func (r *reader) level(name string, def slog.Level) slog.Level {
	v, ok := r.lookup(name, false)
	if !ok {
		return def
	}
	var l slog.Level
	if err := l.UnmarshalText([]byte(strings.TrimSpace(v))); err != nil {
		r.fail("%s%s: must be debug, info, warn or error", prefix, name)
		return def
	}
	return l
}

// baseURL accepts an origin only (scheme, host, optional port). Sub-path deployment is not
// supported: cookies, OAuth redirects and passkeys all assume the origin is the root.
func (r *reader) baseURL(name, def string) *url.URL {
	raw := r.str(name, def)
	u, err := url.Parse(raw)
	switch {
	case err != nil, u.Host == "", u.Hostname() == "":
		r.fail("%s%s: must be an absolute URL such as https://books.example.org", prefix, name)
	case u.Scheme != "http" && u.Scheme != "https":
		r.fail("%s%s: scheme must be http or https", prefix, name)
	case u.User != nil, u.RawQuery != "", u.Fragment != "", (u.Path != "" && u.Path != "/"):
		r.fail("%s%s: must be an origin only (no credentials, path, query or fragment)", prefix, name)
	default:
		return &url.URL{Scheme: u.Scheme, Host: u.Host}
	}
	d, _ := url.Parse(def)
	return d
}

// prefixes parses a comma-separated list of CIDRs or single addresses.
func (r *reader) prefixes(name string) []netip.Prefix {
	v, ok := r.lookup(name, false)
	if !ok || strings.TrimSpace(v) == "" {
		return nil
	}
	var out []netip.Prefix
	for _, item := range strings.Split(v, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if p, err := netip.ParsePrefix(item); err == nil {
			out = append(out, p.Masked())
			continue
		}
		a, err := netip.ParseAddr(item)
		if err != nil {
			r.fail("%s%s: %q is not a CIDR or an IP address", prefix, name, item)
			continue
		}
		out = append(out, netip.PrefixFrom(a, a.BitLen()))
	}
	return out
}

// list parses a comma-separated list.
func (r *reader) list(name string, def []string) []string {
	v, ok := r.lookup(name, false)
	if !ok {
		return def
	}
	var out []string
	for _, item := range strings.Split(v, ",") {
		if item = strings.ToLower(strings.TrimSpace(item)); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func (r *reader) integer(name string, def int) int {
	v, ok := r.lookup(name, false)
	if !ok {
		return def
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n <= 0 || n > 65535 {
		r.fail("%s%s: must be a number between 1 and 65535", prefix, name)
		return def
	}
	return n
}

// httpsURL parses an absolute http(s) URL without credentials.
func (r *reader) httpsURL(name, def string) string {
	v := r.str(name, def)
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
		r.fail("%s%s: must be an absolute http(s) URL", prefix, name)
		return def
	}
	return strings.TrimSuffix(u.String(), "/")
}
