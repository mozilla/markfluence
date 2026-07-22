.DEFAULT_GOAL := help

LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

VERSION ?= dev
LDFLAGS = -ldflags "-X github.com/mozilla/markfluence/cmd.Version=$(VERSION)"

.PHONY: help all build install lint vet fmt test regen-regressions parity

help:  ## Show this help
	@echo "Available rules:"
	@grep -F -h "##" Makefile | grep -F -v grep | sed 's/\(.*\):.*##/\1:  /'

all: build

build: $(LOCALBIN)  ## Build the markfluence binary into ./bin
	go build $(LDFLAGS) -o $(LOCALBIN)/markfluence .

install:  ## Install the markfluence binary
	go install $(LDFLAGS) .

lint: $(LOCALBIN)/golangci-lint  ## Lint with golangci-lint
	$(LOCALBIN)/golangci-lint run

vet:  ## Run go vet
	go vet ./...

fmt:  ## Format Go files
	go fmt ./...

test:  ## Run tests
	go test ./...

regen-regressions:  ## Regenerate the converter regression goldens
	go test ./internal/convert -run TestRegression -update

parity:  ## Compare the Python and Go regression outputs (phase-1 aid)
	go run ./tools/paritycheck

# golangci-lint

GOLANGCI_LINT_VERSION ?= v2.6.0
GOLANGCI_LINT = $(LOCALBIN)/golangci-lint-$(GOLANGCI_LINT_VERSION)

$(LOCALBIN)/golangci-lint: $(GOLANGCI_LINT)
	ln -sf $(GOLANGCI_LINT) $(LOCALBIN)/golangci-lint

$(GOLANGCI_LINT): $(LOCALBIN)
	GOBIN=$(LOCALBIN) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	mv $(LOCALBIN)/golangci-lint $(GOLANGCI_LINT)
