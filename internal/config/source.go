package config

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	prefix       = "BIBLISANN_"
	fileSuffix   = "_FILE"
	configFile   = "CONFIG_FILE"
	maxFileBytes = 1 << 20
)

// Sources are the inputs of Load. The zero value reads the real process environment and disk.
type Sources struct {
	// LookupEnv reads one environment variable. Defaults to os.LookupEnv.
	LookupEnv func(string) (string, bool)
	// ReadFile reads a file, limited to a small size. Defaults to a bounded os.ReadFile.
	ReadFile func(path string) ([]byte, error)
}

func (s Sources) withDefaults() Sources {
	if s.LookupEnv == nil {
		s.LookupEnv = os.LookupEnv
	}
	if s.ReadFile == nil {
		s.ReadFile = readBounded
	}
	return s
}

func readBounded(path string) ([]byte, error) {
	f, err := os.Open(path) //nolint:gosec // the path is operator-provided configuration
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxFileBytes {
		return nil, errors.New("file is too large")
	}
	return data, nil
}

// reader resolves named values across the three sources and remembers every problem.
type reader struct {
	src  Sources
	file map[string]string // values from the config file, keyed by full variable name
	errs []error
}

func newReader(src Sources) *reader {
	return &reader{src: src.withDefaults(), file: map[string]string{}}
}

func (r *reader) fail(format string, args ...any) {
	r.errs = append(r.errs, fmt.Errorf(format, args...))
}

// loadConfigFile reads BIBLISANN_CONFIG_FILE, when set, into r.file.
func (r *reader) loadConfigFile() {
	path, ok := r.src.LookupEnv(prefix + configFile)
	if !ok || path == "" {
		return
	}
	data, err := r.src.ReadFile(path)
	if err != nil {
		r.fail("%s%s: cannot read config file: %w", prefix, configFile, err)
		return
	}
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, found := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !found || !strings.HasPrefix(key, prefix) {
			// The line itself is not echoed: it may hold a secret.
			r.fail("config file line %d: expected %s<NAME>=<value>", n, prefix)
			continue
		}
		if _, dup := r.file[key]; dup {
			r.fail("config file line %d: %s is set twice", n, key)
			continue
		}
		r.file[key] = unquote(strings.TrimSpace(val))
	}
}

func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	return v
}

// lookup returns the value of BIBLISANN_<name>. When sensitive is true the _FILE form is also
// honored. It reports (value, true) when exactly one source provides it.
func (r *reader) lookup(name string, sensitive bool) (string, bool) {
	full := prefix + name

	type candidate struct {
		origin string
		value  string
		isFile bool
	}
	var found []candidate
	if v, ok := r.src.LookupEnv(full); ok {
		found = append(found, candidate{"environment variable " + full, v, false})
	}
	if v, ok := r.file[full]; ok {
		found = append(found, candidate{"config file key " + full, v, false})
	}
	if sensitive {
		if v, ok := r.src.LookupEnv(full + fileSuffix); ok {
			found = append(found, candidate{"environment variable " + full + fileSuffix, v, true})
		}
		if v, ok := r.file[full+fileSuffix]; ok {
			found = append(found, candidate{"config file key " + full + fileSuffix, v, true})
		}
	}
	switch len(found) {
	case 0:
		return "", false
	case 1:
	default:
		names := make([]string, len(found))
		for i, c := range found {
			names[i] = c.origin
		}
		r.fail("%s is set more than once (%s): keep one", full, strings.Join(names, ", "))
		return "", false
	}
	c := found[0]
	if !c.isFile {
		return c.value, true
	}
	data, err := r.src.ReadFile(c.value)
	if err != nil {
		r.fail("%s%s: cannot read secret file: %w", full, fileSuffix, err)
		return "", false
	}
	return strings.TrimRight(string(data), "\r\n"), true
}

func (r *reader) err() error { return errors.Join(r.errs...) }
