# Agent Development Guidelines for org-lsp

**⚠️ CRITICAL FOR AGENTS**: Always use `just` for building and testing (`just build`, `just test`). Never run `go build`/`go test` directly - `just` ensures proper module resolution.

This document serves as a knowledge base for AI agents and developers working on org-lsp. It captures architectural decisions, coding patterns, and common pitfalls to ensure consistent development.

## Project Overview

org-lsp is a minimal LSP server for org-mode files focused on navigation and linking capabilities. It uses glsp for LSP protocol handling and a custom orgscanner package for parsing and indexing.

## Technology Stack

### Core Dependencies
- **LSP Framework**: `github.com/tliron/glsp` v0.2.2 - Protocol-3.16 compliant LSP server framework
- **Org Parser**: `github.com/alexispurslane/go-org` v1.9.1 - Org-mode AST parsing (fork of `niklasfasching` version)
- **Testing**: `github.com/stretchr/testify` v1.11.1 - Rich assertions and test helpers
- **Logging**: `log/slog` - Structured logging (Go 1.21+)

### Build System
- **Task Runner**: `just` (justfile) - Modern alternative to make
- **Commands**: `just <target>` (see justfile for available commands)

## Code Organization

```
org-lsp/
├── cmd/server/main.go          # Entry point, minimal
├── server/                     # LSP server implementation
│   ├── server.go              # Core handler logic
│   └── integration_test.go    # Integration tests (testify-based)
├── orgscanner/                # File scanning & indexing
│   ├── scanner.go            # File discovery
│   ├── parser.go             # Org parsing logic
│   ├── types.go              # Domain types
│   └── orgscanner.go         # Main processing
├── SPEC.md                   # Feature specification
├── AGENTS.md                # This file
└── justfile                 # Build automation
```

### Package Boundaries
- **server/**: LSP protocol handling, request routing, handler logic
- **orgscanner/**: Pure domain logic - parsing, indexing, no LSP awareness
- **cmd/server/**: CLI entry point only

## Key Architectural Patterns

### State Management
Server state is a global singleton accessed via `serverState` pointer, initialized in `initialize()`:
```go
type ServerState struct {
    OrgScanRoot     string
    ProcessedFiles  *orgscanner.ProcessedFiles
    OpenDocs        map[protocol.DocumentUri]*org.Document
    DocVersions     map[protocol.DocumentUri]int32
}
```

### Handler Pattern
All LSP handlers follow this structure:
1. Validate `serverState != nil`
2. Extract document from `OpenDocs`
3. Perform operation
4. Return LSP-formatted response

Link resolution is refactored to separate concerns:
- `resolveFileLink()` and `resolveIDLink()` return `org.Position` (domain types)
- `toProtocolLocation()` converts to LSP protocol types
- This allows both definition and hover to share resolution logic

### Path Resolution
**Critical**: Always resolve to absolute paths with proper expansion:
- Handle `~` expansion via `os.UserHomeDir()`
- Resolve environment variables via `os.ExpandEnv()`
- Convert relative paths relative to current document or workspace root
- Clean paths with `filepath.Clean()`

## Testing Guidelines

### Testify Usage
Always use testify assertions for clearer test output:
```go
// ❌ Avoid
if result == nil {
    t.Error("Expected non-nil")
}

// ✅ Use
require.NotNil(t, result, "Expected non-nil result")
assert.Equal(t, expected, actual, "Values should match")
```

**Convention**:
- Use `require` for conditions that should stop the test
- Use `assert` for non-critical checks that can continue

### Test Structure
Integration tests use real TCP connections:
```go
// Create server
srv := ourserver.New()
go srv.RunTCP(addr)

// Connect with retry
conn, err := net.DialTimeout("tcp", addr, timeout)

// JSON-RPC communication
jsonrpcConn := jsonrpc2.NewConn(...)
```

### Test Helpers in justfile
```bash
just test                    # Run all tests (with INFO logs)
just test-quiet             # Run tests (ERROR logs only)
just test <pattern>         # Run specific test pattern
just test-quiet <pattern>   # Run specific test quietly
```

## Common Pitfalls

### 1. Org-Mode Formatting
**CRITICAL**: Org headings MUST NOT have leading whitespace:
```go
// ❌ WRONG - Has tabs before *
targetContent := `Foo

	* Target Heading    // tabs before star - org parser ignores this!
`

// ✅ CORRECT - No leading whitespace
targetContent := `Foo

* Target Heading      // star at column 0 - correct!
`
```

This commonly happens with backtick string literals that preserve code indentation. Always ensure headings start at column 0.

### 2. Path Handling
- Never assume relative paths are relative to CWD
- Always convert to absolute paths before file I/O
- Use `filepath.Join()` for cross-platform compatibility
- Remember: orgscanner stores paths relative to `OrgScanRoot`

### 3. Type Assertions
Protocol types often use `any` interface - always type assert with context:
```go
// ✅ Good
markupContent, ok := result.Contents.(protocol.MarkupContent)
require.True(t, ok, "Expected Contents to be MarkupContent, got %T", result.Contents)

// ❌ Bad (no context on failure)
content := result.Contents.(protocol.MarkupContent) // Will panic on failure
```

### 4. Helix Client-Side Completion Filtering

**CRITICAL**: Helix performs its own client-side filtering on completion items based on the `Label` field. The text before the cursor must be a prefix of the `Label`, otherwise Helix will silently filter out the completion.

```go
// ❌ WRONG - Label doesn't match what user typed
// User typed: #+begin_s
// Label is just "src"
// Result: Helix filters it out because "src" doesn't match "#+begin_s"
item := protocol.CompletionItem{
    Label:  "src",
    Kind:   ptrTo(protocol.CompletionItemKindKeyword),
    Detail: strPtr("Block type"),
}

// ✅ CORRECT - Label includes full prefix
// User typed: #+begin_s
// Label is "#+begin_src"
// Result: Helix shows it because "#+begin_src" starts with "#+begin_s"
item := protocol.CompletionItem{
    Label:  "#+begin_src",
    Kind:   ptrTo(protocol.CompletionItemKindKeyword),
    Detail: strPtr("Block type"),
}
```

**Additionally**: Use `TextEdit` with a calculated range instead of `InsertText` to prevent duplication. When the user has typed `#+begin_s` and selects the completion, the server must replace the entire `#+begin_s` prefix, not just insert at the cursor position.

## Logging Conventions

### Log Levels
Controlled via `ORG_LSP_LOG_LEVEL` environment variable:
- `DEBUG`: Detailed link parsing, AST walking
- `INFO`: Client connect/disconnect, file scans (default)
- `WARN`: Missing files, invalid UUIDs, parse warnings
- `ERROR`: Failures, panics

### Log Format
Use structured logging with key-value pairs:
```go
// ✅ Good
slog.Info("Starting org file scan", "root", root)

// ❌ Bad
slog.Info(fmt.Sprintf("Starting scan: %s", root))
```

## Development Workflow

### 1. Making Changes

**AGENTS MUST USE JUST**: Always use `just` commands for building and testing. Never run `go build` or `go test` directly, as `just` ensures proper module resolution and build flags.

```bash
# Make changes
zed server/server.go

# Build (always via just)
just build

# Format code
just fmt

# Run quick tests
just test-quiet HoverFileLink

# Run full test suite
just test

# Check coverage
just test-coverage
```

### 2. Adding Features
1. Update SPEC.md with feature specification
2. Add tests first (testify-based)
3. Implement feature following patterns above
4. Ensure proper error handling and logging
5. Run full test suite locally

### 3. Debugging Tests
Set log level to see what's happening:
```bash
ORG_LSP_LOG_LEVEL=DEBUG just test TestServerLifecycle/HoverFileLink
```

## Type Safety Principles

### Prefer Domain Types
- Use `orgscanner.HeaderLocation` internally
- Convert to LSP types only at handler boundaries
- This ensures compiler catches type mismatches

### Explicit vs. Implicit
Be explicit about types at boundaries:
```go
// Handler signature - keep it clean
func textDocumentDefinition(...) (any, error)

// But internally use strong types
location := protocol.Location{...} // Not map[string]interface{}
```

## Common Tasks Reference

### Adding a New LSP Handler
1. Add method to `New()` in server.go
2. Implement handler following existing patterns
3. Add capability in `initialize()`
4. Write integration test in integration_test.go

### Modifying Link Resolution
1. Update `resolveFileLink()` or `resolveIDLink()`
2. Ensure they return `orgscanner` domain types
3. Update `toProtocolLocation()` and/or `toProtocolRange()` if format changes
4. Tests in both definition and hover contexts

### Debugging Hover/Definition
1. Check log output: `ORG_LSP_LOG_LEVEL=INFO just test`
2. Verify org scanner finds the target (check uuid-indexed count)
3. Check `findLinkAtPosition` actually finds the link node
4. Verify path resolution produces correct absolute path

## Key Files to Understand
- **server/server.go**: Request routing and handler logic
- **server/integration_test.go**: Test patterns to follow
- **orgscanner/parser.go**: How org files are parsed and indexed
- **SPEC.md**: Feature specifications and architecture diagrams

## Performance Notes
- orgscanner re-parses on file save (blocking operation)
- Hover extracts context lines via `os.ReadFile()` - consider caching for large files
- UUID index uses `sync.Map` for concurrent access
- Document parsing happens on open/change/save

--- 

**Last Updated**: 2024-01-27
**Maintainer**: @alexispurslane
