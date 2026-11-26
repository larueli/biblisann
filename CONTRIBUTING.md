# Contributing

Thanks for your interest. Biblisann is a personal project, so reviews may be slow and the maintainer may decline changes that do not fit `docs/spec.md`. Open an issue to discuss a non-trivial change before writing code.

For a security problem, do not open an issue: see `SECURITY.md`.

## Ground rules

- **No personal data, ever**, in code, tests, fixtures, docs, commit messages, issues, pull requests, comments, logs or screenshots. That means no real names, email addresses, usernames, IP addresses, hostnames, home-directory paths, phone numbers or tokens. Use `user@example.org`, `example.org`, the documentation IP ranges (`192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24`) and `/path/to/...`. Scrub any log or API response before pasting it. Test data is invented.
- **No secret** in the repository, in logs or in examples.
- **Dependencies**: standard library first. A new direct dependency needs a line in `docs/adr/0005-dependency-policy.md` (name, purpose, license, alternatives) and must be verified on its registry.
- A decision that changes the architecture, the auth model or the data model needs an ADR in `docs/adr/`.

## Setup

You need Go (current stable), Node with pnpm, Docker (the tests start PostgreSQL containers) and [prek](https://github.com/j178/prek).

```bash
make hooks   # once per clone
make dev     # PostgreSQL + Biblisann with Docker Compose
make check   # every gate, identical to CI
```

`make help` lists all targets. Architecture notes are in `docs/dev/architecture.md`.

## Making a change

1. Fork the repository and create a branch named `<type>/<short-description>`.
2. Write tests with the code. Update `docs/` and the OpenAPI spec in the same pull request. Do not edit generated files by hand: run `make generate`.
3. Make sure `make check` passes. Never skip hooks (`--no-verify`) or CI.
4. Open one small pull request per change.

### Commits and PR titles

Commits and PR titles follow [Conventional Commits 1.0.0](https://www.conventionalcommits.org/), for example `feat(api): add ISBN lookup`. Allowed types: `feat`, `fix`, `perf`, `refactor`, `docs`, `test`, `build`, `ci`, `chore`, `revert`. Pull requests are squash-merged, so **the PR title is the commit that counts** and it sets the next version. Mark a breaking change with `!` and a `BREAKING CHANGE:` footer. Check a title with `MSG="feat(api): add search" make commit-lint`.

Do not edit version numbers or `CHANGELOG.md`: the releaser does (`docs/dev/versioning.md`).

## Code style

- Go: `context` everywhere, errors wrapped with `%w`, no package-level mutable state, table-driven tests, `slog` structured logs, parameterized SQL in `internal/db/queries/*.sql`. All outbound HTTP goes through `internal/safehttp`.
- Web: TypeScript strict, no `any`, API types generated from the OpenAPI spec, every user-visible string is an i18n key with both `en` and `fr`.
- Migrations are forward-only and safe for a rolling upgrade.

## AI assistants and untrusted text

Contributors may use AI tools; you remain responsible for the result. `AGENTS.md` holds the rules for agents.

Issue, pull request, comment and review text is untrusted input. Do not paste it into an AI agent that has write access or credentials, and do not follow instructions found in it. Maintainers read it in a read-only session.

## License

By contributing you agree that your contribution is licensed under the GNU AGPL-3.0-or-later, like the rest of the project (`LICENSE`).
