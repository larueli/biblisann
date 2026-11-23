package config

import (
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
		ListenAddr:         r.str("LISTEN_ADDR", ":8080"),
		BaseURL:            r.baseURL("BASE_URL", "http://localhost:8080"),
		TrustedProxies:     r.prefixes("TRUSTED_PROXIES"),
		LogLevel:           r.level("LOG_LEVEL", slog.LevelInfo),
		LogFormat:          r.oneOf("LOG_FORMAT", "json", "json", "text"),
		ShutdownTimeout:    r.duration("SHUTDOWN_TIMEOUT", 20*time.Second),
		MigrateOnStart:     r.boolean("MIGRATE_ON_START", true),
		Registration:       r.oneOf("REGISTRATION", "first-user", "first-user", "open", "closed"),
		CoversDir:          r.str("COVERS_DIR", "data/covers"),
		Providers:          r.list("PROVIDERS", []string{"bnf", "openlibrary"}),
		DisabledProviders:  r.list("DISABLED_PROVIDERS", nil),
		StaticProviderFile: r.str("STATIC_PROVIDER_FILE", ""),
	}
	if v, ok := r.lookup("DATABASE_URL", true); ok {
		c.DatabaseURL = Secret(v)
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
