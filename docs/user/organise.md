# Organise, lend and automate

## Reading, grades, lists

Open **Reading, grade, lending** under a book to set its status (to read, reading, read, abandoned), a grade from 1 to 10 and a comment. These are yours alone. Grades from outside (critics, providers) are shown separately once available.

Every account has a **wishlist**; **Lists** can hold books you do not own. Items you already own (any edition) are flagged. Under **Missing volumes**, one click adds every missing volume of a series to your wishlist.

## Shelves

The **Shelves** page draws a shelf: a place is the shelf, the places inside it are its layers. Books stand as spines (thicker when they have more pages). Drag a book to another layer, or select it and choose where to move it. Books without a place wait at the bottom.

## Places, labels and inventory

Create places (house, room, shelf…) in a tree. Select one to print its **QR label**. Scan the label in the scanner: the books you scan next go to that place. **Inventory** compares a list of scanned ISBNs with what the place (and everything below it) should contain.

## Loans

Lend one of your copies from its book menu, or record a book you **borrowed** (from a person or a library) in the Loans page. Reminders go out the day before, on the due day, and weekly while overdue.

### Notifications

In **Settings > Notifications** add where you want to be told: email (needs SMTP on the server), a webhook (the request body is signed with the secret shown once, header `X-Biblisann-Signature: sha256=<hmac>`), or an ntfy topic URL. **Send a test** checks them. Targets on private networks are refused for webhooks and ntfy.

On a phone or computer, **Settings > Notifications > Enable notifications on this device** subscribes this browser to push messages (needs the server's VAPID keys; on iPhone, install the app to the home screen first).

A **calendar feed** (Settings > Access tokens > *Create calendar feed*) gives a private URL to subscribe to in any calendar app; it only shows due dates and can be revoked.

## Sharing

Library administrators share a library with a user (by email) or a group, with the role `read`, `edit`, `admin`, or a **custom profile** (a chosen set of permissions). In **Settings > Sharing my libraries** (open a library you administer) you share with a user by email or with a group, and define custom profiles from checkboxes of permissions. Instance administrators create the groups and their members in **Settings > User groups**. The same actions exist in the API (`/v1/libraries/{id}/sharing`, `/profiles`, `/v1/groups`).

## Tokens, scripts and AI assistants

**Settings > Access tokens** creates personal access tokens (`Authorization: Bearer bb_pat_…`). A token is read-only unless you allow changes, never exceeds your rights, and can be restricted to some libraries through the API. The MCP endpoint `/mcp` accepts the same tokens, so an AI assistant can search your library, check whether you own a book, add a scanned ISBN or lend a book. It has no delete tool, and everything it reads from your library is flagged to the assistant as untrusted text.

## Import and backup

**Settings > Import from another service** loads a **Goodreads**, **StoryGraph** or **LibraryThing** export (CSV; LibraryThing's tab-separated export works too): books you read or own get a copy, "to-read" books go to the wishlist, star ratings become grades (five stars is 10), reviews become comments. No metadata provider is queried; re-importing does not duplicate copies.

Backups: `biblisann backup > backup.tar` and `biblisann restore < backup.tar` (into an empty instance) use a versioned logical format that restores across versions, covers included; administrators can also download one in Settings. The archive holds password hashes: store it like the database. Database-level disaster recovery (`pg_dump`, CloudNativePG/barman) is separate and recommended too.

## Cover storage

Covers live in `BIBLISANN_COVERS_DIR`, or in an S3-compatible bucket with `BIBLISANN_STORAGE_BACKEND=s3`. To move: set the S3 variables, run `biblisann migrate-covers local s3`, then switch the backend. The reverse works the same way. Use S3 for several replicas.

## Take your data with you, and leave

- **Export**: *My data > Download all my data* gives one `.tar` with your profile, contacts, lists, reading status, grades and every library you administer (copies, places, loans, covers). A single library: `GET /v1/libraries/{id}/export`. Libraries shared with you belong to their owners and are not included. The file contains no passwords or tokens.
- **Import**: *Restore from an account export* or *Import a library export* creates new libraries and lists; it never overwrites anything, never changes your email or sign-in, and works on another server.
- **Sign-in sessions**: *Where I am signed in* lists your browsers; revoke one, or sign out everywhere else. *Authorized applications* lists the MCP clients you approved; revoke any of them.
- **Delete my account**: removes the account and the libraries only you use. If you alone administer a library that others use, choose a successor first (or tick the option to promote another member). Administrators can disable, sign out or delete accounts in *Settings > Accounts*.

## Community ratings

With a Hardcover token configured, the server fetches Hardcover's rating for every book with an ISBN in the background (about 50 requests a minute at most, retried when Hardcover is busy or down) and refreshes them every few days. They appear next to your own grade and never replace it.

## Hardcover (two-way)

*Settings > Hardcover*: paste your Hardcover API token and choose the direction: **both ways**, **Hardcover to Biblisann only**, or **Biblisann to Hardcover only**. Reading status and your rating are kept in step, books are matched by ISBN, and when both sides changed a book the most recent edit wins. A large library syncs a bit at a time in the background (Hardcover allows about 60 requests a minute). *Unlink* forgets the token and leaves your data untouched on both sides. If Hardcover rejects the token, the screen asks you to link again.
