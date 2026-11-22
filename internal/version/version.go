// Package version exposes the build version, injected at link time.
package version

import "runtime/debug"

// version is set with -ldflags "-X github.com/larueli/biblisann/internal/version.version=1.2.3".
var version = ""

// Version returns the release version without the leading "v". Outside a release build it
// falls back to the VCS revision recorded by the Go toolchain, then to "dev".
func Version() string {
	if version != "" {
		return trimV(version)
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" && len(s.Value) >= 7 {
				return "dev+" + s.Value[:7]
			}
		}
	}
	return "dev"
}

func trimV(v string) string {
	if len(v) > 1 && v[0] == 'v' {
		return v[1:]
	}
	return v
}
