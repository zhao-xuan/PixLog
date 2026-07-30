BINARY := bin/pixlog

.PHONY: build test vet check install clean

build:
	go build -trimpath -o $(BINARY) ./cmd/pixlog

test:
	go test ./... -count=1

vet:
	go vet ./...

check: test vet build

install:
	go install ./cmd/pixlog

clean:
	rm -rf bin