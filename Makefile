.PHONY: build test

GO ?= go

build:
	$(GO) build -o bin/tunnelto ./cmd/tunnelto

test:
	$(GO) test ./...
