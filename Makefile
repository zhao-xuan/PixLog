PIXLOG_BINARY := bin/pixlog
GIT_PIXLOG_BINARY := bin/git-pixlog

.PHONY: build test vet check install clean

build:
	go build -trimpath -o $(PIXLOG_BINARY) ./cmd/pixlog
	go build -trimpath -o $(GIT_PIXLOG_BINARY) ./cmd/git-pixlog

test:
	go test ./... -count=1

vet:
	go vet ./...

check: test vet build

install:
	go install ./cmd/pixlog ./cmd/git-pixlog

clean:
	rm -rf bin