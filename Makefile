SHELL := /bin/bash
.DEFAULT_GOAL := help

BIN      := bin/glowpod
WEB_DIST := backend/internal/web/dist
NODE_MODULES := frontend/node_modules/.modules.yaml
SITE_MODULES := site/node_modules/.modules.yaml
# The version stamped into the binary and image: the git tag, else the chart's appVersion.
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//' || echo dev)
IMAGE    ?= glowpod
LDFLAGS  := -s -w -X main.version=$(VERSION)
# Platforms `make dist` builds the glowpod download for.
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

.PHONY: help deps dev web build dist test test-backend test-frontend e2e lint lint-backend lint-frontend fmt clean image image-push chart site site-dev

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

web: $(NODE_MODULES)
	cd frontend && pnpm build
	find $(WEB_DIST) -mindepth 1 ! -name .gitkeep -delete
	cp -R frontend/dist/. $(WEB_DIST)/

build: web ## Build the frontend and one Go binary that embeds it, at bin/glowpod
	cd backend && CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o ../$(BIN) ./cmd/glowpod

dist: web ## Build the glowpod downloads for every platform, with checksums, in dist/
	rm -rf dist && mkdir dist
	@set -e; for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; name=glowpod_$(VERSION)_$${os}_$${arch}; exe=glowpod; \
		[ "$$os" = windows ] && exe=glowpod.exe; \
		echo "  $$name"; mkdir dist/$$name; \
		(cd backend && CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" -o ../dist/$$name/$$exe ./cmd/glowpod); \
		cp LICENSE README.md CHANGELOG.md dist/$$name/; \
		if [ "$$os" = windows ]; then (cd dist && zip -qr $$name.zip $$name); \
		else tar -C dist -czf dist/$$name.tar.gz $$name; fi; \
		rm -r dist/$$name; \
	done
	cd dist && shasum -a 256 glowpod_* > checksums.txt

test: test-backend test-frontend ## Run all tests

test-backend:
	cd backend && go test -race ./...

test-frontend: $(NODE_MODULES)
	cd frontend && pnpm test

e2e: $(NODE_MODULES) ## Run Playwright smoke tests (needs: pnpm exec playwright install chromium)
	cd frontend && pnpm e2e

$(SITE_MODULES): site/package.json site/pnpm-lock.yaml
	cd site && pnpm install --frozen-lockfile
	@touch $@

site: $(SITE_MODULES) ## Build the glowpod.dev website into site/dist and check it against its CSP
	cd site && pnpm format:check && pnpm build

site-dev: $(SITE_MODULES) ## Run the website's dev server (open http://localhost:5180)
	cd site && pnpm dev

image: ## Build the container image for this machine (needs Docker): make image IMAGE=registry/glowpod
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE):$(VERSION) .

image-push: ## Build for amd64 and arm64 and push: make image-push IMAGE=registry.example.com/you/glowpod
	docker buildx build --platform linux/amd64,linux/arm64 --build-arg VERSION=$(VERSION) \
		-t $(IMAGE):$(VERSION) --push .

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
	rm -rf bin dist frontend/dist site/dist
	find $(WEB_DIST) -mindepth 1 ! -name .gitkeep -delete
