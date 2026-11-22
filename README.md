# Biblisann

Self-hosted management of a physical book library.

- Where are my books? What do I own? Do I already have this one (offline, in a shop)?
- Have I read it? Have I lent it, or must I give it back?

**Status:** bootstrap. No code yet; Slice 0 is next (see `docs/roadmap.md`).

## For humans

- Product requirements: `docs/spec.md`
- Plan: `docs/roadmap.md`
- Decisions: `docs/adr/`

## For AI agents

Start with `AGENTS.md`. Launch prompts are in `docs/prompts/`.

## Container images

Releases follow SemVer and Conventional Commits; a release PR is maintained automatically (`docs/dev/versioning.md`). When it is merged, images are published to GitHub Container Registry as `ghcr.io/<owner>/biblisann`, signed with cosign and shipped with SBOM and provenance attestations (`.github/workflows/publish.yml`).

## License

To be decided (see open decisions in `docs/roadmap.md`).
