GO  ?= go
BIN ?= bin/cronen

# The golangci-lint version is pinned here and nowhere else: CI runs
# `make lint` too. `make lint` installs this exact version into bin/.
GOLANGCI_LINT_VERSION := v2.15.0
GOLANGCI_LINT         := $(CURDIR)/bin/golangci-lint-$(GOLANGCI_LINT_VERSION)

.PHONY: all build test lint demo clean

all: build

build:
	$(GO) build -o $(BIN) ./cmd/cronen

test:
	$(GO) vet ./...
	$(GO) test -race ./...

lint: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run ./...

$(GOLANGCI_LINT):
	GOBIN=$(CURDIR)/bin/.tools $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	mv $(CURDIR)/bin/.tools/golangci-lint $@

demo:
	$(GO) run ./cmd/cronen --demo

clean:
	rm -rf bin
