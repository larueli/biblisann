# 0027. Security audit follow-ups

Date: 2026-11-25
Status: Accepted

## Context

The security audit found that unverified local addresses could be linked to OIDC identities or
receive shares, and that import, backup, offline, and token-revocation paths needed stronger
isolation and recovery guarantees. ADR 0026 described sharing as administrator-only, but the
owner clarified that `library.share` remains a grantable custom permission.

## Decision

- Keep `library.share` as a custom permission, subject to its existing restrictions on elevated
  grants. Administrator is a preset containing that permission.
- Require verified address ownership for new email-based shares. Existing grants remain in place.
  Link an existing account to an OIDC identity explicitly after recent authentication, and bind
  the link to the initiating session. Do not link accounts by matching email during normal login.
- Preserve authentication factors and identities in logical backups. Reject version 1 logical
  backups, which did not include factors, and require the original secret key when restoring TOTP.
- Refresh a shared edition only through providers permitted by every library holding it. Record a
  skipped refresh when the intersection is empty.
- Scope offline data by account and verify the current session before replay. Leave old unscoped
  offline data on the device without automatically replaying it.
- Serialize OAuth rotation and revocation; invalidate pending authorization codes when revoking.
  Reclaim old unused dynamic client registrations under capacity pressure.
- Require a CSRF-protected request and recent authentication for an administrator to download a
  full backup. Import database changes commit atomically; cover files are added after commit as a
  best-effort step because a filesystem or S3 is not part of the database transaction.
- Replace `BIBLISANN_COVERS_DIR` with `BIBLISANN_DATA_DIR`, storing local covers in its `covers`
  subdirectory. Refuse the obsolete setting so upgrades cannot silently use an empty directory.
- Limit server spans to bounded method and status attributes and omit request paths and client
  addresses from access logs and traces.

## Consequences

Existing accounts need address verification before new email-based sharing. Operators must review
existing OIDC links and grants; these changes cannot establish whether older links were legitimate.
Legacy logical backups require a new export from the original instance or database-level recovery.
Existing unscoped pending scans are not automatically imported into an account-scoped outbox.
