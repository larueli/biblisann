# 0011. License and project identity

Date: 2026-11-22
Status: Accepted

## Context

The project is public and self-hosted. The license must keep modified network deployments open, and the identity (owner, module path) must be fixed before the first import path is written.

## Decision

- License: **GNU AGPL-3.0-or-later**. Every Go source file need not carry a header; the `LICENSE` file and the `license` field of the Helm chart and `package.json` are authoritative.
- GitHub owner `larueli`, repository `biblisann` (public). Go module path `github.com/larueli/biblisann`. Image `ghcr.io/larueli/biblisann`, chart `oci://ghcr.io/larueli/charts/biblisann`.
- Dependencies must be compatible with AGPL-3.0 (permissive licenses: MIT, BSD, Apache-2.0, ISC, MPL-2.0).

## Consequences

- Anyone running a modified version as a network service must offer the source. The UI shows a link to the source (task for Slice 1).
- Contributions are accepted under the same license. A DCO sign-off or CLA is not required for now (open decision if a dual license is ever wanted).
