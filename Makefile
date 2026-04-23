BINARY := agent-deepweb
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/agent-deepweb

build-mock:
	go build -o mockdeep ./cmd/mockdeep

mock:
	go run ./cmd/mockdeep

mock-dev:
	go run ./cmd/agent-deepweb $(ARGS)

test:
	go test ./... -count=1

test-short:
	go test ./... -count=1 -short

lint:
	golangci-lint run ./...

fmt:
	gofmt -w .
	@command -v goimports >/dev/null && goimports -w . || echo "goimports not installed (optional; install: go install golang.org/x/tools/cmd/goimports@latest)"

clean:
	rm -f $(BINARY) mockdeep
	rm -f release/agent-deepweb-*

dev:
	go run ./cmd/agent-deepweb $(ARGS)

vet:
	go vet ./...

.PHONY: build build-mock mock mock-dev test test-short lint fmt clean dev vet
