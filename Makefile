.DEFAULT_GOAL := help

LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

VERSION ?= dev
LDFLAGS = -ldflags "-X github.com/mozilla/markfluence/internal/buildinfo.Version=$(VERSION)"

GOLANGCI_LINT_VERSION ?= v2.6.0
GOLANGCI_LINT = $(LOCALBIN)/golangci-lint-$(GOLANGCI_LINT_VERSION)

.PHONY: help all build install lint vet fmt test regen-regressions

help:  ## Show this help
	@echo "Available rules:"
	@grep -F -h "##" Makefile | grep -F -v grep | sed 's/\(.*\):.*##/\1:  /'

all: build

build: $(LOCALBIN)  ## Build the markfluence binary into ./bin
	go build $(LDFLAGS) -o $(LOCALBIN)/markfluence .

install:  ## Install the markfluence binary
	go install $(LDFLAGS) .

lint: $(GOLANGCI_LINT)  ## Lint with golangci-lint
	$(GOLANGCI_LINT) run

vet:  ## Run go vet
	go vet ./...

fmt:  ## Format Go files
	go fmt ./...

test:  ## Run tests
	go test ./...

regen-regressions:  ## Regenerate the converter regression goldens
	go test ./internal/convert -run TestRegression -update

# golangci-lint (version/path defined near the top so `lint` can depend on it).
# Order-only dependency on $(LOCALBIN) so adding files to bin/ (e.g. `make
# build`) doesn't retrigger the install. Installs the versioned binary once.
$(GOLANGCI_LINT): | $(LOCALBIN)
	GOBIN=$(LOCALBIN) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	mv $(LOCALBIN)/golangci-lint $(GOLANGCI_LINT)
