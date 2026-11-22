# 0001. Backend in Go

Date: 2026-11-22
Status: Accepted

## Context

Priorities favor security, readability, low memory and CPU, single-artifact deployment and high availability. The maintainer is a DevOps/SRE engineer who knows Python and PHP and wants to learn a compiled language. Rust and Go were compared.

## Decision

Go, with the standard library first (`net/http` ServeMux, `log/slog`, `database/sql` types via `pgx`). The built frontend is embedded in the binary.

## Consequences

- One static binary, small images, fast startup, easy Kubernetes probes.
- Mature libraries exist for every hard part: OIDC/OAuth, WebAuthn, SCIM, the official MCP SDK, Postgres tooling.
- Rust would give a stronger type system and lower memory but slower iteration and thinner OAuth/SCIM libraries. Revisit only for a specific hot path.
- MARC parsing is the weakest spot in Go; keep parsers isolated per provider.
