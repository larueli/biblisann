# 0024. Administration screens, browser push, shelf view and token families

Date: 2026-11-23
Status: Accepted

## Decision

- **Administration in the web app**: library administrators share a library with users and groups, pick built-in roles or custom permission profiles, and manage the profiles (Settings > Sharing my libraries). Instance administrators manage user groups and their members, and create the SCIM provisioning token (Settings). Everything goes through the existing, permission-checked API.
- **Web Push in the browser**: the PWA now uses a custom service worker (`web/src/sw.ts`, vite-plugin-pwa `injectManifest`) that precaches the app shell, keeps server routes out of the navigation fallback, shows push notifications and opens the loans page on click. *Enable notifications on this device* asks permission, subscribes with the server's VAPID public key (`webpush_public_key` in the channel list) and registers the subscription as a channel. A subscription the push service rejects is left to the user to remove.
- **Shelf view**: a place is a shelf, the places inside it are its layers. Books are drawn as spines (thickness from the page count, colour from the title, height from the ID: deterministic) on boards; a book moves by drag and drop or from a select. Backed by `GET /v1/libraries/{id}/instances?place_id=` (exactly that place) and `?unplaced=true`.
- **Refresh-token families**: every OAuth authorization starts a family; rotated tokens stay in it. A refresh token presented a second time proves that two parties hold copies, so the whole family (refresh and access tokens) is revoked and the user must authorize again. The legitimate client that retries an old token after a lost response gets the same treatment, the standard trade-off of OAuth 2.1.

## Consequences

- The service worker is no longer generated, so its routing rules live in `sw.ts` (and must match the denylist in `vite.config.ts` comments if routes are added).
- Push needs VAPID keys on the server (`biblisann vapid`) and, on iOS, an installed PWA.
