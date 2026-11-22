# 0006. GitHub as platform, GHCR for images and charts

Date: 2026-11-22
Status: Accepted

## Context

The repository lives on GitHub. Container images and the Helm chart must be easy to pull from Docker, Compose and Kubernetes, with a verifiable supply chain.

## Decision

- Source, issues, PRs and CI on GitHub (Actions, Dependabot).
- Images are published to `ghcr.io/<owner>/biblisann` and the Helm chart to `oci://ghcr.io/<owner>/charts`, on releases only: the releaser (ADR 0009) creates the `vX.Y.Z` tag and calls `publish.yml`. Multi-arch (amd64, arm64).
- Each image carries an SBOM and provenance attestation and is signed with cosign keyless (GitHub OIDC).
- Workflows use least privilege: `contents: read` by default, `packages: write` and `id-token: write` only in the release job.
- Tags are immutable by convention; deployments should pin by digest.

## Consequences

- Vendor lock-in is limited to CI glue: the build is `make`-driven and a Dockerfile, so another CI can reproduce it.
- Package visibility (public/private) must be set once on GHCR after the first push.
