package safehttp

import (
	"errors"
	"net/netip"
)

// ErrBlockedAddress is returned when a request targets a non-public address.
var ErrBlockedAddress = errors.New("safehttp: destination address is not allowed")

// blockedPrefixes lists ranges that netip's helpers do not already classify as non-global.
var blockedPrefixes = mustPrefixes(
	"0.0.0.0/8",       // "this network"
	"100.64.0.0/10",   // carrier-grade NAT
	"192.0.0.0/24",    // IETF protocol assignments
	"192.0.2.0/24",    // documentation
	"198.18.0.0/15",   // benchmarking
	"198.51.100.0/24", // documentation
	"203.0.113.0/24",  // documentation
	"240.0.0.0/4",     // reserved, includes broadcast
	"64:ff9b::/96",    // NAT64: can reach IPv4 internals
	"64:ff9b:1::/48",  // local-use NAT64
	"100::/64",        // discard-only
	"2001:db8::/32",   // documentation
	"2002::/16",       // 6to4: embeds an IPv4 address
	"fec0::/10",       // deprecated site-local
)

func mustPrefixes(ss ...string) []netip.Prefix {
	out := make([]netip.Prefix, len(ss))
	for i, s := range ss {
		out[i] = netip.MustParsePrefix(s)
	}
	return out
}

// isPublic reports whether addr is a globally routable unicast address.
func isPublic(addr netip.Addr) bool {
	addr = addr.Unmap() // ::ffff:10.0.0.1 must be judged as 10.0.0.1
	if !addr.IsValid() || !addr.IsGlobalUnicast() {
		return false
	}
	if addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() ||
		addr.IsMulticast() || addr.IsUnspecified() {
		return false
	}
	for _, p := range blockedPrefixes {
		if p.Contains(addr) {
			return false
		}
	}
	return true
}
