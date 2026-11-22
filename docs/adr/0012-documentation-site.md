# 0012. Documentation site with Zensical

Date: 2026-11-22
Status: Accepted

## Context

User and developer documentation (priority 6) must be browsable, multilingual later, and the project needs a public face. MkDocs Material is entering maintenance mode; its authors build **Zensical** as the successor, reading the same Markdown and a compatible configuration.

## Decision

- The docs site is built with **Zensical** from `docs/` and `zensical.toml`, published to GitHub Pages by a workflow.
- Zensical is a build-time Python tool, pinned by exact version in the Makefile (`ZENSICAL_VERSION`) and run with `uvx`. It is not a dependency of the application.
- The site root is a **landing page** (`docs/index.md` using the `home` template and the overrides in `docs/overrides/`), separate from the documentation tree, so a designed landing page can be added without moving the docs. Slice 0 ships a sober placeholder; a designed page is a later task.
- Agent working files (`HANDOFF.md`, `agents.md`, `agents/`, `prompts/`) stay in `docs/` but are excluded from the site (`exclude_docs`). ADRs and the roadmap are built but only linked from the roadmap and spec pages.

## Alternatives considered

- Plain Markdown on GitHub: no search, no landing page.
- MkDocs Material: works today, but it is in maintenance; migrating later costs more than starting on Zensical.

## Consequences

- Zensical is young: pin the version, keep the Markdown portable (standard extensions only) so a switch back to MkDocs stays cheap.
