package catalog

import "slices"

// LookupSettings configure metadata lookup. Layers are merged most specific first (ADR 0013):
// request, library, user, instance default. A nil Providers or Languages means "not set here".
type LookupSettings struct {
	Providers []string `json:"providers,omitempty"` // ordered; first provider wins a field
	Languages []string `json:"languages,omitempty"` // preferred languages, ISO 639-1
}

// ResolveSettings returns the first set value of each field from layers (most specific first),
// then removes providers disabled by the administrator and unknown ones. Lower layers can never
// re-enable a provider the administrator disabled.
func ResolveSettings(known []string, disabled []string, layers ...*LookupSettings) LookupSettings {
	var out LookupSettings
	for _, l := range layers {
		if l == nil {
			continue
		}
		if out.Providers == nil && l.Providers != nil {
			out.Providers = slices.Clone(l.Providers)
		}
		if out.Languages == nil && l.Languages != nil {
			out.Languages = slices.Clone(l.Languages)
		}
	}
	out.Providers = slices.DeleteFunc(out.Providers, func(p string) bool {
		return !slices.Contains(known, p) || slices.Contains(disabled, p)
	})
	out.Providers = slices.Compact(out.Providers)
	return out
}
