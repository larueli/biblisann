# Sign-in and security

## Ways to sign in

- **Password** (Argon2id, 10 characters minimum).
- **Two-factor**: Settings > Two-factor authentication shows a QR code for any authenticator app, then gives ten one-time recovery codes. Needs `BIBLISANN_SECRET_KEY` on the server.
- **Passkeys**: Settings > Passkeys. A passkey replaces password and second factor. Disable with `BIBLISANN_PASSKEYS=false`.
- **Single sign-on (OIDC)**: set the `BIBLISANN_OIDC_*` variables. An identity is attached to an existing account only if the provider says the email is verified. Groups in the token become groups in Biblisann; `BIBLISANN_OIDC_ADMIN_GROUP` makes its members administrators.
- **Forgot password**: needs SMTP. The link works once for one hour and signs you out everywhere.

## Provisioning with SCIM

An administrator creates a provisioning token through the API (`POST /v1/me/tokens` with `kind: scim`) and gives it, with `<BASE_URL>/scim/v2`, to the identity provider. Users are created without a password (they sign in through OIDC); "delete" in the provider deactivates the account.

## AI assistants (MCP) with OAuth

MCP clients that support OAuth find the server by themselves (`/.well-known/oauth-protected-resource`), register, and send you to an authorization screen where you choose read-only or read-write and optionally limit the libraries. The resulting token works on `/mcp` only. Personal access tokens (Settings) remain available for scripts.

## OPDS

A calendar/catalog feed token (Settings > Access tokens > *Create calendar feed*) also opens `/opds/<token>/`, a read-only OPDS catalog of your libraries for catalog apps. It lists metadata and covers; Biblisann does not host book files.

## Metadata providers

Default: BnF and Open Library. Also available by name: `google`, `loc`, `k10plus`, `static` and your own addons (see `docs/dev/addons.md`). Each lookup sends the ISBN to the providers in use.
