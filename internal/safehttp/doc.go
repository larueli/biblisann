// Package safehttp is the only way Biblisann makes outbound HTTP requests.
//
// It protects against server-side request forgery (SSRF): requests to loopback, private,
// link-local, multicast and other non-public addresses are refused. The check runs when the
// connection is made, on the resolved IP, so DNS rebinding and redirects to internal hosts
// cannot bypass it. Responses are size-limited and every request has a deadline.
package safehttp
