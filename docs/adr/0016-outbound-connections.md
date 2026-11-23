# 0016. Which outbound connections go through safehttp

Date: 2026-11-23
Status: Accepted

## Context

`AGENTS.md` requires all outbound HTTP to use `internal/safehttp` (no private, loopback or link-local targets). Some integrations talk to infrastructure the operator chose, which is often on a private network.

## Decision

- **User- or data-controlled destinations always use safehttp**: metadata providers, cover URLs, webhook and ntfy targets, Web Push endpoints (through `safehttp.Client.Std`), OIDC discovery and addon providers.
- **Operator-configured infrastructure does not**: the PostgreSQL URL, the SMTP relay, the S3 endpoint. They come from the process configuration, which only the operator controls, and they usually have private addresses (a MinIO service, an in-cluster relay). No request can make the server connect to them with attacker-chosen host, path or headers.
- SMTP credentials are only sent over TLS (`starttls` default, or `tls`); `none` is for a trusted local relay and is explicit.

## Consequences

- A user who registers a webhook on a private address gets a delivery error, by design.
- Adding a new integration requires deciding which side of this line it is on.
