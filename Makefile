.PHONY: install web build test linux-amd64

export GOCACHE := $(CURDIR)/.cache/go-build
export GOMODCACHE := $(CURDIR)/.cache/go-mod
export GOPATH := $(CURDIR)/.cache/go

install:
	npm ci --prefix web --cache $(CURDIR)/.cache/npm --no-fund --no-audit
	go mod download

web:
	npm run build --prefix web

build: web
	go build -trimpath -ldflags="-s -w" -o bin/portpilot ./cmd/portpilot

test: web
	go test -race ./...

linux-amd64: web
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o bin/portpilot-linux-amd64 ./cmd/portpilot
