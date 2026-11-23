# Biblisann task runner. Same targets locally and in CI (see .github/workflows/ci.yml).
# Recipes start with ">" instead of a tab (.RECIPEPREFIX).
SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c
.RECIPEPREFIX := >
.DEFAULT_GOAL := help

# Pin tool versions (AGENTS.md: no floating versions).
GOVULNCHECK_VERSION ?= v1.10.0
SQLC_VERSION ?= v1.31.1
GOLANGCI_LINT_VERSION ?= v2.15.0
IMAGE ?= biblisann:dev
VERSION ?= dev
ZENSICAL_VERSION ?= 0.1.3

.PHONY: e2e help hooks task check commit-lint docs-check fmt lint test vuln generate generate-check web-check build dev dev-down docker docs docs-serve

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
> @if [ -f go.mod ]; then GOMEMLIMIT=1GiB GOGC=50 go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run --concurrency 2 ./...; else echo "lint: no go.mod yet, skipped"; fi
> @if [ -f web/package.json ]; then cd web && pnpm exec biome check .; fi

test: ## Run Go tests with the race detector (Docker needed for testcontainers)
> @if [ -f go.mod ]; then GOMEMLIMIT=1GiB go test -p 2 -race -count=1 ./...; else echo "test: no go.mod yet, skipped"; fi

vuln: ## Scan Go and web dependencies for known vulnerabilities
> @if [ -f go.mod ]; then go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...; else echo "vuln: no go.mod yet, skipped"; fi
> @if [ -f web/package.json ]; then cd web && pnpm audit --prod --audit-level high; fi

generate: ## Regenerate sqlc code, the OpenAPI spec and the web API client
> go run github.com/sqlc-dev/sqlc/cmd/sqlc@$(SQLC_VERSION) generate
> go run ./cmd/biblisann openapi > api/openapi.json
> cd web && pnpm install --frozen-lockfile && pnpm run generate:api

generate-check: generate ## Fail when generated files are not committed up to date
> git diff --exit-code -- api internal/db/sqlc web/src/api

web-check: ## Typecheck, test and build the web app
> @if [ -f web/package.json ]; then cd web && pnpm install --frozen-lockfile && pnpm run typecheck && pnpm run test && pnpm run build; else echo "web-check: no web/ yet, skipped"; fi

build: ## Build the web app, then the static binary into bin/biblisann
> cd web && pnpm install --frozen-lockfile && pnpm run build
> CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X github.com/larueli/biblisann/internal/version.version=$(VERSION)" -o bin/biblisann ./cmd/biblisann

dev: ## Start PostgreSQL and the app with Compose (http://localhost:8080)
> @scripts/dev-secrets.sh
> docker compose -f deploy/compose/compose.yaml up --build

e2e: build ## Run the Playwright smoke test against a local server (needs Docker for PostgreSQL)
> @scripts/e2e.sh

dev-down: ## Stop the Compose stack (keeps the data volume)
> docker compose -f deploy/compose/compose.yaml down

docker: ## Build the container image locally
> docker build --build-arg VERSION=$(VERSION) -t $(IMAGE) .

docs: ## Build the documentation site into site/
> uvx --from "zensical==$(ZENSICAL_VERSION)" zensical build --clean

docs-serve: ## Serve the documentation site locally
> uvx --from "zensical==$(ZENSICAL_VERSION)" zensical serve
