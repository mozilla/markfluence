LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

VERSION ?= dev
LDFLAGS = -ldflags "-X github.com/mozilla/markfluence/cmd.Version=$(VERSION)"

.PHONY: all build install lint vet fmt test

all: build

build: $(LOCALBIN)
	go build $(LDFLAGS) -o $(LOCALBIN)/markfluence .

install:
	go install $(LDFLAGS) .

lint: $(LOCALBIN)/golangci-lint
	$(LOCALBIN)/golangci-lint run

vet:
	go vet ./...

fmt:
	go fmt ./...

test:
	go test ./...

## golangci-lint

GOLANGCI_LINT_VERSION ?= v2.6.0
GOLANGCI_LINT = $(LOCALBIN)/golangci-lint-$(GOLANGCI_LINT_VERSION)

$(LOCALBIN)/golangci-lint: $(GOLANGCI_LINT)
	ln -sf $(GOLANGCI_LINT) $(LOCALBIN)/golangci-lint

$(GOLANGCI_LINT): $(LOCALBIN)
	GOBIN=$(LOCALBIN) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	mv $(LOCALBIN)/golangci-lint $(GOLANGCI_LINT)
