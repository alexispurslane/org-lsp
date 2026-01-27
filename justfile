# Set SHELL and use proper flags
set shell := ["sh", "-cu"]

# Default recipe
default:
    @just --list

# Build all binaries
build:
    @echo "Building server..."
    @mkdir -p bin
    go build -o bin/org-lsp ./cmd/server
    @echo "✓ Built: bin/org-lsp"

# Run the LSP server
run build:
    @echo "Starting org-lsp..."
    ./bin/org-lsp

# Build and run the server (alias for convenience)
server: build
    @echo "Starting org-lsp..."
    ./bin/org-lsp

# Run the LSP server tests
test: build
    @echo "Running LSP server integration tests..."
    go test -v ./...

# Run all Go tests
test-unit:
    @echo "Running Go unit tests (excluding integration tests)..."
    go test -v -short ./...

# Run all Go tests with coverage
test-coverage:
    @echo "Running Go tests with coverage..."
    go test -v -coverprofile=coverage.out ./...
    @go tool cover -html=coverage.out -o coverage.html
    @echo "✓ Coverage report: coverage.html"

# Format code
fmt:
    @echo "Formatting code..."
    go fmt ./...
    @echo "✓ Code formatted"

# Lint code (requires golangci-lint)
lint:
    @echo "Running linter..."
    @command -v golangci-lint >/dev/null || (echo "golangci-lint not found. Install with: curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin" && exit 1)
    golangci-lint run ./...
    @echo "✓ Linting passed"

# Format imports
imports:
    @echo "Sorting imports..."
    @command -v goimports >/dev/null || (echo "goimports not found. Install with: go install golang.org/x/tools/cmd/goimports@latest" && exit 1)
    goimports -w .
    @echo "✓ Imports sorted"

# Tidy go.mod
tidy:
    @echo "Tidying go.mod..."
    go mod tidy
    go mod verify
    @echo "✓ go.mod tidied"

# Download dependencies
deps:
    @echo "Downloading dependencies..."
    go mod download
    @echo "✓ Dependencies downloaded"

# Upgrade dependencies
deps-update:
    @echo "Upgrading dependencies..."
    go get -u ./...
    go mod tidy
    @echo "✓ Dependencies upgraded"

# Clean build artifacts
clean:
    @echo "Cleaning build artifacts..."
    rm -rf bin/
    rm -f coverage.out coverage.html
    @echo "✓ Cleaned"

# Show dependencies tree
deps-tree:
    @echo "Dependency tree:"
    go mod graph

# Show outdated dependencies
deps-outdated:
    @echo "Checking for outdated dependencies..."
    go list -u -m all

# Run everything needed before committing (lint, fmt, test)
pre-commit: fmt lint test
    @echo "✓ Pre-commit checks passed!"

# Watch for changes and rebuild (requires entr)
watch-dev:
    @echo "Watching for changes (requires entr)..."
    @command -v entr >/dev/null || (echo "entr not found. Install with: brew install entr" && exit 1)
    @find . -name '*.go' | entr -r just run

# Show Go version
version:
    go version

# Initialize development environment
init: deps tidy build test
    @echo "✓ Development environment initialized"
