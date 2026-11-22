// Package api builds the HTTP surface: health probes, the versioned REST API described by an
// OpenAPI 3.1 document generated from code, and the embedded web interface.
//
// Every API operation is registered through Register, which forces the author to declare the
// permission the operation needs (exposed as the x-permission extension in the spec). Errors
// are RFC 9457 problem details.
package api
