# Security policy

## Reporting a vulnerability

Do not open a public issue, pull request or discussion for a vulnerability.

Use GitHub private vulnerability reporting: on the repository page, open the **Security** tab, then **Report a vulnerability**. Only the maintainer sees the report.

Please include:

- the affected version (`/v1/version`, or the image tag and digest) and how you deploy it;
- the impact in one or two sentences;
- the steps to reproduce, with invented data only.

**Never put real personal data, secrets, tokens or logs you have not scrubbed in a report.** Use reserved examples (`user@example.org`, `192.0.2.1`, `/path/to/...`).

## What to expect

This is a personal project maintained on a best-effort basis. There is no service-level agreement. The maintainer aims to acknowledge a report within 7 days, and to agree a fix and a disclosure date with you. Fixes are released through the normal release process (`docs/dev/versioning.md`) and announced in the GitHub Release notes and a GitHub security advisory. Reporters are credited unless they ask not to be.

## Supported versions

Only the latest release receives security fixes. Before `1.0.0`, upgrade to the newest `0.x` release to get them.

## Scope

In scope: the Biblisann server, the web app, the REST API, the MCP server, the container image and the Helm chart in this repository.

Out of scope: a deployment that does not follow `docs/user/install.md` and `docs/user/security.md` (for example no TLS or an exposed database), vulnerabilities in third-party services (metadata providers, identity providers), and findings that need an already compromised administrator account or host.

## Verifying a release

Images are signed with cosign and carry SBOM and provenance attestations. Verification steps are in `docs/user/install.md`. Verify by digest, not by tag.
