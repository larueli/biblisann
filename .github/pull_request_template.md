## What and why

<!-- One or two sentences. Link the roadmap item. -->

## Checklist

- [ ] PR title is a Conventional Commit (it becomes the squash commit and sets the next version)
- [ ] `make check` is green
- [ ] Tests added or updated (behavior, not just coverage)
- [ ] `docs/` and the OpenAPI spec updated if behavior changed
- [ ] New decision recorded as an ADR; new dependency has a row in `docs/adr/0005-dependency-policy.md`
- [ ] `docs/roadmap.md` checkbox and `docs/HANDOFF.md` updated
- [ ] Security review done if this touches auth, authz, MCP, safehttp, secrets, Dockerfile, `deploy/` or `.github/`
- [ ] No secret, token or personal data (names, emails, IPs, hostnames, home paths) in the diff, the PR text or the commit messages
- [ ] Migration is forward-only and safe for a rolling upgrade (if applicable)
