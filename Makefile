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