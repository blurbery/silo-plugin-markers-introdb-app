.PHONY: build test lint clean build-all

BINARY=plugin
PLATFORMS=linux/amd64 linux/arm64 darwin/arm64
VERSION ?= $(shell git describe --tags --always 2>/dev/null | sed 's/^v//')
LDFLAGS=-s -w -X main.version=$(VERSION)

build:
	GOWORK=off go build -trimpath -ldflags="$(LDFLAGS)" -o $(BINARY) .

test:
	GOWORK=off go test ./...

lint:
	golangci-lint run ./...

clean:
	rm -f $(BINARY)

build-all:
	@for platform in $(PLATFORMS); do \
		GOOS=$${platform%%/*} GOARCH=$${platform##*/} CGO_ENABLED=0 GOWORK=off \
		go build -trimpath -ldflags="$(LDFLAGS)" -o dist/$(BINARY)-$${platform%%/*}-$${platform##*/} .; \
	done

