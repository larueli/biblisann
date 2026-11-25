# First run

1. Open your instance. The first screen is the sign-in page: choose **No account yet? Create one**. The first account of an instance becomes its administrator; afterwards registration is closed unless `BIBLISANN_REGISTRATION=open`.
2. Create a library (for example "Home").
3. Tap **Scan**, allow the camera, and point it at the barcode on the back of a book (the long one starting with 978 or 979). You can also type the ISBN.
4. The book is looked up (BnF, then Open Library by default), stored with its cover, and added to your library. If you already own the book, or another edition of it, you are told instead of getting a duplicate.
5. Back on the library page, search by title, author or ISBN. Accents, case and small typos do not matter.

## Offline

Install the app (browser menu: *Install app* / *Add to Home screen*). The list of your books is kept on the phone and refreshed whenever you are online. In a shop without network, scan a book: Biblisann answers from the phone whether you own it. New scans wait in a queue and are sent when the network returns; sending twice never creates two copies.

Offline books and queued scans are kept separately for each account. The app verifies the signed-in account before sending queued scans, so they wait while the session cannot be verified. When the scanner reconnects, it removes cached books from libraries you can no longer read; pending scans for those libraries remain queued if access is restored. Signing out removes that account's offline data, including pending scans.

After upgrading from a version that used a shared offline database, its existing cached books and queued scans remain in browser storage but are not loaded or sent automatically, because their account ownership cannot be established safely. Re-scan any unsent books after signing in; do not clear browser storage until you have recovered anything you need.

## Metadata providers

Looking a book up sends its ISBN to the providers. You can choose, per library, which providers to use and in which order (library settings: `{"providers": ["bnf"], "languages": ["fr"]}`), and per request with `?providers=` and `?languages=`. Administrators can disable providers instance-wide.

When an existing edition is refreshed, Biblisann only contacts providers allowed by every library that holds a copy. This applies to scheduled and manual refreshes; an empty provider list in any holding library prevents external refresh for that edition. Such a refresh is recorded as skipped, and the book's metadata is kept. Editions with no library copies use the instance defaults.
