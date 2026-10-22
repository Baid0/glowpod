SHELL := /bin/bash
.DEFAULT_GOAL := help

BIN      := bin/glowpod
WEB_DIST := backend/internal/web/dist
NODE_MODULES := frontend/node_modules/.modules.yaml

.PHONY: help deps dev build test test-backend test-frontend e2e lint lint-backend lint-frontend fmt clean

help: ## List the targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F ':.*## ' '{printf "  %-15s %s\n", $$1, $$2}'

deps: $(NODE_MODULES) ## Install Go and frontend dependencies
	cd backend && go mod download

$(NODE_MODULES): frontend/package.json frontend/pnpm-lock.yaml
	cd frontend && pnpm install --frozen-lockfile
	@touch $@

dev: $(NODE_MODULES) ## Run the Go API and the Vite dev server (open http://localhost:5173)
	@trap 'kill 0' EXIT; \
	(cd backend && go run ./cmd/glowpod -dev) & \
	(cd frontend && pnpm dev) & \
	wait

build: $(NODE_MODULES) ## Build the frontend and one Go binary that embeds it, at bin/glowpod
	cd frontend && pnpm build
	find $(WEB_DIST) -mindepth 1 ! -name .gitkeep -delete
	cp -R frontend/dist/. $(WEB_DIST)/
	cd backend && CGO_ENABLED=0 go build -trimpath -o ../$(BIN) ./cmd/glowpod

test: test-backend test-frontend ## Run all tests

test-backend:
	cd backend && go test -race ./...

test-frontend: $(NODE_MODULES)
	cd frontend && pnpm test

e2e: $(NODE_MODULES) ## Run Playwright smoke tests (needs: pnpm exec playwright install chromium)
	cd frontend && pnpm e2e

image: ## Build the container image (needs Docker): make image TAG=0.1.0
	docker build -t glowpod:$(or $(TAG),dev) .

chart: ## Lint the Helm chart and render it in its main setups
	helm lint deploy/helm/glowpod
	helm template g deploy/helm/glowpod > /dev/null
	helm template g deploy/helm/glowpod --set publicURL=https://g.example.com \
		--set auth.oidc.enabled=true --set auth.oidc.issuer=https://sso.example.com --set auth.oidc.clientID=g \
		--set auth.ldap.enabled=true --set auth.ldap.url=ldaps://ldap.example.com --set auth.ldap.userBase=dc=x \
		--set ingress.enabled=true --set networkPolicy.enabled=true > /dev/null

lint: lint-backend lint-frontend ## Run linters, type checks and format checks

lint-backend:
	cd backend && go vet ./... && golangci-lint run ./...

lint-frontend: $(NODE_MODULES)
	cd frontend && pnpm typecheck && pnpm lint && pnpm format:check

fmt: $(NODE_MODULES) ## Format Go and frontend code
	cd backend && golangci-lint fmt ./...
	cd frontend && pnpm format

clean: ## Remove build output
	rm -rf bin frontend/dist
	find $(WEB_DIST) -mindepth 1 ! -name .gitkeep -delete
