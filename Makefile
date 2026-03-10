VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -X main.version=$(VERSION)
BINARY  := mcp-language-server

.PHONY: build install test clean version

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

install:
	go install -ldflags "$(LDFLAGS)" .

test:
	go test ./internal/...

clean:
	rm -f $(BINARY)

version:
	@echo $(VERSION)
