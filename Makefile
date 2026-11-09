# Developer tasks. Run `make help` for the list.
SHELL   := /bin/bash
.DEFAULT_GOAL := help

GO      ?= go
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/baoopn/baoopn-2026/internal/platform/app.Version=$(VERSION)

ifneq (,$(wildcard .env))
include .env
export
endif

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

.PHONY: deps
deps: ## Start local Postgres and RustFS (dev media bucket)
	docker compose up -d postgres rustfs rustfs-init

.PHONY: run-api
run-api: ## Run the Go API on :8080
	$(GO) run ./cmd/api

.PHONY: run-mediaworker
run-mediaworker: ## Run the WebP media worker against the local API (needs INTERNAL_API_TOKEN)
	$(GO) run -tags nodynamic ./cmd/mediaworker

.PHONY: proto
proto: ## Lint and regenerate gRPC code (buf, protoc-gen-go, protoc-gen-go-grpc)
	buf lint
	buf generate

.PHONY: web-dev
web-dev: ## Run the SvelteKit dev server on :5173 (proxies /api to :8080)
	pnpm --dir web dev

.PHONY: build
build: ## Build the API binary and the web app
	CGO_ENABLED=0 $(GO) build -trimpath -tags nodynamic -ldflags '$(LDFLAGS)' -o bin/ ./cmd/...
	pnpm --dir web install --frozen-lockfile
	pnpm --dir web build

.PHONY: test
test: ## Run Go tests with the race detector
	$(GO) test -race -count=1 ./...

.PHONY: pg
pg: ## Start an embedded Postgres on :5434 for DB tests (blocks)
	$(GO) run ./internal/testdb/cmd/pg

.PHONY: test-db
test-db: ## Run Go tests against the embedded Postgres from `make pg`
	TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5434/postgres?sslmode=disable' $(GO) test -count=1 ./...

.PHONY: mail-preview
mail-preview: ## Render every email to internal/contact/testdata/email/
	$(GO) test -count=1 ./internal/contact -run TestEmailGolden -update

.PHONY: docker
docker: ## Build both Docker images
	docker build -f deploy/docker/api.Dockerfile --build-arg VERSION=$(VERSION) -t baoopn-api:dev .
	docker build -f deploy/docker/web.Dockerfile -t baoopn-web:dev .

.PHONY: backup-test
backup-test: ## Backup image roundtrip and status tests (Docker)
	sh deploy/backup/test/roundtrip.sh
	SKIP_BUILD=1 sh deploy/backup/test/status.sh

.PHONY: lint
lint: ## go vet + svelte-check
	$(GO) vet ./...
	pnpm --dir web check

.PHONY: fmt
fmt: ## Format Go and web sources
	gofmt -w cmd internal migrations
	pnpm --dir web exec prettier --write .
