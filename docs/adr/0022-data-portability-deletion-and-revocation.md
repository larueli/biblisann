# 0022. Data portability, account deletion, revocation and bulk ratings

Date: 2026-11-23
Status: Accepted

## Decision

- **Exports** are tar archives of NDJSON plus cover files, referencing each other through local keys and books by ISBN and title, never by database ID, so they import into any instance. A *library export* holds the places tree, copies, editions, the library's lists and the exporting user's loans in it. An *account export* holds the profile (read-only information), contacts, personal lists and wishlist, reading status, grades, and one library file for every library the user **administers** (libraries merely shared with them belong to someone else). Archives contain no credentials, tokens or secrets.
- **Imports** always create new objects (a new library per library file, new lists) and never overwrite or merge: books are matched in the shared catalog by ISBN (or title and publisher for ISBN-less books), covers are re-encoded, and everything goes through the normal services, so permissions and validation apply. The profile in an archive is never applied. Archives are limited to 64 MiB and 200 000 rows.
- **Account deletion** (self-service with the typed email and password, or by an instance administrator) removes the account and what belongs only to it. Libraries only that user uses are deleted; a shared library they alone administer blocks the deletion unless `promote_successors` is set, in which case the most privileged other member becomes administrator. The last active instance administrator cannot be deleted. Catalog books, works and cover files are shared and stay (orphaned cover files are not yet garbage-collected).
- **Revocation**: users list and revoke their own sessions (one, or all others) and the OAuth applications they authorized; administrators list accounts, disable or re-enable them (disabling signs them out), revoke every session, OAuth grant and personal access token of a user, and delete accounts. Disabled accounts fail on every credential type.
- **Hardcover ratings for every book**: a periodic job (every 10 minutes) refreshes the works that are due, at most 50 per run and no faster than `BIBLISANN_HARDCOVER_PER_MINUTE` (default 50 of Hardcover's 60). A rated book is rechecked after `BIBLISANN_HARDCOVER_REFRESH_DAYS` (default 3), an unrated one after 7 days, a failed one after a growing delay (30 minutes per failure, at most 24 hours). On HTTP 429 the job stops, does not mark the book as checked, and River snoozes it for the delay Hardcover asked; three consecutive failures fail the run so River retries with backoff. Books without an ISBN cannot be looked up.
- **S3** is tested against RustFS 1.0.6 started by the test itself.

## Consequences

- Imports can be repeated safely for the catalog but create a new library each time.
- Importers for Goodreads, StoryGraph and LibraryThing share one pipeline (`internal/importer`); a new source is one column mapping.
