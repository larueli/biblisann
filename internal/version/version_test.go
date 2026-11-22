package version

import "testing"

func TestVersion(t *testing.T) {
	tests := []struct {
		name string
		set  string
		want string
	}{
		{"injected with v", "v1.2.3", "1.2.3"},
		{"injected without v", "0.1.0", "0.1.0"},
		{"prerelease", "v1.0.0-rc.1", "1.0.0-rc.1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			old := version
			t.Cleanup(func() { version = old })
			version = tt.set
			if got := Version(); got != tt.want {
				t.Errorf("Version() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestVersionFallback(t *testing.T) {
	old := version
	t.Cleanup(func() { version = old })
	version = ""
	if Version() == "" {
		t.Error("fallback version must not be empty")
	}
}
