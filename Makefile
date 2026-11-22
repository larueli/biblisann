# Biblisann task runner. Same targets locally and in CI (see .github/workflows/ci.yml).
# Recipes start with ">" instead of a tab (.RECIPEPREFIX).
SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c
.RECIPEPREFIX := >
.DEFAULT_GOAL := help

# Pin tool versions (AGENTS.md: no floating versions).
GOVULNCHECK_VERSION ?= v1.1.4
IMAGE ?= biblisann:dev

.PHONY: help hooks task check commit-lint docs-check fmt lint test vuln generate web-check dev docker

help: ## List targets
> @grep -hE '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-12s %s\n", $$1, $$2}'

hooks: ## Install the git hooks (prek: pre-commit, commit-msg, pre-push)
> prek install

# Usage: NAME=0-config make task      or      NAME=0-config AGENT=claude make task
task: ## Create an isolated worktree + branch for one task (NAME, optional AGENT=claude|codex)
> @scripts/new-task.sh "$${NAME:-}" "$${AGENT:-}"

check: ## Run every gate through prek, both stages (identical in CI)
> prek run --all-files
> prek run --all-files --hook-stage pre-push

# The message comes from the environment ($$MSG) and is never expanded by make, so a hostile
# PR title cannot inject shell code. Usage: MSG="feat(api): add search" make commit-lint
commit-lint: ## Check $MSG against Conventional Commits (CI uses it for PR titles)
> @f="$$(mktemp)"; rc=0; printf '%s\n' "$$MSG" > "$$f"; \
>   prek run conventional-pre-commit --hook-stage commit-msg --commit-msg-filename "$$f" || rc=$$?; \
>   rm -f "$$f"; exit $$rc

# Primitives below are called by the prek hooks and can be run alone.
docs-check: ## Verify required docs exist and AGENTS.md stays small
> @scripts/check-docs.sh

fmt: ## Format Go and web code
> @if [ -f go.mod ]; then gofmt -w $$(git ls-files '*.go'); fi
> @if [ -f web/package.json ]; then cd web && pnpm exec biome format --write .; fi

lint: ## Lint Go and web code
> @if [ -f go.mod ]; then golangci-lint run ./...; else echo "lint: no go.mod yet, skipped"; fi
> @if [ -f web/package.json ]; then cd web && pnpm exec biome check .; fi

test: ## Run Go tests with the race detector (Docker needed for testcontainers)
> @if [ -f go.mod ]; then go test -race -count=1 ./...; else echo "test: no go.mod yet, skipped"; fi

vuln: ## Scan Go dependencies for known vulnerabilities
> @if [ -f go.mod ]; then go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...; else echo "vuln: no go.mod yet, skipped"; fi

generate: ## Regenerate sqlc, the OpenAPI spec and the web API client (wired in Slice 0)
> @echo "generate: not wired yet (Slice 0)"

web-check: ## Typecheck, test and build the web app
> @if [ -f web/package.json ]; then cd web && pnpm install --frozen-lockfile && pnpm run typecheck && pnpm run test && pnpm run build; else echo "web-check: no web/ yet, skipped"; fi

dev: ## Start the local dev stack (Compose, wired in Slice 0)
> @echo "dev: not wired yet (Slice 0)"

docker: ## Build the container image locally
> @if [ -f Dockerfile ]; then docker build -t $(IMAGE) .; else echo "docker: no Dockerfile yet, skipped"; fi
