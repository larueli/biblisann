# Biblisann

Self-hosted management of a physical book library.

- Where are my books? What do I own? Do I already have this one (offline, in a shop)?
- Have I read it? Have I lent it, or must I give it back?

**Status:** `0.x`, foundations (Slice 0): server, API skeleton, web shell, container and Helm chart. Scanning and library features come next (see `docs/roadmap.md`).

## Quick start

```bash
make dev   # PostgreSQL + Biblisann with Docker Compose, http://localhost:8080
make check # every gate, same as CI
```

Install and configuration: `docs/user/`. Architecture: `docs/dev/architecture.md`.

## For humans

- Product requirements: `docs/spec.md`
- Plan: `docs/roadmap.md`
- Decisions: `docs/adr/`

## For AI agents

Start with `AGENTS.md`. Launch prompts are in `docs/prompts/`.

## Container images

Releases follow SemVer and Conventional Commits; a release PR is maintained automatically (`docs/dev/versioning.md`). When it is merged, images are published to GitHub Container Registry as `ghcr.io/<owner>/biblisann`, signed with cosign and shipped with SBOM and provenance attestations (`.github/workflows/publish.yml`).

## License

GNU AGPL-3.0-or-later, see `LICENSE`.
