package config

import "log/slog"

// Secret is a sensitive string that never prints itself.
type Secret string

func (Secret) redacted() string { return "[redacted]" }

// Reveal returns the secret value. Call it only where the value is used.
func (s Secret) Reveal() string { return string(s) }

// String implements fmt.Stringer and hides the value.
func (s Secret) String() string { return s.redacted() }

// GoString hides the value in %#v output.
func (s Secret) GoString() string { return s.redacted() }

// LogValue implements slog.LogValuer and hides the value in logs.
func (s Secret) LogValue() slog.Value { return slog.StringValue(s.redacted()) }

// MarshalText hides the value in JSON and text encoders.
func (s Secret) MarshalText() ([]byte, error) { return []byte(s.redacted()), nil }
