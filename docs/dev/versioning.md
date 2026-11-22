# Versioning, commits and releases

Decisions: ADR 0009 (versioning and releaser) and ADR 0010 (hooks).

## Rules

- **SemVer 2.0.0.** The git tag `vMAJOR.MINOR.PATCH` is the single source of truth. Nobody edits a version number or `CHANGELOG.md` by hand; the releaser does.
- **Conventional Commits 1.0.0** for every commit and every PR title.
- **Squash merge only**, with the PR title as the commit message. The title is what the releaser reads.
- Before 1.0.0 (`0.x`): a breaking change bumps MINOR, a feature bumps MINOR, a fix bumps PATCH. After 1.0.0: breaking = MAJOR, feat = MINOR, fix/perf = PATCH.

## Commit format

```
<type>(<scope>)<!>: <summary in imperative mood, lowercase, no period>

<body: why, not what>

BREAKING CHANGE: <what breaks and how to migrate>
Refs: #123
```

Types (enforced by the commit-msg hook and the `pr-title` CI job): `feat`, `fix`, `perf`, `refactor`, `docs`, `test`, `build`, `ci`, `chore`, `revert`.

Suggested scopes: `api`, `mcp`, `auth`, `catalog`, `providers`, `loans`, `sync`, `web`, `pwa`, `db`, `config`, `helm`, `docker`, `deps`.

| Commit | Release effect | In changelog |
|--------|----------------|--------------|
| `feat` | MINOR | Features |
| `fix` | PATCH | Bug Fixes |
| `perf` | PATCH | Performance |
| `refactor`, `docs` | none alone | yes (Refactoring, Documentation) |
| `build`, `ci`, `test`, `chore` | none alone | hidden |
| any type with `!` or `BREAKING CHANGE:` | MAJOR (MINOR before 1.0) | Breaking changes |

## What counts as a breaking change here

- Removing or changing a field, status code or semantics of a stable `/v1` API operation (CI runs `oasdiff`).
- Renaming or removing an environment variable, config key or Helm value, or changing its default in a way that alters behavior.
- Changing an MCP tool name, input schema or scope.
- A database migration that is not safe for a rolling upgrade, or that cannot run on the previous minor.
- Changing the backup/export format so older exports no longer restore.

Mark these with `!` and a `BREAKING CHANGE:` footer that tells the operator what to do.

## How a release happens

1. Work lands on `main` through squash-merged PRs.
2. On each push to `main`, `release-please.yml` updates one **release PR**: next version, `CHANGELOG.md`, and the version file.
3. You review and merge that PR. That is the decision to release.
4. The same workflow run creates the tag and the GitHub Release (notes = changelog section) and calls `publish.yml`.
5. `publish.yml` validates the tag, builds the multi-arch image, pushes it to GHCR with SBOM and provenance, signs it with cosign, and pushes the Helm chart.

Image tags: `X.Y.Z`, `X.Y` (not for pre-releases) and `sha-<short>`. There is no `latest` tag; deployments should pin by digest.

To republish an existing tag (for example after a registry outage), run the `publish` workflow manually with the tag. To force a version (for example `1.0.0`), add a commit with a `Release-As: 1.0.0` footer.

## Version in the binary

The build receives the tag through the `VERSION` build argument and injects it with `-ldflags "-X"`. The same value is returned by `/v1/version`, used as the OpenAPI `info.version`, and written to the Helm chart `appVersion` (Slice 0 task).

## Local workflow

```bash
make hooks                              # once per clone
MSG="feat(api): add isbn lookup" make commit-lint   # check a message or PR title
make check                              # the same gates as CI
```

## Repository settings (GitHub)

- Merge method: squash only, default commit message = **pull request title**.
- Protect `main`: require the `check` and `pr-title` jobs, no direct pushes, linear history.
- GitHub App (variable `RELEASE_APP_ID`, secret `RELEASE_APP_PRIVATE_KEY`, setup in `docs/dev/github.md`). Without it the release PR is created, but GitHub does not run CI on it.
