# Writing an addon metadata provider

An addon is any HTTP service that answers one request. Declare it in the JSON file referenced by `BIBLISANN_ADDON_PROVIDERS_FILE`:

```json
[{"name": "mylibrary", "url": "https://addons.example.org/lookup", "token": "optional bearer token"}]
```

Add `mylibrary` to `BIBLISANN_PROVIDERS` (or to a library's lookup settings) to use it. Names are lowercase (`[a-z][a-z0-9_-]`), unique, and cannot reuse a built-in name. URLs must be https and public: calls go through the SSRF-safe client.

## Contract

```
GET <url>?isbn=<isbn13>            Authorization: Bearer <token>   (only when a token is set)

200 application/json
{"title": "Dune", "subtitle": "", "authors": ["Frank Herbert"], "publisher": "Ace", "year": 1990,
 "language": "en", "pages": 412, "description": "...", "series": "Dune", "series_position": 1,
 "cover_url": "https://.../cover.jpg"}

404   the book is unknown
```

Only `title` is required. Everything you return is untrusted text for Biblisann: control characters are stripped, lists are bounded, and `cover_url` is dropped unless it is https (covers are downloaded, re-encoded and size-limited). Answer within a few seconds: requests time out at 15 seconds, and answers (including 404) are cached by Biblisann.
