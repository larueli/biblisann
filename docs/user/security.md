# Sign-in and security

## Ways to sign in

- **Password** (Argon2id, 10 characters minimum).
- **Two-factor**: Settings > Two-factor authentication shows a QR code for any authenticator app, then gives ten one-time recovery codes. Needs `BIBLISANN_SECRET_KEY` on the server.
- **Passkeys**: Settings > Passkeys. A passkey replaces password and second factor. Disable with `BIBLISANN_PASSKEYS=false`.
- **Single sign-on (OIDC)**: set the `BIBLISANN_OIDC_*` variables. To link an existing account, sign in to it, confirm your identity, and use the linking action in Settings. The provider must verify the same email address. Groups in the token become groups in Biblisann; `BIBLISANN_OIDC_ADMIN_GROUP` makes its members administrators.
- **Forgot password**: needs SMTP. The link works once for one hour and signs you out everywhere.

## Confirming sensitive actions (step-up)

Changing the password, turning two-factor on or off, adding or removing a passkey, creating a token, downloading a full backup and deleting the account ask you to prove who you are again, even with a valid session. You can confirm with your password (plus an authenticator or recovery code when two-factor is on), with a passkey, or by signing in again at your single sign-on provider. With single sign-on the provider is asked to sign you in again. When it reports when you signed in (`auth_time`, which OpenID Connect requires for this request) the confirmation lasts 5 minutes; when it does not, each sensitive action needs its own sign-in at the provider. The confirmation unlocks these actions for 5 minutes, for that browser session only.

API clients see `403` with the detail `step_up_required`; the answer to `GET /v1/auth/session` carries `step_up_until` while the window is open. Personal access tokens cannot perform these actions at all.

### Known limits

- Single sign-on providers that send no `auth_time` and ignore `prompt=login` give a weaker confirmation: it proves a sign-in flow just happened, not that the provider asked you again. Test your provider.
- Library administrators who are not instance administrators get the same answer for known and unknown email addresses when sharing, but a few answers (the person already has elevated access, the last administrator) and the member list still reveal that an account exists.
- Everyone who may record loans in a library sees the borrower's name, the contact name and the notes of its loans.

## Provisioning with SCIM

An administrator creates a provisioning token in Settings (*Create provisioning token (SCIM)*, shown once) and gives it, with `<BASE_URL>/scim/v2`, to the identity provider. Users are created without a password (they sign in through OIDC); "delete" in the provider deactivates the account.

New SCIM-created accounts without credentials or sessions can link automatically on their first
sign-in with a matching, verified provider email. Accounts provisioned before this protection was
added cannot be identified safely as such; an administrator must help their owner regain local
access and complete explicit linking. Existing OIDC identities remain linked: audit existing links,
credentials and shares for possible prior account pre-hijacking.

New email-based shares and group additions require a verified account address. An existing user
can verify it by redeeming an invitation sent to that address or by explicitly linking a matching
verified OIDC identity. Existing shares are retained and should be reviewed by administrators.

## AI assistants (MCP) with OAuth

MCP clients that support OAuth find the server by themselves (`/.well-known/oauth-protected-resource`), register, and send you to an authorization screen where you choose read-only or read-write and optionally limit the libraries. The resulting token works on `/mcp` only. Refresh tokens work once; if one is ever presented twice, the whole authorization is revoked and the client has to ask you again. Personal access tokens (Settings) remain available for scripts.

## OPDS

A calendar/catalog feed token (Settings > Access tokens > *Create calendar feed*) also opens `/opds/<token>/`, a read-only OPDS catalog of your libraries for catalog apps. It lists metadata and covers; Biblisann does not host book files.

## Metadata providers

Default: BnF and Open Library. Also available by name: `google`, `loc`, `k10plus`, `static` and your own addons (see `docs/dev/addons.md`). Each lookup sends the ISBN to the providers in use.

Outbound HTTPS requests cannot redirect to HTTP. Requests carrying credentials or a body cannot
redirect to a different origin; configure the final service URL when an integration needs one.

## Single sign-on and two-factor

When you sign in through OIDC, your identity provider is the authentication factor: Biblisann does not ask for its own authenticator code. Enforce multi-factor authentication at the provider.

## The secret key (`BIBLISANN_SECRET_KEY`)

The key seals authenticator secrets and Hardcover tokens in the database. Treat it like the database password: keep it with your other secrets and back it up **separately** from database backups (a backup does not contain it).

Biblisann checks it at every start and **refuses to start** when:

- it is not set but users have two-factor authentication or a Hardcover link (starting anyway would let accounts sign in without their second factor);
- it does not open the data sealed earlier (a different key than before).

The error names the problem and never prints the key. The fix is to start with the original key. If the key is truly lost, the sealed data cannot be recovered: delete it on purpose (`DELETE FROM user_totp;` and `DELETE FROM hardcover_links;` in PostgreSQL, then affected users enrol again), and only then start without or with a new key. While the server is running with two-factor users but no key, signing in is refused for them rather than skipping the second factor.
