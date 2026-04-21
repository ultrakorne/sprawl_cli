# sprawl — two-binary build.
#
# `sprawl` (prod) and `sprawl_dev` (dev) ship from the same codebase.
# The only per-binary differences are the APIURL and AppName values injected
# via -ldflags into the internal/build package. See docs/plans/sprawl_cli_evaluation.md.

MODULE      := github.com/ultrakorne/sprawl_cli
PKG_BUILD   := $(MODULE)/internal/build
CMD         := ./cmd/sprawl
DIST        := dist

PROD_APP    := sprawl
DEV_APP     := sprawl_dev
PROD_URL    ?= https://sprawl.up.railway.app
DEV_URL     ?= http://localhost:4000

VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT      ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE        ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

# -s -w strip the binary; the -X vars are what differentiate prod vs dev.
define LDFLAGS
-s -w \
-X '$(PKG_BUILD).APIURL=$(1)' \
-X '$(PKG_BUILD).AppName=$(2)' \
-X '$(PKG_BUILD).Version=$(VERSION)' \
-X '$(PKG_BUILD).Commit=$(COMMIT)' \
-X '$(PKG_BUILD).Date=$(DATE)'
endef

.PHONY: all build build-dev build-all run run-dev tidy test test-race check fmt fmt-check vet clean help

all: build-all ## Build both binaries.

build: $(DIST) ## Build the prod binary ($(PROD_APP)) with PROD_URL baked in.
	CGO_ENABLED=0 go build -trimpath -ldflags "$(call LDFLAGS,$(PROD_URL),$(PROD_APP))" \
		-o $(DIST)/$(PROD_APP) $(CMD)

build-dev: $(DIST) ## Build the dev binary ($(DEV_APP)) targeting DEV_URL.
	CGO_ENABLED=0 go build -trimpath -ldflags "$(call LDFLAGS,$(DEV_URL),$(DEV_APP))" \
		-o $(DIST)/$(DEV_APP) $(CMD)

build-all: build build-dev ## Build both binaries.

run-dev: build-dev ## Build and run the dev binary; pass args via ARGS=.
	$(DIST)/$(DEV_APP) $(ARGS)

run: build ## Build and run the prod binary; pass args via ARGS=.
	$(DIST)/$(PROD_APP) $(ARGS)

tidy: ## go mod tidy.
	go mod tidy

test: ## Run the full test suite.
	go test ./...

test-race: ## Run tests with the race detector (slower; use before releases).
	go test -race ./...

check: fmt-check vet test ## fmt-check + vet + test. Run before every commit.

fmt: ## gofmt everything.
	gofmt -w .

fmt-check: ## Fail if anything is unformatted (CI-friendly; does not rewrite files).
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt needs to run on:"; echo "$$unformatted"; exit 1; \
	fi

vet: ## go vet.
	go vet ./...

clean: ## Remove build artifacts.
	rm -rf $(DIST)

$(DIST):
	@mkdir -p $(DIST)

help: ## Show this help.
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)
