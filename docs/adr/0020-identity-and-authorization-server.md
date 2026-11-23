# 0020. Identity: OIDC, second factor, passkeys, SCIM and the OAuth server

Date: 2026-11-23
Status: Accepted

## Decision

- **OIDC** (`coreos/go-oidc`, authorization code + PKCE S256 + nonce). Discovery and token calls use the safehttp client, lazily, so an IdP outage does not stop the server. The authorization request state is stored in PostgreSQL (single use, 10 minutes) and also bound to the browser by a cookie, against login CSRF.
- **Account linking** only on an ID token with `email_verified=true` from the configured issuer. An existing `(issuer, subject)` identity signs in directly; otherwise the verified email links to the account with that address, or creates one when `BIBLISANN_OIDC_AUTO_CREATE` is on. Users created this way have no password.
- **Group sync**: groups in the configured claim are mirrored into externally managed groups (`external_id` set) and removed on the next sign-in when the user leaves them. `BIBLISANN_OIDC_ADMIN_GROUP` grants instance administration, re-evaluated at every sign-in.
- **TOTP** (RFC 6238, implemented on the standard library) with 10 single-use recovery codes. The secret is sealed with AES-256-GCM under `BIBLISANN_SECRET_KEY`; without that key two-factor is off. A code works once (time step must advance), a challenge dies after five wrong attempts. **Password reset** by email (one-hour single-use token, hashed at rest) is available when SMTP is configured; it always answers 204, and ends all sessions.
- **Passkeys** (`go-webauthn`): discoverable credentials, user verification required, so a passkey replaces password and second factor. Ceremonies are single use and stored in PostgreSQL. Disable with `BIBLISANN_PASSKEYS=false` (for example with OIDC-only sign-in). The relying-party ID is the host of `BIBLISANN_BASE_URL`.
- **SCIM 2.0** Users and Groups under `/scim/v2` with a dedicated bearer token only an instance administrator can create. Deleting a user deactivates the account (and ends its sessions) instead of erasing it, so library ownership is never lost by a provisioning mistake. Only `userName eq` / `displayName eq` filters.
- **OAuth 2.1 authorization server** for MCP: authorization code with mandatory PKCE (S256), dynamic client registration (RFC 7591) for public clients with strict redirect URI rules, rotating single-use refresh tokens, resource indicators (RFC 8707), metadata documents (RFC 8414, RFC 9728). Consent is a web screen where the user chooses read-only or read-write (read-write by default) and may restrict the grant to some libraries. Access tokens are opaque, hashed at rest, valid one hour, and bound to the MCP resource: the REST API refuses them.

## Consequences

- Passkey and OIDC sign-in bypass the TOTP step by design (both are already strong factors).
- Refresh-token theft detection is limited to rotation (a replayed old token fails); a full token-family revocation is not implemented.
