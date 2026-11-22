# Developer setup on a Windows host

Recommended: **WSL2 (Ubuntu) for everything**, Windows only for the IDE UI, browser and phone tooling. The Makefile and scripts assume a Linux shell, and both agents support WSL2 well.

## Windows side

- Windows 11 with WSL2 (`wsl --install`), Windows Terminal.
- Docker Desktop with the WSL2 integration enabled for your Ubuntu distro (needed by testcontainers and Compose).
- JetBrains: GoLand (covers Go and the TypeScript/React frontend). VS Code is optional (Playwright trace viewer, Dev Containers).
- Android platform-tools (`adb`) and Chrome, for PWA testing on a real phone.

## Inside WSL (Ubuntu)

Keep the repository in the Linux filesystem (`~/code/biblisann`), not under `/mnt/c`.

- Base: `git`, `make`, `gh` (`gh auth login`), `curl`, `jq`
- Go (current stable), Node.js LTS with `corepack enable` (pnpm)
- Go tools: `golangci-lint`, `sqlc`, `goose`, `oasdiff`
- Delivery: `helm`, `kubectl`, `kind` or `k3d` (chart tests), `cosign` (optional)
- Hooks: `prek` (`uv tool install prek`, `pipx install prek`, or its standalone installer), then `make hooks` once per clone
- Workflow security: `actionlint`, `zizmor`, `pinact` (pin Actions by SHA)
- AI agents: Claude Code (`curl -fsSL https://claude.ai/install.sh | bash`) and Codex CLI (standalone installer, or `npm install -g @openai/codex`)

## IDE

- Open the project from the WSL path (`\\wsl$\Ubuntu\home\<you>\code\biblisann`) so toolchains run inside WSL.
- Use the IDE terminal (WSL shell) to run `claude` and `codex`; review agent diffs in the IDE's Git tool window.
- AI Assistant chat supports Codex and Claude Agent through ACP for quick edits. Project subagents and hooks need the CLI.

## Testing the PWA on your Android phone

Camera access needs a secure context. With USB debugging on:

```bash
adb reverse tcp:8080 tcp:8080   # phone's localhost:8080 -> your dev server
```

Then open `http://localhost:8080` in Chrome on the phone (localhost counts as secure) and debug it from `chrome://inspect` on Windows.

## First run

```bash
cd ~/code/biblisann
make hooks
make check
# One worktree per task: creates ../biblisann-wt/0-plan on branch docs/0-plan, then starts claude in it
TASK_TYPE=docs NAME=0-plan AGENT=claude make task      # or AGENT=codex
```

Then paste `docs/prompts/launch-slice0.md`.
