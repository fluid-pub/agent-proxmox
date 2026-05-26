GO ?= go
CONFIG ?= config/agent.yml
BINARY ?= dist/fluid-agent-proxmox
VERSION ?= 0.1.1
LDFLAGS := -ldflags "-s -w -X main.Version=$(VERSION)"

.PHONY: deps dev build build-linux test fmt lint help monorepo-replace

# Use when this tree lives under the Fluid workspace (code/agents/proxmox) with agent-core at ../core.
monorepo-replace:
	$(GO) mod edit -replace fluid/agents/core=../core
	@$(GO) mod tidy

deps:
	@test -d core || test -d ../core || (echo "Run: git submodule update --init --recursive (public repo) or develop from fluid monorepo with ../core" && exit 1)
	$(GO) mod download
	@$(GO) mod tidy

dev: deps
	@test -f env.secrets || (echo "Missing env.secrets. Create it from env.secrets.example." && exit 1)
	@set -a; . ./env.secrets; set +a; $(GO) run ./cmd -config $(CONFIG)

build: deps
	$(GO) build ./...

build-linux: deps
	@mkdir -p dist
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build $(LDFLAGS) -o $(BINARY) ./cmd

test: deps
	$(GO) test ./...

fmt:
	gofmt -w .
	$(GO) fmt ./...

lint:
	@echo "No linter configured for this module yet."

help:
	@echo "Targets: deps dev build build-linux test fmt monorepo-replace"
