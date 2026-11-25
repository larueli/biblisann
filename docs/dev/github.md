# GitHub setup

The repository is `larueli/biblisann`, public. Do these once, after creating it.

## Repository settings

- **General > Pull Requests**: allow squash merging only, default commit message = *Pull request title*. Disable merge commits and rebase merging. Delete head branches on merge.
- **Branch protection (or ruleset) on `main`**: require a PR, require the status checks `check` and `pr-title` (add `image`, `helm`, `e2e` once they have run), block force pushes and deletions, require linear history, require up-to-date branches.
- **Actions > General**: workflow permissions *read-only*; do not allow Actions to create PRs with the default token. Require approval for workflows from first-time contributors (public repository).
- **Pages**: source *GitHub Actions* (the `docs` workflow deploys the site).
- **Code security**: enable Dependabot alerts and security updates, secret scanning with push protection, private vulnerability reporting.
- **Packages**: after the first publish, set the image and chart packages to *public* and link them to the repository.

## Public repository and automation

Issues, PRs and comments are written by strangers. No workflow may run untrusted content with a write token or secrets: no `pull_request_target`, no `issue_comment` automation that checks out or interprets PR text. The same applies to AI agents (see `AGENTS.md`). `zizmor` audits workflows in `make check`.

## Releaser GitHub App

release-please needs a token that lets CI run on its release PR (ADR 0009). Create a GitHub App:

1. Settings > Developer settings > GitHub Apps > New. Name it for example `biblisann-releaser`. Homepage: the repository URL. Disable webhooks.
2. Repository permissions: **Contents: read and write**, **Pull requests: read and write**, **Metadata: read**. Nothing else.
3. Create the App, then *Generate a private key* (a `.pem` file).
4. Install the App on `larueli/biblisann` only.
5. In the repository: add the variable `RELEASE_APP_ID` (the App ID) and the secret `RELEASE_APP_PRIVATE_KEY` (the whole `.pem`). Delete the local `.pem` afterwards.

Until this is done the `release` workflow falls back to `GITHUB_TOKEN`: it works, but GitHub does not start CI on the release PR.

## First release

Merge the first feature PRs, wait for the release PR (`chore(main): release 0.1.0`), check its changelog, merge it. The `release` workflow tags, creates the GitHub Release and calls `publish.yml`. Check the signed image by digest against the exact trusted publishing workflow identity and issuer, as described in [Install](../user/install.md), and run `docker buildx imagetools inspect ghcr.io/larueli/biblisann:0.1.0`. Verify the actual identity for both the automatic reusable-workflow release and a manual republish before relying on the instructions: neither path has run on GitHub yet. The Helm chart is not currently signed. Versions stay `0.x` until Slice 3 is delivered.
