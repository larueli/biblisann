# 0009. SemVer, Conventional Commits and a PR-based releaser

Date: 2026-11-22
Status: Accepted

## Context

Several humans and AI agents commit to the repository. Versions, tags, changelogs and image publishing must be derived from commit history, reviewable, and not depend on anyone remembering to bump a number.

## Decision

- SemVer 2.0.0, Conventional Commits 1.0.0, squash merges with the PR title as the commit message.
- **release-please** (manifest mode) is the releaser: it keeps one release PR with the next version and `CHANGELOG.md`; merging it creates the tag and the GitHub Release. It runs in GitHub Actions but only reads Conventional Commits and writes plain files and tags, so another CI could replace it.
- The release workflow calls `publish.yml` (reusable workflow, also runnable by hand) to build and push the image and Helm chart to GHCR. Publishing is chained from the release run rather than triggered by the tag, because tags created with the default `GITHUB_TOKEN` do not start other workflows.
- Pre-1.0: breaking changes bump MINOR.

## Alternatives considered

- **git-cliff** (changelog only) plus scripts: flexible, but we would own the tagging and PR logic.
- **semantic-release**: fully automatic on every merge, no human gate on what ships, Node-based.
- **GoReleaser**: strong for Go binaries and images; may be added later for binary artifacts. Not needed for the first slices.

## Consequences

- The release decision is a human merge of the release PR.
- A token other than `GITHUB_TOKEN` (GitHub App or fine-grained PAT) is needed so CI runs on the release PR. Tracked as an open decision in the roadmap.
- Bad commit messages produce bad changelogs, hence the commit-msg hook and the `pr-title` CI check.
