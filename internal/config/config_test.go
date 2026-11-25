package config

import (
	"io/fs"
	"log/slog"
	"net/url"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) Sources {
	return Sources{
		LookupEnv: func(k string) (string, bool) { v, ok := m[k]; return v, ok },
		ReadFile:  func(p string) ([]byte, error) { return nil, &fs.PathError{Op: "open", Path: p, Err: fs.ErrNotExist} },
	}
}

func withFiles(m map[string]string, files map[string]string) Sources {
	s := env(m)
	s.ReadFile = func(p string) ([]byte, error) {
		if c, ok := files[p]; ok {
			return []byte(c), nil
		}
		return nil, &fs.PathError{Op: "open", Path: p, Err: fs.ErrNotExist}
	}
	return s
}

func TestDefaults(t *testing.T) {
	c, err := Load(env(nil), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if c.ListenAddr != ":8080" || c.BaseURL.String() != "http://localhost:8080" ||
		c.LogLevel != slog.LevelInfo || c.LogFormat != "json" ||
		c.ShutdownTimeout != 20*time.Second || !c.MigrateOnStart || c.DataDir != "data" {
		t.Errorf("unexpected defaults: %+v", c)
	}
}

func TestDatabaseRequired(t *testing.T) {
	if _, err := Load(env(nil), Options{RequireDatabase: true}); err == nil {
		t.Fatal("expected an error when DATABASE_URL is missing")
	}
	c, err := Load(env(map[string]string{"BIBLISANN_DATABASE_URL": "postgres://u:pw@h/db"}), Options{RequireDatabase: true})
	if err != nil {
		t.Fatal(err)
	}
	if c.DatabaseURL.Reveal() != "postgres://u:pw@h/db" {
		t.Error("database url not loaded")
	}
}

func TestSecretFromFile(t *testing.T) {
	s := withFiles(map[string]string{"BIBLISANN_DATABASE_URL_FILE": "/run/secrets/db"},
		map[string]string{"/run/secrets/db": "postgres://u:pw@h/db\n"})
	c, err := Load(s, Options{RequireDatabase: true})
	if err != nil {
		t.Fatal(err)
	}
	if c.DatabaseURL.Reveal() != "postgres://u:pw@h/db" {
		t.Errorf("trailing newline must be trimmed, got %q", c.DatabaseURL.Reveal())
	}
}

func TestConflicts(t *testing.T) {
	tests := []struct {
		name  string
		env   map[string]string
		files map[string]string
		want  string
	}{
		{"var and _FILE", map[string]string{
			"BIBLISANN_DATABASE_URL": "x", "BIBLISANN_DATABASE_URL_FILE": "/s",
		}, map[string]string{"/s": "y"}, "set more than once"},
		{"env and config file", map[string]string{
			"BIBLISANN_LISTEN_ADDR": ":1", "BIBLISANN_CONFIG_FILE": "/c",
		}, map[string]string{"/c": "BIBLISANN_LISTEN_ADDR=:2\n"}, "set more than once"},
		{"_FILE on non sensitive value is ignored, not a conflict", nil, nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(withFiles(tt.env, tt.files), Options{})
			if tt.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestConfigFile(t *testing.T) {
	file := "# comment\n\nBIBLISANN_LISTEN_ADDR = \"127.0.0.1:9000\"\nBIBLISANN_LOG_FORMAT=text\n"
	c, err := Load(withFiles(map[string]string{"BIBLISANN_CONFIG_FILE": "/c"}, map[string]string{"/c": file}), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if c.ListenAddr != "127.0.0.1:9000" || c.LogFormat != "text" {
		t.Errorf("config file not applied: %+v", c)
	}
}

func TestInvalidValues(t *testing.T) {
	tests := []struct{ name, key, val string }{
		{"bad url", "BIBLISANN_BASE_URL", "not a url"},
		{"bad scheme", "BIBLISANN_BASE_URL", "ftp://example.org"},
		{"path in url", "BIBLISANN_BASE_URL", "https://example.org/app"},
		{"credentials in url", "BIBLISANN_BASE_URL", "https://u:p@example.org"},
		{"bad level", "BIBLISANN_LOG_LEVEL", "loud"},
		{"bad format", "BIBLISANN_LOG_FORMAT", "xml"},
		{"bad duration", "BIBLISANN_SHUTDOWN_TIMEOUT", "soon"},
		{"negative duration", "BIBLISANN_SHUTDOWN_TIMEOUT", "-1s"},
		{"bad bool", "BIBLISANN_MIGRATE_ON_START", "maybe"},
		{"bad proxy", "BIBLISANN_TRUSTED_PROXIES", "10.0.0.0/8, nope"},
		{"empty data directory", "BIBLISANN_DATA_DIR", ""},
		{"old cover directory", "BIBLISANN_COVERS_DIR", "/path/to/covers"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(env(map[string]string{tt.key: tt.val}), Options{})
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tt.key) {
				t.Errorf("error should name %s: %v", tt.key, err)
			}
		})
	}
}

func TestTrustedProxies(t *testing.T) {
	c, err := Load(env(map[string]string{"BIBLISANN_TRUSTED_PROXIES": "10.1.2.3/8, 192.168.0.1 ,::1"}), Options{})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range c.TrustedProxies {
		got = append(got, p.String())
	}
	if want := "10.0.0.0/8 192.168.0.1/32 ::1/128"; strings.Join(got, " ") != want {
		t.Errorf("got %v, want %s", got, want)
	}
}

func TestErrorsNeverLeakSecrets(t *testing.T) {
	files := map[string]string{"/c": "this line has a secret=hunter2 but no prefix\n"}
	_, err := Load(withFiles(map[string]string{"BIBLISANN_CONFIG_FILE": "/c"}, files), Options{})
	if err == nil || strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("expected an error without the secret, got %v", err)
	}
}

func TestSecretIsRedacted(t *testing.T) {
	c := Config{DatabaseURL: "postgres://u:pw@h/db", BaseURL: mustURL(t)}
	var sb strings.Builder
	slog.New(slog.NewJSONHandler(&sb, nil)).Info("cfg", "config", c)
	if strings.Contains(sb.String(), "pw@h") {
		t.Errorf("secret leaked in log: %s", sb.String())
	}
	if strings.Contains(c.DatabaseURL.String(), "pw") {
		t.Error("String must redact")
	}
}

func mustURL(t *testing.T) *url.URL {
	t.Helper()
	c, err := Load(env(nil), Options{})
	if err != nil {
		t.Fatal(err)
	}
	return c.BaseURL
}

func TestDefaultsOfLaterSettings(t *testing.T) {
	c, err := Load(env(nil), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if c.GCSoftDeleteDays != 90 || c.SourceURL != "https://github.com/larueli/biblisann" || c.MetricsPublic || c.MetricsToken != "" ||
		c.HardcoverRefreshDays != 3 || c.HardcoverPerMinute != 50 || !c.Passkeys {
		t.Errorf("defaults: %+v", c)
	}
	c, err = Load(env(map[string]string{"BIBLISANN_SOURCE_URL": "https://git.example.org/me/fork/", "BIBLISANN_METRICS_PUBLIC": "true"}), Options{})
	if err != nil || c.SourceURL != "https://git.example.org/me/fork" || !c.MetricsPublic {
		t.Errorf("overrides: %+v %v", c, err)
	}
	for _, bad := range []string{"ftp://x.example", "not a url", "https://u:p@x.example"} {
		if _, err := Load(env(map[string]string{"BIBLISANN_SOURCE_URL": bad}), Options{}); err == nil {
			t.Errorf("SOURCE_URL %q accepted", bad)
		}
	}
}
