# GitHub setup

The repository is `larueli/biblisann`, public. This page records how it was created and configured, so it can be rebuilt or moved without guessing. Do the sections in order: repository, settings, releaser App, first CI run, branch protection, first release.

## 1. Create the repository

1. GitHub > New repository. Owner `larueli`, name `biblisann`, **Public**.
2. Leave it **empty**: no README, no license, no `.gitignore`, no template. The first push must not conflict with a GitHub-made commit.
3. Default branch `main`.
4. Check your git identity before the first push. History is public and carries the author name and email of every commit. Use a public identity such as the GitHub noreply address (`<id>+<login>@users.noreply.github.com`, the id is your numeric account id from github.com/settings/emails) and tick *Block command line pushes that expose my email* in the GitHub email settings. Verify with `git log --all --format='%an <%ae>' | sort -u`.
5. Add the remote and push `main` (the pre-push hooks run the full `make check`, so it takes a while):

   ```bash
   git remote add origin git@github.com:larueli/biblisann.git
   git push -u origin main
   ```

   The push needs an SSH key registered on the account (or HTTPS with `gh auth login`). If git reports `Permission denied (publickey)` while `ssh -T git@github.com` works, check `GIT_SSH_COMMAND` and `core.sshCommand`: they can force another key.

Push **before** enabling branch protection, which blocks direct pushes to `main`.

## 2. Repository settings

Settings > General > Pull Requests:

- Allow squash merging **only**. Default commit message = **Pull request title** (not "title and commit details": release-please parses the whole commit message, and a stray `feat:` or `BREAKING CHANGE:` line in a branch commit body would change the version).
- Disable merge commits and rebase merging.
- Automatically delete head branches. Leave auto-merge and "always suggest updating" off.

Settings > Actions > General:

- Workflow permissions **read-only**. Do **not** allow Actions to create or approve pull requests (the releaser App does it, section 3).
- Require approval for workflows from first-time contributors (the repository is public).

Settings > Pages: source **GitHub Actions** (the `docs` workflow deploys the site). Do not pick the suggested Jekyll or Static HTML workflows. If the first deployment fails, check Settings > Environments > `github-pages` allows `main`.

Settings > Advanced Security:

- Private vulnerability reporting **on** (`SECURITY.md` points to it).
- Dependency graph, Dependabot alerts, malware alerts, Dependabot security updates **on**. Version updates are configured in `.github/dependabot.yml`, so leave grouped security updates off.
- Secret protection and push protection **on**.
- Code scanning: CodeQL **default setup** (Go, JavaScript/TypeScript and Actions). It runs inside GitHub and adds no workflow file to pin.
- Leave the AI scan features and agentic autofix **off**: they spend AI credits, send code to an AI service, and `AGENTS.md` forbids wiring agents to act on the repository with credentials.

Packages: after the first publish, set the image and chart packages to *public* and link them to the repository.

## 3. Releaser GitHub App

release-please needs a token that lets CI run on its release PR (ADR 0009). The default `GITHUB_TOKEN` cannot: with the settings above, release-please fails with "GitHub Actions is not permitted to create or approve pull requests", and even if that setting were allowed, GitHub does not start CI on a PR opened with `GITHUB_TOKEN`. Create the App before expecting the first release PR.

Settings > Developer settings > GitHub Apps > New GitHub App:

| Field | Value |
|---|---|
| Name | `biblisann-releaser` (any unique name) |
| Homepage URL | the repository URL |
| Callback URL, "Request user authorization" | empty, unchecked |
| Webhook | **uncheck Active** |
| Repository permissions | **Contents: Read and write**, **Pull requests: Read and write**, **Metadata: Read-only** |
| Account and organization permissions | none |
| Subscribe to events | none |
| Where can this App be installed | **Only on this account** |

The workflow asks for exactly `contents: write` and `pull-requests: write`. Add nothing else (no Workflows, Actions, Issues or Administration).

Then:

1. Copy the **App ID** (the number, not the Client ID).
2. *Private keys* > **Generate a private key**: a `.pem` file downloads.
3. *Install App* > your account > **Only select repositories** > `biblisann`.
4. Repository > Settings > Secrets and variables > Actions: add the **variable** `RELEASE_APP_ID` and the **secret** `RELEASE_APP_PRIVATE_KEY` (the whole `.pem`, BEGIN and END lines included).
5. Delete the local `.pem`. Never paste it in a chat, an issue or a commit.
6. Re-run the failed `release` run, or push to `main`: a release PR `chore(main): release 0.x` appears.

To rotate the key: generate a new one, update the secret, delete the old key on the App page.

## 4. Check the first CI run

Push, then open the Actions tab. The `ci` workflow runs the jobs `check`, `pr-title`, `image`, `helm`, `e2e` and `api-compat`; `docs`, `release` and CodeQL run too. Fix what fails before protecting `main`. `e2e` can be reproduced locally with `make e2e`.

## 5. Protect `main`

Settings > Rules > Rulesets (or Branch protection) on `main`: require a pull request, require the status checks `check` and `pr-title` (add `image`, `helm`, `e2e` and `api-compat` once green), block force pushes and deletions, require linear history, require up-to-date branches.

## 6. First release

Merge the first feature PRs, wait for the release PR (`chore(main): release 0.1.0`), check its changelog, merge it. The `release` workflow tags, creates the GitHub Release and calls `publish.yml`. Check the signed image by digest against the exact trusted publishing workflow identity and issuer, as described in [Install](../user/install.md), and run `docker buildx imagetools inspect ghcr.io/larueli/biblisann:0.1.0`. Verify the actual identity for both the automatic reusable-workflow release and a manual republish before relying on the instructions: neither path has run on GitHub yet. The Helm chart is not currently signed. Versions stay `0.x` until Slice 3 is delivered.

## Public repository and automation

Issues, PRs and comments are written by strangers. No workflow may run untrusted content with a write token or secrets: no `pull_request_target`, no `issue_comment` automation that checks out or interprets PR text. The same applies to AI agents (see `AGENTS.md`). `zizmor` audits workflows in `make check`.

## Moving or renaming the repository

Renaming within the same account and transferring to another account or organization are both breaking for operators: the image name, the chart path and the signature identity contain the owner and the repository name. Mark the commit that changes them with `!` and a `BREAKING CHANGE:` footer, and write a new ADR (ADR 0011 records the current identity).

**What GitHub keeps or redirects**

- Web and git URLs redirect, so existing clones keep working. Update your own remote anyway: `git remote set-url origin <new url>`.
- Settings, branch protection, Dependabot configuration, repository secrets and variables, issues and PRs move with the repository.

**What does not follow, and what to do**

| Item | What happens | Action |
|---|---|---|
| Releaser App | An App installed "only on this account" cannot be installed elsewhere. A rename in the same account keeps the installation. | After a transfer to another owner: create a new App there (section 3), replace `RELEASE_APP_ID` and `RELEASE_APP_PRIVATE_KEY`, delete the old App and key. |
| Container image and chart (GHCR) | Packages stay under the old owner and the old name. | The next release publishes under the new owner (`ghcr.io/<owner>/biblisann`, `ghcr.io/<owner>/charts/biblisann`). Set the new packages to public and link them to the repository. Announce the change in the release notes and keep the old packages for a while. |
| Signature identity | The certificate identity contains `github.com/<owner>/<repo>/.github/workflows/publish.yml`. Old images still verify with the old identity only. | Update the `cosign verify` commands in `docs/user/install.md`, and verify a new release again (section 6). |
| Pages | The site URL changes (`https://<owner>.github.io/<repo>/`). | Set Pages to *GitHub Actions* again if needed, update `site_url`, and re-check the environment rule. |
| Actions OIDC, rulesets, required checks | Can reference the old name. | Re-check the Actions tab, then re-add the required checks if they were dropped. |
| Dependabot, code scanning | Settings move, but check that they are still enabled. | Settings > Advanced Security. |

**What to change in the repository.** Find every occurrence with `grep -rIn 'larueli' . --exclude-dir=node_modules --exclude-dir=.git --exclude-dir=dist --exclude=pnpm-lock.yaml`. Today they are:

- `go.mod` and every Go import of `github.com/larueli/biblisann` (rewrite with `go mod edit -module <new path>` and a search and replace over `*.go`, then `make generate` and `make check`), the `-X` linker flag in `Makefile` and `Dockerfile`, the `local-prefixes` entry in `.golangci.yml`.
- `Dockerfile` (`org.opencontainers.image.source`), `deploy/helm/biblisann/Chart.yaml` (`home`, `sources`) and `values.yaml` (`image.repository`), `deploy/compose/compose.example.yaml`.
- `zensical.toml` (`site_url`, `repo_url`, `repo_name`) and `docs/overrides/home.html`.
- `docs/user/install.md` (clone URL, image, `cosign verify` identity, Helm OCI path), `docs/user/configuration.md` and `internal/config/config.go` (default of `BIBLISANN_SOURCE_URL`, which must point at the exact source of the running version: AGPL section 13), `docs/adr/0011-license-and-project-identity.md` (superseded by the new ADR).
- Anything else the `grep` finds, including this page.

Forks and operators must set `BIBLISANN_SOURCE_URL` to their own source location.
