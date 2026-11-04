BIN := bin/amx
PREFIX ?= $(HOME)/.local

.PHONY: build install test fmt vet clean

build:
	@mkdir -p bin
	go build -o $(BIN) ./cmd/amx

install: build
	@mkdir -p $(PREFIX)/bin
	install -m 0755 $(BIN) $(PREFIX)/bin/amx
	@echo "installed $(PREFIX)/bin/amx"

test:
	go test ./...

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './bin/*')

vet:
	go vet ./...

clean:
	rm -rf bin
