PIXLOG_BINARY := bin/pixlog
GIT_PIXLOG_BINARY := bin/git-pixlog
VERSION ?= 0.1.0-dev
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
DIST_DIR ?= dist
LDFLAGS ?= -X github.com/pixlog/pixlog/internal/cli.Version=$(VERSION)

.PHONY: build package test vet check install clean

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(PIXLOG_BINARY) ./cmd/pixlog
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(GIT_PIXLOG_BINARY) ./cmd/git-pixlog

package:
	bash scripts/package.sh "$(VERSION)" "$(GOOS)" "$(GOARCH)" "$(DIST_DIR)"

test:
	go test ./... -count=1

vet:
	go vet ./...

check: test vet build

install:
	go install ./cmd/pixlog ./cmd/git-pixlog

clean:
	rm -rf bin
	rm -rf dist