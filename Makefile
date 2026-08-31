# ==================================================================================== #
# VARIABLES
# ==================================================================================== #

APP_NAME := srosha
MODULE   := github.com/Serajian/srosha

# Two independently deployable binaries over one shared core.
BINARIES  := gateway dispatcher console
BUILD_DIR := build
CMD_DIR   := ./cmd

# Local development only. In production nothing publishes a host port: the
# services talk over the private docker network on their fixed ports
# (gRPC 50051, REST 8080, dispatcher 8081, nats 4222, postgres 5432).
# See docs/reference/srosha-infra-deploy.md §4.
BASE_PORT ?= 7000

# The port the gateway's gRPC server listens on. Read from NOTIF_GRPC_ADDR in
# .env (":50051" -> "50051"); falls back to the deployed value.
GRPC_PORT ?= $(shell grep -sE '^NOTIF_GRPC_ADDR=' .env | tail -1 | cut -d= -f2 | tr -d '[:space:]' | sed 's/.*://')
ifeq ($(strip $(GRPC_PORT)),)
GRPC_PORT := 50051
endif

DOCKER_DIR      := deployment/app
DOCKER_COMPOSE  := $(DOCKER_DIR)/docker-compose.yml
DOCKER_FILE     := $(DOCKER_DIR)/Dockerfile

# The local dependencies only -- postgres and nats, each published on loopback
# so a binary running here with `go run` can reach them. Deliberately a
# different file from DOCKER_COMPOSE: that one is the deployed stack, publishes
# no host port, and is written on its own branch.
DOCKER_COMPOSE_DEV := $(DOCKER_DIR)/docker-compose.dev.yml

# Where each binary answers /healthz and /readyz. Read from the per-binary env
# file (":8080" -> "8080"); falls back to the documented default.
GATEWAY_HEALTH_PORT ?= $(shell grep -sE '^NOTIF_GRPC_HTTP_ADDR=' .env.gateway .env | tail -1 | cut -d= -f2 | tr -d '[:space:]' | sed 's/.*://')
ifeq ($(strip $(GATEWAY_HEALTH_PORT)),)
GATEWAY_HEALTH_PORT := 8080
endif

DISPATCHER_HEALTH_PORT ?= $(shell grep -sE '^NOTIF_HTTP_ADDR=' .env.dispatcher .env | tail -1 | cut -d= -f2 | tr -d '[:space:]' | sed 's/.*://')
ifeq ($(strip $(DISPATCHER_HEALTH_PORT)),)
DISPATCHER_HEALTH_PORT := 8081
endif
DOCKER_IMAGE    := $(APP_NAME):latest
DOCKER_ENV_FILE := .env

MIGRATIONS_DIR := migrations
# migrate-up runs goose ON THIS MACHINE and reaches postgres through the local
# port mapping. It cannot work on the server: nothing there has goose, and the
# deployed postgres publishes no host port at all -- "postgres:5432" is a name
# that only resolves inside srosha-net. Use migrate-server there, which runs
# goose from the image, in a container on that network.
#
# Migration target. Override per environment; never hard-code credentials here.
#   make migrate-up DB_URL="postgres://user:pass@host:port/db?sslmode=disable"
# Role and database name match the deployed cluster. The host and port are the
# local mapping only; in production this is postgres:5432 on the private network.
# Passwords: letters, digits and hyphens only — see the infra doc §5.
DB_USER     ?= srosha
DB_PASSWORD ?= srosha
DB_HOST     ?= 127.0.0.1
DB_PORT     ?= $(shell echo $$(( $(BASE_PORT) + 1 )))
DB_NAME     ?= srosha
DB_URL      ?= postgres://$(DB_USER):$(DB_PASSWORD)@$(DB_HOST):$(DB_PORT)/$(DB_NAME)?sslmode=disable

NATS_PORT   ?= $(shell echo $$(( $(BASE_PORT) + 2 )))

GOOSE_DRIVER ?= postgres

COLOR_RESET=\033[0m
COLOR_GREEN=\033[32m
COLOR_YELLOW=\033[33m
COLOR_RED=\033[31m
COLOR_BLUE=\033[34m

TEST_TIMEOUT=10m
INTEGRATION_TEST_TIMEOUT=15m
INTEGRATION_DIR := ./tests/integration

GO_BIN := $(shell go env GOPATH)/bin
GOVULNCHECK := $(GO_BIN)/govulncheck
GOVULNDB ?= https://vuln.go.dev

# Find a golangci-lint v2 binary wherever it happens to live, rather than hard-coding one
# machine's path — this has to work on CI and on someone else's laptop too.
GOLANGCI_LINT := $(shell \
	    for bin in /opt/homebrew/bin/golangci-lint $$(which -a golangci-lint 2>/dev/null) "$(GO_BIN)/golangci-lint"; do \
	       if [ -x "$$bin" ] && "$$bin" version 2>/dev/null | grep -Eq 'version (v)?2\.'; then \
	          echo "$$bin"; exit 0; \
	       fi; \
	    done; \
	    echo golangci-lint \
)

# Default target
.DEFAULT_GOAL := help

# ==================================================================================== #
# HELP
# ==================================================================================== #

.PHONY: help
help: ## [Help] Show this help
	@echo ""
	@echo "Usage:"
	@echo "  make <target>"
	@echo ""
	@awk 'BEGIN { FS=":.*## "; } \
	/^[a-zA-Z0-9][a-zA-Z0-9_-]*:.*## / { \
	   target=$$1; desc=$$2; \
	   group="Other"; \
	   if (substr(desc,1,1)=="[") { \
	      rb=index(desc,"]"); \
	      if (rb>0) { \
	         group=substr(desc,2,rb-2); \
	         desc=substr(desc,rb+1); \
	         gsub(/^[ \t]+/,"",desc); \
	      } \
	   } \
	   items[group]=items[group] sprintf("  \033[36m%-20s\033[0m %s\n", target, desc); \
	   if (!(group in seen)) { order[++n]=group; seen[group]=1 } \
	} \
	END { \
	   for (i=1; i<=n; i++) { \
	      g=order[i]; \
	      printf "\033[33m%s\033[0m\n", g; \
	      printf "%s\n", items[g]; \
	   } \
	}' $(MAKEFILE_LIST)
	@echo ""

# ==================================================================================== #
# SETUP & DEPENDENCIES
# ==================================================================================== #
.PHONY: at-first
at-first: setup-dev deps git-precommit ## [Setup] First-time setup (tools + deps + git hook)

.PHONY: setup-dev
setup-dev: ## [Setup] Install dev tools (gofumpt, gci, golangci-lint, goose, buf, ...)
	@echo "$(COLOR_YELLOW)📦 Installing development tools...$(COLOR_RESET)"
	@go install github.com/segmentio/golines@latest
	@go install github.com/daixiang0/gci@latest
	@go install mvdan.cc/gofumpt@latest
	@go install github.com/gordonklaus/ineffassign@latest
	@go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
	@go install github.com/client9/misspell/cmd/misspell@latest
	@go install github.com/jondot/goweight@latest
	@go install golang.org/x/vuln/cmd/govulncheck@latest
	@go install github.com/pressly/goose/v3/cmd/goose@latest
	@go install github.com/bufbuild/buf/cmd/buf@latest
	@go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
	@echo "$(COLOR_GREEN)✅ Dev tools installed successfully.$(COLOR_RESET)"

.PHONY: deps
deps: ## [Setup] Tidy and download Go modules
	@echo " 📦 Downloading and cleaning dependencies..."
	@go mod tidy
	@go mod download
	@echo " ✅ Dependencies are up to date."

# ==================================================================================== #
# BUILD
# ==================================================================================== #
.PHONY: build
build: ## [Build] Build every binary into ./build
	@echo "$(COLOR_YELLOW)🔨 Building $(BINARIES)...$(COLOR_RESET)"
	@mkdir -p $(BUILD_DIR)
	@for b in $(BINARIES); do \
	   echo "  → $$b"; \
	   go build -o $(BUILD_DIR)/$$b $(CMD_DIR)/$$b || exit 1; \
	done
	@echo "$(COLOR_GREEN)✅ Build complete.$(COLOR_RESET)"

.PHONY: build-gateway
build-gateway: ## [Build] Build only the gateway binary
	@mkdir -p $(BUILD_DIR)
	@go build -o $(BUILD_DIR)/gateway $(CMD_DIR)/gateway
	@echo "$(COLOR_GREEN)✅ gateway built.$(COLOR_RESET)"

.PHONY: build-dispatcher
build-dispatcher: ## [Build] Build only the dispatcher binary
	@mkdir -p $(BUILD_DIR)
	@go build -o $(BUILD_DIR)/dispatcher $(CMD_DIR)/dispatcher
	@echo "$(COLOR_GREEN)✅ dispatcher built.$(COLOR_RESET)"

.PHONY: build-console
build-console: ## [Build] Build only the console binary (the pages people sign in to)
	@mkdir -p $(BUILD_DIR)
	@go build -o $(BUILD_DIR)/console $(CMD_DIR)/console
	@echo "$(COLOR_GREEN)✅ console built.$(COLOR_RESET)"

.PHONY: clean
clean: ## [Build] Remove build artifacts and coverage output
	@rm -rf $(BUILD_DIR) coverage.out
	@echo "$(COLOR_GREEN)✅ Cleaned.$(COLOR_RESET)"

# ==================================================================================== #
# RUN
# ==================================================================================== #
# Built and then run, rather than `go run`: go run returns non-zero when its
# child takes a signal, so a clean Ctrl-C came out looking like a failed build.
# Running the binary means the exit code is the service's own.
.PHONY: run-gateway
run-gateway: build-gateway ## [Run] Run the gateway locally
	@echo "$(COLOR_YELLOW)🚀 Running gateway...$(COLOR_RESET)"
	@$(BUILD_DIR)/gateway

.PHONY: run-dispatcher
run-dispatcher: build-dispatcher ## [Run] Run the dispatcher locally
	@echo "$(COLOR_YELLOW)🚀 Running dispatcher...$(COLOR_RESET)"
	@$(BUILD_DIR)/dispatcher

.PHONY: run-console
run-console: build-console ## [Run] Run the console locally
	@echo "$(COLOR_YELLOW)🚀 Running console...$(COLOR_RESET)"
	@$(BUILD_DIR)/console

.PHONY: free-port
free-port: ## [Run] Free GRPC_PORT by stopping whatever is listening on it
	@pids=$$(lsof -ti tcp:$(GRPC_PORT) -sTCP:LISTEN 2>/dev/null); \
	if [ -z "$$pids" ]; then \
	   echo "$(COLOR_GREEN)✅ port $(GRPC_PORT) is already free.$(COLOR_RESET)"; \
	else \
	   echo "$(COLOR_YELLOW)🔌 port $(GRPC_PORT) is held by:$(COLOR_RESET)"; \
	   lsof -nP -iTCP:$(GRPC_PORT) -sTCP:LISTEN; \
	   kill $$pids 2>/dev/null || true; \
	   sleep 1; \
	   still=$$(lsof -ti tcp:$(GRPC_PORT) -sTCP:LISTEN 2>/dev/null); \
	   if [ -n "$$still" ]; then \
	      echo "$(COLOR_YELLOW)… still up, forcing.$(COLOR_RESET)"; \
	      kill -9 $$still 2>/dev/null || true; \
	      sleep 1; \
	   fi; \
	   if [ -n "$$(lsof -ti tcp:$(GRPC_PORT) -sTCP:LISTEN 2>/dev/null)" ]; then \
	      echo "$(COLOR_RED)❌ could not free port $(GRPC_PORT).$(COLOR_RESET)"; exit 1; \
	   fi; \
	   echo "$(COLOR_GREEN)✅ port $(GRPC_PORT) freed.$(COLOR_RESET)"; \
	fi

.PHONY: rerun
rerun: free-port run-gateway ## [Run] Free GRPC_PORT, then run the gateway

# ==================================================================================== #
# PROTO
# ==================================================================================== #
.PHONY: proto
proto: ## [Proto] Generate Go code from the protobuf definitions (buf generate)
	@echo "$(COLOR_YELLOW)✍️  buf generate...$(COLOR_RESET)"
	@buf generate
	@echo "$(COLOR_GREEN)📚 gen/ regenerated.$(COLOR_RESET)"

.PHONY: proto-lint
proto-lint: ## [Proto] Lint the protobuf definitions
	@echo "$(COLOR_YELLOW)🔎 buf lint...$(COLOR_RESET)"
	@buf lint
	@echo "$(COLOR_GREEN)✅ Proto lint passed.$(COLOR_RESET)"

.PHONY: proto-breaking
proto-breaking: ## [Proto] Detect breaking protobuf changes against master
	@echo "$(COLOR_YELLOW)🔎 buf breaking (against master)...$(COLOR_RESET)"
	@buf breaking --against '.git#branch=master'
	@echo "$(COLOR_GREEN)✅ No breaking proto changes.$(COLOR_RESET)"

# ==================================================================================== #
# FORMAT & BASIC CHECKS
# ==================================================================================== #
.PHONY: format-core
format-core: gofumpt gci misspell govet ineffassign ## [Format] Format code without golines + run basic checks
	@echo "$(COLOR_GREEN)✅ Core code formatted & checked successfully.$(COLOR_RESET)"

.PHONY: format
format: format-core golines ## [Format] Format code + run basic checks
	@echo "$(COLOR_GREEN)✅ Code formatted & checked successfully.$(COLOR_RESET)"

.PHONY: gofumpt
gofumpt: ## [Format] Format Go files using gofumpt (-extra)
	@echo "$(COLOR_YELLOW)🧹 gofumpt...$(COLOR_RESET)"
	@gofumpt -extra -w .

.PHONY: gci
gci: ## [Format] Sort/group imports using gci
	@echo "$(COLOR_YELLOW)🧹 gci...$(COLOR_RESET)"
	@gci write --skip-generated \
	   -s standard \
	   -s "prefix($(MODULE))" \
	   -s default \
	   --custom-order \
	   .

.PHONY: golines
golines: ## [Format] Wrap long lines using golines (max 100)
	@echo "$(COLOR_YELLOW)🧹 golines...$(COLOR_RESET)"
	@golines --max-len=100 -w .

# base ref the fast golines path diffs against
GOLINES_BASE ?= origin/master

.PHONY: golines-changed
golines-changed: ## [Format] golines only on Go files changed vs $(GOLINES_BASE) (fast pre-push path)
	@echo "$(COLOR_YELLOW)🧹 golines (changed vs $(GOLINES_BASE))...$(COLOR_RESET)"
	@base=$$(git merge-base $(GOLINES_BASE) HEAD 2>/dev/null) ; \
	if [ -z "$$base" ]; then \
	   echo "  $(GOLINES_BASE) not found → formatting the whole tree" ; \
	   golines --max-len=100 -w . ; \
	else \
	   files=$$( { git diff --name-only --diff-filter=d "$$base" HEAD -- '*.go' ; \
	               git diff --name-only --diff-filter=d -- '*.go' ; \
	               git diff --name-only --diff-filter=d --cached -- '*.go' ; } | sort -u ) ; \
	   if [ -n "$$files" ]; then \
	      printf '%s\n' "$$files" | tr '\n' '\0' | xargs -0 golines --max-len=100 -w ; \
	   else \
	      echo "  no changed Go files" ; \
	   fi ; \
	fi

.PHONY: misspell
misspell: ## [Format] Fix common misspellings (Go files only)
	@echo "$(COLOR_YELLOW)🔎 misspell (Go files only)...$(COLOR_RESET)"
	@find . -type f -name '*.go' \
	   -not -path './vendor/*' \
	   -not -path './build/*' \
	   -not -path './sdk/go/notification/*' \
	   -not -path './deployment/*' \
	   -not -path './.git/*' \
	   -print0 | xargs -0 misspell -w

.PHONY: govet
govet: ## [Format] Run go vet
	@echo "$(COLOR_YELLOW)🔎 go vet...$(COLOR_RESET)"
	@go vet ./...

.PHONY: ineffassign
ineffassign: ## [Format] Detect ineffectual assignments
	@echo "$(COLOR_YELLOW)🔎 ineffassign...$(COLOR_RESET)"
	@ineffassign ./...

# ==================================================================================== #
# LINT
# ==================================================================================== #
.PHONY: lint
lint: ## [Lint] Run golangci-lint
	@echo "$(COLOR_YELLOW)🧹 Running golangci-lint...$(COLOR_RESET)"
	@$(GOLANGCI_LINT) run --timeout 2m ./...
	@echo "$(COLOR_GREEN)✅ Lint passed.$(COLOR_RESET)"

.PHONY: lint-fix
lint-fix: format ## [Lint] Run format then golangci-lint --fix
	@echo "$(COLOR_YELLOW)🔧 Running golangci-lint with --fix...$(COLOR_RESET)"
	@$(GOLANGCI_LINT) run --fix --timeout 2m ./...
	@echo "$(COLOR_GREEN)✅ Lint autofix done.$(COLOR_RESET)"

# Both halves of `make format`, checked rather than applied. golines is here as
# well as gofmt because it was not, and a repository accumulated 35 files of
# long lines that nothing complained about until somebody ran the formatter and
# got an unrelated diff mixed into their work.
#
# sdk/ is absent here and checked by `make sdk` instead, because it is its own
# module. Note that golangci-lint does NOT cover this: its formatters are
# gofumpt and gci, and neither wraps a long line.
.PHONY: fmt-check
fmt-check: ## [Lint] Fail if anything is unformatted. Read-only, unlike `format`.
	@echo "$(COLOR_YELLOW)🔎 gofmt -l...$(COLOR_RESET)"
	@unformatted=$$(gofmt -l ./cmd ./internal ./pkg 2>/dev/null || true); \
	if [ -n "$$unformatted" ]; then \
	   echo "$(COLOR_RED)❌ not formatted:$(COLOR_RESET)"; \
	   echo "$$unformatted" | sed 's/^/   /'; \
	   echo "   run: make format"; \
	   exit 1; \
	fi
	@echo "$(COLOR_YELLOW)🔎 golines -l...$(COLOR_RESET)"
	@if command -v golines >/dev/null 2>&1; then \
	   toolong=$$(golines --max-len=100 -l ./cmd ./internal ./pkg 2>/dev/null || true); \
	   if [ -n "$$toolong" ]; then \
	      echo "$(COLOR_RED)❌ lines over 100:$(COLOR_RESET)"; \
	      echo "$$toolong" | sed 's/^/   /'; \
	      echo "   run: make format"; \
	      exit 1; \
	   fi; \
	fi
	@echo "$(COLOR_GREEN)✅ Formatting is clean.$(COLOR_RESET)"

# buf comes from `make setup-dev`, and buf.yaml does not exist yet. A missing
# tool or a missing config must not block a commit -- warn and carry on.
.PHONY: proto-lint-if-present
proto-lint-if-present: ## [Lint] Run buf lint if buf and its config are both present
	@if ! command -v buf >/dev/null 2>&1; then \
	   echo "$(COLOR_YELLOW)⚠  buf not installed — skipping proto lint. Run: make setup-dev$(COLOR_RESET)"; \
	elif [ ! -f buf.yaml ] && [ ! -f buf.work.yaml ]; then \
	   echo "$(COLOR_YELLOW)⚠  no buf.yaml — skipping proto lint. Run: buf config init$(COLOR_RESET)"; \
	else \
	   $(MAKE) --no-print-directory proto-lint; \
	fi

# golangci-lint comes from `make setup-dev`. A fresh clone has not run it yet, and
# a missing tool must not block a push -- warn and carry on.
.PHONY: lint-if-present
lint-if-present: ## [Lint] Run golangci-lint if it is installed, warn if it is not
	@if ! $(GOLANGCI_LINT) version >/dev/null 2>&1; then \
	   echo "$(COLOR_YELLOW)⚠  golangci-lint not installed — skipping. Run: make setup-dev$(COLOR_RESET)"; \
	else \
	   $(MAKE) --no-print-directory lint; \
	fi

.PHONY: arch-check
arch-check: ## [Lint] Fail if the domain layer imports anything it must not
	@echo "$(COLOR_YELLOW)🏛  Checking domain layer dependencies...$(COLOR_RESET)"
	@imports=$$(go list -f '{{range .Imports}}{{.}}{{"\n"}}{{end}}' ./internal/core/domain/... 2>/dev/null | sort -u); \
	third=$$(echo "$$imports" | grep -v "^$(MODULE)/" | grep -E '^[^/]+\.' || true); \
	internal=$$(echo "$$imports" | grep "^$(MODULE)/" \
	   | grep -vE '^$(MODULE)/(internal/core/(shared|domain)|pkg/errs)' || true); \
	bad=$$(printf '%s\n%s\n' "$$third" "$$internal" | grep -v '^$$' || true); \
	if [ -n "$$bad" ]; then \
	   echo "$(COLOR_RED)❌ core/domain may import only stdlib, core/shared and pkg/errs.$(COLOR_RESET)"; \
	   echo "   It imports:"; \
	   echo "$$bad" | sed 's/^/     /'; \
	   exit 1; \
	fi
	@echo "$(COLOR_GREEN)✅ Domain layer is clean.$(COLOR_RESET)"
	@echo "$(COLOR_YELLOW)🏛  Checking who opens infrastructure...$(COLOR_RESET)"
	@edges=$$(go list -f '{{$$p := .ImportPath}}{{range .Imports}}{{$$p}} {{.}}{{"\n"}}{{end}}' ./... 2>/dev/null); \
	bad=$$(echo "$$edges" | grep " $(MODULE)/internal/registry$$" \
	   | grep -vE '^$(MODULE)/internal/(registry|bootstrap) ' || true); \
	if [ -n "$$bad" ]; then \
	   echo "$(COLOR_RED)❌ only bootstrap may import internal/registry.$(COLOR_RESET)"; \
	   echo "$$bad" | sed 's/^/     /'; \
	   exit 1; \
	fi; \
	bad=$$(echo "$$edges" | grep -E '^$(MODULE)/internal/infra/' \
	   | grep -E " $(MODULE)/internal/(config|registry)($$|/)" || true); \
	if [ -n "$$bad" ]; then \
	   echo "$(COLOR_RED)❌ internal/infra must not import config or registry.$(COLOR_RESET)"; \
	   echo "$$bad" | sed 's/^/     /'; \
	   exit 1; \
	fi; \
	bad=$$(echo "$$edges" | awk -v a="$(MODULE)/internal/adapter/" \
	   '$$1 ~ "^" a && $$2 ~ "^" a && $$2 != $$1 && index($$2, $$1 "/") != 1' || true); \
	if [ -n "$$bad" ]; then \
	   echo "$(COLOR_RED)❌ one adapter must not import another. Declare the interface"; \
	   echo "   you need in the package that calls it; bootstrap passes the"; \
	   echo "   implementation in.$(COLOR_RESET)"; \
	   echo "$$bad" | sed 's/^/     /'; \
	   exit 1; \
	fi
	@echo "$(COLOR_GREEN)✅ Infrastructure boundaries are clean.$(COLOR_RESET)"

.PHONY: vulncheck
vulncheck: ## [Lint] Scan for known vulnerabilities reachable from our code (govulncheck)
	@echo "$(COLOR_YELLOW)🔒 Running govulncheck (db: $(GOVULNDB))...$(COLOR_RESET)"
	@[ -x "$(GOVULNCHECK)" ] || go install golang.org/x/vuln/cmd/govulncheck@latest
	@$(GOVULNCHECK) -db "$(GOVULNDB)" ./... || { \
	   echo "$(COLOR_RED)⚠️  govulncheck failed. If it was a 403/timeout, vuln.go.dev is likely"; \
	   echo "   geo-blocked here — retry behind a VPN, or point GOVULNDB at a mirror:$(COLOR_RESET)"; \
	   echo "   HTTPS_PROXY=http://host:port make vulncheck"; \
	   echo "   make vulncheck GOVULNDB=<mirror-or-file://path>"; \
	   exit 1; \
	}
	@echo "$(COLOR_GREEN)✅ No reachable vulnerabilities.$(COLOR_RESET)"

# --- what the git hooks run --------------------------------------------------
# These only CHECK. A hook that rewrote files would leave the rewrite unstaged,
# so the unformatted version is what gets committed. Use `make fix` for that.

.PHONY: precommit
precommit: fmt-check govet arch-check sqlc-check proto-lint-if-present ## [Lint] What the pre-commit hook runs (~1s, read-only)
	@echo "$(COLOR_GREEN)✅ Pre-commit checks passed.$(COLOR_RESET)"

.PHONY: prepush
prepush: precommit lint-if-present test-race sdk ## [Lint] What the pre-push hook runs (read-only)
	@echo "$(COLOR_GREEN)✅ Pre-push checks passed.$(COLOR_RESET)"

# sdk/go is a module of its own, and `go test ./...` does NOT descend into a
# nested module. Without this target every check above would pass while never
# once compiling the code customers actually import.
.PHONY: sdk
sdk: ## [Test] Build, vet and test the SDK module
	@echo "$(COLOR_YELLOW)📦 sdk/go...$(COLOR_RESET)"
	@cd sdk/go && go build ./... && go vet ./... && go test -race ./...
	@if command -v golines >/dev/null 2>&1; then \
	   toolong=$$(cd sdk/go && golines --max-len=100 -l . 2>/dev/null || true); \
	   if [ -n "$$toolong" ]; then \
	      echo "$(COLOR_RED)❌ lines over 100 in sdk/go:$(COLOR_RESET)"; \
	      echo "$$toolong" | sed 's/^/   /'; \
	      echo "   run: make format"; \
	      exit 1; \
	   fi; \
	fi
	@if command -v golangci-lint >/dev/null 2>&1; then \
	   cd sdk/go && golangci-lint run; \
	fi
	@echo "$(COLOR_GREEN)✅ SDK module is clean.$(COLOR_RESET)"

# --- run by hand -------------------------------------------------------------

.PHONY: fix
fix: deps format lint-fix ## [Lint] Tidy, format and autofix. WRITES FILES.
	@echo "$(COLOR_GREEN)✅ Everything that can be fixed automatically, was.$(COLOR_RESET)"

# ==================================================================================== #
# GIT
# ==================================================================================== #
.PHONY: git-precommit
git-precommit: ## [Git] Configure git hooks path and enable local git hooks
	@if [ ! -d .githooks ]; then \
	   echo "$(COLOR_YELLOW)no .githooks/ directory — skipping.$(COLOR_RESET)"; \
	else \
	   echo "$(COLOR_YELLOW)🔗 Setting up git hooks...$(COLOR_RESET)"; \
	   git config --local core.hooksPath .githooks; \
	   [ -f .githooks/pre-commit ] && chmod +x .githooks/pre-commit || true; \
	   [ -f .githooks/pre-push ] && chmod +x .githooks/pre-push || true; \
	   echo "$(COLOR_GREEN)✅ Git hooks configured.$(COLOR_RESET)"; \
	fi

.PHONY: clean-branches
clean-branches: ## [Git] Delete all local branches except master
	@echo "$(COLOR_YELLOW)🗑️ Cleaning local branches (except master)...$(COLOR_RESET)"
	@git branch | grep -vE "main|master" | xargs -r git branch -D
	@git fetch --prune
	@echo "$(COLOR_GREEN)✅ Local branches cleaned.$(COLOR_RESET)"

.PHONY: sync
sync: ## [Git] Go to master, sync it with the remote, drop local branches already merged there
	@if [ -n "$$(git status --porcelain --untracked-files=no)" ]; then \
	   echo "$(COLOR_RED)✋ You have uncommitted changes.$(COLOR_RESET)"; \
	   git status --short --untracked-files=no; \
	   echo "$(COLOR_RED)   Checking out master would carry them onto master, which is exactly what we never do.$(COLOR_RESET)"; \
	   echo "$(COLOR_RED)   Commit them on your branch, or stash them, then run make sync again.$(COLOR_RESET)"; \
	   exit 1; \
	fi
	@echo "$(COLOR_YELLOW)🔀 Switching to master...$(COLOR_RESET)"
	@git checkout master
	@echo "$(COLOR_YELLOW)📡 Fetching every remote (pruning branches deleted upstream)...$(COLOR_RESET)"
	@git fetch --all --prune
	@echo "$(COLOR_YELLOW)⬇️  Pulling master...$(COLOR_RESET)"
	@git pull --ff-only
	@echo "$(COLOR_YELLOW)🗑️  Deleting local branches already merged into origin/master...$(COLOR_RESET)"
	@held=$$(git worktree list --porcelain | sed -n 's|^branch refs/heads/||p'); \
	merged=$$(git branch --merged origin/master --format='%(refname:short)' \
	   | grep -vE '^(main|master)$$' || true); \
	deletable=$$(echo "$$merged" | grep -vxF "$$held" || true); \
	if [ -z "$$deletable" ]; then \
	   echo "   nothing to delete"; \
	else \
	   echo "$$deletable" | xargs -n1 git branch -d; \
	fi; \
	inuse=$$(echo "$$merged" | grep -xF "$$held" || true); \
	if [ -n "$$inuse" ]; then \
	   echo "$(COLOR_YELLOW)⚠️  Merged, but a worktree has them checked out, so git cannot delete them:$(COLOR_RESET)"; \
	   echo "$$inuse" | sed 's/^/     /'; \
	   echo "$(COLOR_YELLOW)   Drop the worktree first, then run make sync again:$(COLOR_RESET)"; \
	   here=$$(git rev-parse --show-toplevel); \
	   for b in $$inuse; do \
	      p=$$(git worktree list --porcelain \
	         | awk -v b="$$b" '/^worktree /{p=$$2} $$0=="branch refs/heads/"b{print p}'); \
	      [ "$$p" = "$$here" ] || echo "   git worktree remove $$p"; \
	   done; \
	fi
	@stale=$$(git branch -vv | grep ': gone\]' | sed -E 's/^\*? *([^ ]+).*/\1/' \
	   | grep -vE '^(main|master)$$' || true); \
	if [ -n "$$stale" ]; then \
	   echo "$(COLOR_YELLOW)⚠️  Gone upstream but NOT merged into origin/master — squashed, or genuinely unmerged work.$(COLOR_RESET)"; \
	   echo "$(COLOR_YELLOW)   Left alone on purpose. Check, then delete by hand:$(COLOR_RESET)"; \
	   echo "$$stale" | sed 's/^/   git branch -D /'; \
	fi
	@echo "$(COLOR_GREEN)✅ master is up to date.$(COLOR_RESET)"

# ==================================================================================== #
# DOCKER
# ==================================================================================== #
.PHONY: docker-build
docker-build: ## [Docker] Build the single image that carries all three binaries
	@echo "$(COLOR_YELLOW)🐳 Building $(DOCKER_IMAGE)...$(COLOR_RESET)"
	@docker build -t $(DOCKER_IMAGE) -f $(DOCKER_FILE) .
	@echo "$(COLOR_GREEN)✅ Docker image built. The binary is selected by 'command'.$(COLOR_RESET)"

.PHONY: docker-up
docker-up: ## [Docker] Start docker-compose
	@echo "$(COLOR_YELLOW)🐳📦 Starting docker-compose...$(COLOR_RESET)"
	@docker compose --env-file $(DOCKER_ENV_FILE) -f $(DOCKER_COMPOSE) up -d
	@echo "$(COLOR_GREEN)✅ Docker services up.$(COLOR_RESET)"

.PHONY: docker-down
docker-down: ## [Docker] Stop docker-compose
	@echo "$(COLOR_YELLOW)🐳🛑 Stopping docker-compose...$(COLOR_RESET)"
	@docker compose --env-file $(DOCKER_ENV_FILE) -f $(DOCKER_COMPOSE) down
	@echo "$(COLOR_GREEN)✅ Docker services down.$(COLOR_RESET)"

.PHONY: docker-del
docker-del: ## [Docker] Stop docker-compose and remove its volumes
	@echo "$(COLOR_YELLOW)🐳🗑️ Removing containers and volumes...$(COLOR_RESET)"
	@docker compose --env-file $(DOCKER_ENV_FILE) -f $(DOCKER_COMPOSE) down -v
	@echo "$(COLOR_GREEN)✅ Volumes removed.$(COLOR_RESET)"

.PHONY: docker-reset
docker-reset: docker-del docker-up ## [Docker] Reset docker environment (down -v -> up)
	@echo "$(COLOR_GREEN)🐳♻️ Docker environment reset complete.$(COLOR_RESET)"

.PHONY: docker-logs
docker-logs: ## [Docker] Follow the compose logs
	@docker compose --env-file $(DOCKER_ENV_FILE) -f $(DOCKER_COMPOSE) logs -f

# ==================================================================================== #
# SQLC
# ==================================================================================== #

.PHONY: sqlc
sqlc: ## [SQL] Regenerate the typed query code from migrations/ and queries/
	@echo "$(COLOR_YELLOW)🧬 Generating query code...$(COLOR_RESET)"
	@sqlc generate
	@echo "$(COLOR_GREEN)✅ Query code generated.$(COLOR_RESET)"

.PHONY: sqlc-check
sqlc-check: ## [SQL] Fail if the generated query code is behind the schema
	@command -v sqlc >/dev/null || { \
	   echo "$(COLOR_YELLOW)⚠️  sqlc not installed — skipping. Run: make setup-dev$(COLOR_RESET)"; \
	   exit 0; \
	}
	@sqlc vet 2>/dev/null || true
	@sqlc diff >/dev/null 2>&1 || { \
	   echo "$(COLOR_RED)❌ Generated query code is out of date. Run: make sqlc$(COLOR_RESET)"; \
	   exit 1; \
	}
	@echo "$(COLOR_GREEN)✅ Query code matches the schema.$(COLOR_RESET)"

# ==================================================================================== #
# LOCAL DEPENDENCIES
# ==================================================================================== #
# postgres and nats in containers, the binaries with `go run` on this machine.
# Nothing here is the deployed stack: see DOCKER_COMPOSE_DEV above.

.PHONY: dev-up
dev-up: ## [Dev] Start postgres and nats locally, and wait until both are healthy
	@echo "$(COLOR_YELLOW)🐳 Starting local dependencies...$(COLOR_RESET)"
	@docker compose -f $(DOCKER_COMPOSE_DEV) up -d
	@echo "$(COLOR_YELLOW)⏳ Waiting for them to report healthy...$(COLOR_RESET)"
	@for i in $$(seq 1 40); do \
	   unhealthy=$$(docker compose -f $(DOCKER_COMPOSE_DEV) ps --format '{{.Service}} {{.Health}}' \
	      | grep -v ' healthy$$' || true); \
	   if [ -z "$$unhealthy" ]; then \
	      echo "$(COLOR_GREEN)✅ Local dependencies are up.$(COLOR_RESET)"; \
	      exit 0; \
	   fi; \
	   sleep 2; \
	done; \
	echo "$(COLOR_RED)❌ Still not healthy after 80s:$(COLOR_RESET)"; \
	docker compose -f $(DOCKER_COMPOSE_DEV) ps; \
	exit 1

.PHONY: dev-down
dev-down: ## [Dev] Stop the local dependencies, keeping their data
	@docker compose -f $(DOCKER_COMPOSE_DEV) down
	@echo "$(COLOR_GREEN)✅ Local dependencies stopped.$(COLOR_RESET)"

.PHONY: dev-del
dev-del: ## [Dev] Stop them and delete their volumes
	@docker compose -f $(DOCKER_COMPOSE_DEV) down -v
	@echo "$(COLOR_GREEN)✅ Local dependencies and their data removed.$(COLOR_RESET)"

.PHONY: dev-reset
dev-reset: dev-del dev-up ## [Dev] Throw the local data away and start again

.PHONY: dev-ps
dev-ps: ## [Dev] Show what is running locally
	@docker compose -f $(DOCKER_COMPOSE_DEV) ps

.PHONY: dev-logs
dev-logs: ## [Dev] Follow the local dependency logs
	@docker compose -f $(DOCKER_COMPOSE_DEV) logs -f

.PHONY: dev-ready
dev-ready: ## [Dev] Ask each running binary whether it is ready
	@for pair in "gateway $(GATEWAY_HEALTH_PORT)" "dispatcher $(DISPATCHER_HEALTH_PORT)"; do \
	   set -- $$pair; \
	   printf '%-11s ' "$$1"; \
	   body=$$(curl -s --max-time 2 "localhost:$$2/readyz" 2>/dev/null); \
	   if [ -z "$$body" ]; then \
	      echo "$(COLOR_YELLOW)not running on :$$2$(COLOR_RESET)"; \
	   else \
	      echo "$$body"; \
	   fi; \
	done

# ==================================================================================== #
# MIGRATIONS (goose)
# ==================================================================================== #

.PHONY: migrate-up
migrate-up: ## [Migrations] Apply database migrations
	@echo "$(COLOR_YELLOW)📂 Running migrations up...$(COLOR_RESET)"
	@goose -dir $(MIGRATIONS_DIR) $(GOOSE_DRIVER) "$(DB_URL)" up
	@echo "$(COLOR_GREEN)✅ Migrations applied.$(COLOR_RESET)"

.PHONY: migrate-server-status
migrate-server-status: ## [Migrations] What the SERVER's database has. Changes nothing.
	@test -f $(DOCKER_DIR)/.env || { \
	   echo "$(COLOR_RED)no $(DOCKER_DIR)/.env$(COLOR_RESET)"; \
	   echo "This target runs on the server, in the directory Dokploy cloned"; \
	   echo "into, where it writes that file. On a laptop use: make migrate-status"; \
	   exit 1; }
	@docker compose -f $(DOCKER_COMPOSE) run --rm migrate /app/migrate status

.PHONY: migrate-server
migrate-server: ## [Migrations] Apply migrations ON THE SERVER. A deploy already does this.
	@test -f $(DOCKER_DIR)/.env || { \
	   echo "$(COLOR_RED)no $(DOCKER_DIR)/.env$(COLOR_RESET)"; \
	   echo "This target runs on the server, in the directory Dokploy cloned"; \
	   echo "into, where it writes that file. On a laptop use: make migrate-up"; \
	   exit 1; }
	@echo "$(COLOR_YELLOW)📂 Running migrations from the image...$(COLOR_RESET)"
	@docker compose -f $(DOCKER_COMPOSE) run --rm migrate
	@echo "$(COLOR_GREEN)✅ Migrations applied.$(COLOR_RESET)"

.PHONY: migrate-down
migrate-down: ## [Migrations] Roll back the last migration
	@echo "$(COLOR_YELLOW)📂 Running migrations down...$(COLOR_RESET)"
	@goose -dir $(MIGRATIONS_DIR) $(GOOSE_DRIVER) "$(DB_URL)" down
	@echo "$(COLOR_GREEN)✅ Migration rolled back.$(COLOR_RESET)"

.PHONY: migrate-status
migrate-status: ## [Migrations] Show migration status
	@goose -dir $(MIGRATIONS_DIR) $(GOOSE_DRIVER) "$(DB_URL)" status

.PHONY: migrate-reset
migrate-reset: ## [Migrations] Roll every migration back
	@echo "$(COLOR_RED)⏪ Rolling every migration back...$(COLOR_RESET)"
	@goose -dir $(MIGRATIONS_DIR) $(GOOSE_DRIVER) "$(DB_URL)" reset

.PHONY: migrate-create
migrate-create: ## [Migrations] Create a migration: make migrate-create name=add_notifications
	@if [ -z "$(name)" ]; then \
	   echo "$(COLOR_RED)❌ name is required: make migrate-create name=add_notifications$(COLOR_RESET)"; \
	   exit 1; \
	fi
	@goose -dir $(MIGRATIONS_DIR) create $(name) sql -s
	@echo "$(COLOR_GREEN)✅ Migration created.$(COLOR_RESET)"

# ==================================================================================== #
# TEST & BENCH
# ==================================================================================== #

.PHONY: test
test: test-unit test-integration ## [Test] Run unit + integration tests

.PHONY: test-unit
test-unit: ## [Test] Run unit tests
	@echo "$(COLOR_BLUE)→ Running unit tests...$(COLOR_RESET)"
	@go test -v -race -timeout=$(TEST_TIMEOUT) ./...
	@echo "$(COLOR_GREEN)✓ Unit tests passed$(COLOR_RESET)"

.PHONY: test-race
test-race: ## [Test] Run race-detector tests across all packages (used by prepush)
	@echo "$(COLOR_BLUE)🧪 Running race-detector tests...$(COLOR_RESET)"
	@go test -race -timeout=$(TEST_TIMEOUT) ./...
	@echo "$(COLOR_GREEN)✓ Race tests passed$(COLOR_RESET)"

.PHONY: test-integration
test-integration: ## [Test] Run tests that need a real database and broker (needs: make dev-up)
	@echo "$(COLOR_BLUE)→ Running integration tests...$(COLOR_RESET)"
	@go test -tags integration -timeout=$(INTEGRATION_TEST_TIMEOUT) ./... 
	@if [ -d $(INTEGRATION_DIR) ]; then \
	   go test -tags integration -timeout=$(INTEGRATION_TEST_TIMEOUT) $(INTEGRATION_DIR)/...; \
	fi
	@echo "$(COLOR_GREEN)✓ Integration tests passed$(COLOR_RESET)"

.PHONY: test-short
test-short: ## [Test] Run only short tests
	@echo "$(COLOR_BLUE)→ Running short tests...$(COLOR_RESET)"
	@go test -v -short -race ./...

.PHONY: test-verbose
test-verbose: ## [Test] Run tests with verbose output (no cache)
	@go test -v -race -timeout=$(TEST_TIMEOUT) ./... -count=1

.PHONY: test-coverage
test-coverage: ## [Test] Generate coverage report
	@echo "$(COLOR_BLUE)→ Running tests with coverage...$(COLOR_RESET)"
	@go test ./... -coverprofile=coverage.out -coverpkg=./internal/...,./pkg/...
	@go tool cover -func=coverage.out | grep total | awk '{print "$(COLOR_GREEN)✓ Total coverage: " $$3 "$(COLOR_RESET)"}'

.PHONY: bench
bench: ## [Test] Run benchmarks
	@echo "$(COLOR_BLUE)→ Running benchmarks...$(COLOR_RESET)"
	@go test -bench=. -benchmem ./...

# ==================================================================================== #
# ANALYZE
# ==================================================================================== #
.PHONY: analyze-size
analyze-size: build ## [Analyze] Show total/runtime/custom size for each binary
	@echo "$(COLOR_YELLOW)📊 Analyzing binary size...$(COLOR_RESET)"
	@for b in $(BINARIES); do \
	   BIN=$(BUILD_DIR)/$$b; \
	   TOTAL_SIZE=$$(stat -f%z $$BIN 2>/dev/null || stat -c %s $$BIN); \
	   RUNTIME_SIZE=$$(go tool nm -size $$BIN | grep ' runtime\.' | awk '{sum += $$2} END {print sum}'); \
	   if [ -z "$$RUNTIME_SIZE" ]; then RUNTIME_SIZE=0; fi; \
	   CUSTOM_SIZE=$$((TOTAL_SIZE - RUNTIME_SIZE)); \
	   PCT_RUNTIME=0; PCT_CUSTOM=0; \
	   if [ $$TOTAL_SIZE -gt 0 ]; then \
	      PCT_RUNTIME=$$((RUNTIME_SIZE * 100 / TOTAL_SIZE)); \
	      PCT_CUSTOM=$$((CUSTOM_SIZE * 100 / TOTAL_SIZE)); \
	   fi; \
	   echo "Binary: $$BIN"; \
	   echo "  Total size:   $$TOTAL_SIZE B"; \
	   echo "  Runtime size: $$RUNTIME_SIZE B ($$PCT_RUNTIME%)"; \
	   echo "  Your code:    $$CUSTOM_SIZE B ($$PCT_CUSTOM%)"; \
	done
	@echo "$(COLOR_GREEN)✅ Analysis complete.$(COLOR_RESET)"

.PHONY: analyze-goweight
analyze-goweight: build ## [Analyze] Analyze binary size using goweight
	@echo "$(COLOR_YELLOW)📊 Analyzing binary with goweight...$(COLOR_RESET)"
	@goweight
	@echo "$(COLOR_GREEN)✅ Goweight analysis complete.$(COLOR_RESET)"

# ==================================================================================== #
# GRAPH
# ==================================================================================== #
.PHONY: graph
graph: ## [Graph] Rebuild graph.html from the existing graphify-out/graph.json
	@if ! command -v graphify >/dev/null 2>&1; then \
	   echo "$(COLOR_YELLOW)graphify not installed — skipping.$(COLOR_RESET)"; \
	else \
	   graphify export html && echo "$(COLOR_GREEN)✅ graphify-out/graph.html rebuilt.$(COLOR_RESET)"; \
	fi

.PHONY: graph-open
graph-open: ## [Graph] Open the knowledge graph in the default browser
	@if [ ! -f graphify-out/graph.html ]; then \
	   echo "$(COLOR_YELLOW)No graph yet — run /graphify . in Claude Code first.$(COLOR_RESET)"; \
	else \
	   open graphify-out/graph.html 2>/dev/null || xdg-open graphify-out/graph.html 2>/dev/null || echo "Open graphify-out/graph.html manually."; \
	fi

.PHONY: graph-hooks
graph-hooks: ## [Graph] Install graphify commit hooks + merge driver locally
	@if ! command -v graphify >/dev/null 2>&1; then \
	   echo "$(COLOR_YELLOW)graphify not installed — skipping.$(COLOR_RESET)"; \
	else \
	   graphify hook install && echo "$(COLOR_GREEN)✅ graphify hooks installed.$(COLOR_RESET)"; \
	fi
