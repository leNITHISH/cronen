GO            ?= go
GOLANGCI_LINT ?= golangci-lint
BIN           ?= bin/cronen

.PHONY: all build test lint demo clean

all: build

build:
	$(GO) build -o $(BIN) ./cmd/cronen

test:
	$(GO) vet ./...
	$(GO) test -race ./...

lint:
	$(GOLANGCI_LINT) run ./...

demo:
	$(GO) run ./cmd/cronen --demo

clean:
	rm -rf bin
