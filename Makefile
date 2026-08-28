.DEFAULT_GOAL := help

LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

VERSION ?= dev
LDFLAGS = -ldflags "-X github.com/mozilla/markfluence/internal/buildinfo.Version=$(VERSION)"

GOLANGCI_LINT_VERSION ?= v2.13.2
GOLANGCI_LINT = $(LOCALBIN)/golangci-lint-$(GOLANGCI_LINT_VERSION)

COMPLETIONS_DIR ?= completions

.PHONY: help all build build-linux install completions lint vet fmt fmt-check test check regen-regressions clean

help:  ## Show this help
	@echo "Available rules:"
	@grep -F -h "##" Makefile | grep -F -v grep | sed 's/\(.*\):.*##/\1:  /'

all: build

build: $(LOCALBIN)  ## Build the markfluence binary into ./bin
	go build $(LDFLAGS) -o $(LOCALBIN)/markfluence .

build-linux: $(LOCALBIN)  ## Cross-compile a linux/amd64 binary into ./bin
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build $(LDFLAGS) -o $(LOCALBIN)/markfluence-linux-amd64 .

install:  ## Install the markfluence binary
	go install $(LDFLAGS) .

# The release generates completions from the CLI itself (a goreleaser `before`
# hook runs this rule), so the shipped scripts can never drift from the
# commands and flags that exist.
completions:  ## Generate bash/zsh/fish completion scripts into ./completions
	rm -rf $(COMPLETIONS_DIR)
	mkdir -p $(COMPLETIONS_DIR)
	for shell in bash zsh fish; do \
	  go run . completion $$shell > $(COMPLETIONS_DIR)/markfluence.$$shell; \
	done

lint: $(GOLANGCI_LINT)  ## Lint with golangci-lint
	$(GOLANGCI_LINT) run

vet:  ## Run go vet
	go vet ./...

fmt:  ## Format Go files
	go fmt ./...

fmt-check:  ## Check formatting without modifying files (fails if any file needs gofmt)
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then \
	  echo "These files are not gofmt'd:"; echo "$$out"; exit 1; fi

test:  ## Run tests
	go test ./...

check:  ## Run every check CI runs, in CI's order -- the pre-flight before calling work done
	@# This is the single definition of "does it pass", used by both a developer
	@# and .github/workflows/ci.yml, so the two cannot drift. Recursive rather
	@# than prerequisites so the order holds under `make -j`, where build and
	@# lint would otherwise race to populate ./bin.
	$(MAKE) vet
	$(MAKE) fmt-check
	$(MAKE) test
	$(MAKE) build
	$(MAKE) lint

regen-regressions:  ## Regenerate the converter regression goldens
	go test ./internal/convert -run TestRegression -update

clean:  ## Remove build artifacts (bin/, dist/, completions/, ./markfluence)
	rm -rf $(LOCALBIN) dist $(COMPLETIONS_DIR) markfluence

# golangci-lint (version/path defined near the top so `lint` can depend on it).
# Order-only dependency on $(LOCALBIN) so adding files to bin/ (e.g. `make
# build`) doesn't retrigger the install. Installs the versioned binary once.
$(GOLANGCI_LINT): | $(LOCALBIN)
	GOBIN=$(LOCALBIN) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	mv $(LOCALBIN)/golangci-lint $(GOLANGCI_LINT)
