GO  ?= go
BIN ?= bin/cronen

# The golangci-lint version is pinned here and nowhere else: CI runs
# `make lint` too. `make lint` installs this exact version into bin/.
GOLANGCI_LINT_VERSION := v2.15.0
GOLANGCI_LINT         := $(CURDIR)/bin/golangci-lint-$(GOLANGCI_LINT_VERSION)

# Release and demo tooling, pinned the same way.
GORELEASER_VERSION := v2.19.0
GORELEASER         := $(CURDIR)/bin/goreleaser-$(GORELEASER_VERSION)
VHS_VERSION        := v0.12.1
VHS                := $(CURDIR)/bin/vhs-$(VHS_VERSION)

.PHONY: all build test lint demo gif release-check snapshot clean

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

# Records doc/demo.gif from doc/demo.tape. vhs also needs ttyd and ffmpeg.
gif: build $(VHS)
	$(VHS) doc/demo.tape

release-check: $(GORELEASER)
	$(GORELEASER) check

# A local release build into dist/, without publishing anything.
snapshot: $(GORELEASER)
	$(GORELEASER) release --snapshot --clean

$(GORELEASER):
	GOBIN=$(CURDIR)/bin/.tools $(GO) install github.com/goreleaser/goreleaser/v2@$(GORELEASER_VERSION)
	mv $(CURDIR)/bin/.tools/goreleaser $@

$(VHS):
	GOBIN=$(CURDIR)/bin/.tools $(GO) install github.com/charmbracelet/vhs@$(VHS_VERSION)
	mv $(CURDIR)/bin/.tools/vhs $@

clean:
	rm -rf bin
