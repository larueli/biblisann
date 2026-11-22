# 0004. Own OAuth authorization server, MCP as resource server, OIDC federation

Date: 2026-11-22
Status: Accepted

## Context

The MCP authorization specification requires an OAuth-based flow. Users may have no external identity provider (local passwords), and the app must also federate OIDC providers. A bare-metal install cannot depend on Keycloak, Hydra or similar.

## Decision

- The app embeds an OAuth 2.1 authorization server (authorization code + PKCE, refresh token rotation, resource indicators, metadata endpoints) built on a well-known certified library. It does not hand-roll protocol logic.
- The MCP endpoint is an OAuth resource server (protected resource metadata). Scopes: read, write, restrictable per library. Follow the current MCP authorization spec revision at implementation time and record the revision in the code.
- OIDC login federates external providers into the same user model. Account linking only on a verified email from a configured issuer.
- Until Slice 3, MCP accepts scoped personal access tokens only.

## Consequences

- This is the riskiest component. It gets the security reviewer on every change and its own threat model in `docs/dev/`.
- Tokens are opaque and stored hashed; web uses same-origin cookies, not bearer tokens.
