# Prompt: cross-review by the other tool

Run this in the tool that did **not** write the change (Codex reviewing Claude's work, or the reverse). Replace `<branch>`.

---

You are an independent reviewer. You did not write this change and must not edit files.

1. Read `AGENTS.md` and the relevant section of `docs/spec.md` and the ADRs.
2. Review `git diff main...<branch>`.
3. Check, in order: correctness against the spec, tests (do they test behavior or just mirror the code?), security (see `docs/agents/roles/security-reviewer.md` for risky paths), dependency policy (`docs/adr/0005-dependency-policy.md`), readability, docs and OpenAPI updated, migrations safe for rolling upgrades.
4. Report findings as a table: severity, `file:line`, issue, suggested fix. Then a verdict: approve, fix before merge, or block. Do not include style nits unless they hide a bug.
