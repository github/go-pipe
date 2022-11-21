all: build test fmt vet

.PHONY: all

build:
	go build ./...

test:
	go test ./...

# fmt prints files it changes; used by Actions check.
fmt:
	@go fmt ./...

vet:
	go vet ./...

BIN := $(CURDIR)/bin
GO	:= GO
$(BIN):
		mkdir -p $(BIN)

# Run golang-ci lint on all source files:
GOLANGCILINT := $(BIN)/golangci-lint
$(BIN)/golangci-lint:
	GOBIN=$(BIN) $(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

.PHONY: fmt
lint: | $(GOLANGCILINT)
	@echo "$(M) running golangci-lint"
	$(GOLANGCILINT) -c .golangci.yml run
