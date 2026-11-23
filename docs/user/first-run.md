# First run

1. Open your instance. The first screen is the sign-in page: choose **No account yet? Create one**. The first account of an instance becomes its administrator; afterwards registration is closed unless `BIBLISANN_REGISTRATION=open`.
2. Create a library (for example "Home").
3. Tap **Scan**, allow the camera, and point it at the barcode on the back of a book (the long one starting with 978 or 979). You can also type the ISBN.
4. The book is looked up (BnF, then Open Library by default), stored with its cover, and added to your library. If you already own the book, or another edition of it, you are told instead of getting a duplicate.
5. Back on the library page, search by title, author or ISBN. Accents, case and small typos do not matter.

## Offline

Install the app (browser menu: *Install app* / *Add to Home screen*). The list of your books is kept on the phone and refreshed whenever you are online. In a shop without network, scan a book: Biblisann answers from the phone whether you own it. New scans wait in a queue and are sent when the network returns; sending twice never creates two copies.

## Metadata providers

Looking a book up sends its ISBN to the providers. You can choose, per library, which providers to use and in which order (library settings: `{"providers": ["bnf"], "languages": ["fr"]}`), and per request with `?providers=` and `?languages=`. Administrators can disable providers instance-wide.
