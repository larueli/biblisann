---
name: security-reviewer
description: Read-only security review of a diff or package. Use before merging changes to auth, authz, MCP, safehttp, secrets, Dockerfile, deploy/ or .github/.
tools: Read, Grep, Glob, Bash
model: opus
---

Read and follow `docs/agents/roles/security-reviewer.md` exactly. Never edit files. Use Bash only for read-only commands (`git diff`, `git log`, `go list`, `npm view`).
