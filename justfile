# Help
help:
  just -l

# Build
build:
  go build -o mcp-language-server

# Install locally
install:
  go install

# Format code
fmt:
  gofmt -w .

# Generate LSP types and methods
generate:
  go run ./cmd/generate

# Run code audit checks
check:
  gofmt -l . | grep -v integration_tests/workspaces/ | grep -v integration_tests/test-output/
  test -z "$(gofmt -l . | grep -v integration_tests/workspaces/ | grep -v integration_tests/test-output/)"
  go tool staticcheck ./...
  go tool errcheck ./...
  find . -path "./integration_tests/workspaces" -prune -o \
    -path "./integration_tests/test-output" -prune -o \
    -name "*.go" -print | xargs gopls check
  go tool govulncheck ./...

# Run tests
test:
  go test ./...

# Update snapshot tests
snapshot:
  UPDATE_SNAPSHOTS=true go test ./integration_tests/...
