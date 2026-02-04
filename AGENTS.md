# Agent Development Guidelines for org-lsp

**⚠️ CRITICAL FOR AGENTS**: Always use `just` for building and testing (`just build`, `just test`). Never run `go build`/`go test` directly - `just` ensures proper module resolution.

This document serves as a knowledge base for AI agents and developers working on org-lsp. It captures architectural decisions, coding patterns, and common pitfalls to ensure consistent development.

## Project Overview

org-lsp is a minimal LSP server for org-mode files focused on navigation and linking capabilities. It uses glsp for LSP protocol handling and a custom orgscanner package for parsing and indexing.

## Technology Stack

### Core Dependencies
- **LSP Framework**: `go.lsp.dev/protocol` - LSP protocol types and server framework
- **Org Parser**: `github.com/alexispurslane/go-org` v1.9.1 - Org-mode AST parsing (fork of `niklasfasching` version)
- **Testing**: `github.com/MarvinJWendt/testza` v0.5.2 - Modern assertions with colored diffs
- **Logging**: `log/slog` - Structured logging (Go 1.21+)

### Build System
- **Task Runner**: `just` (justfile) - Modern alternative to make
- **Commands**: `just <target>` (see justfile for available commands)

## Code Organization

```
org-lsp/
├── cmd/server/main.go          # Entry point, minimal
├── server/                     # LSP server implementation
│   └── server.go              # Core handler logic
├── integration/               # Integration tests (NEW - testza-based)
│   ├── lsp_test_context.go   # LSPTestContext and helpers
│   ├── gherkin.go            # Given/When/Then helpers
│   └── lsp_test.go           # Integration tests
├── orgscanner/                # File scanning & indexing
│   ├── scanner.go            # File discovery
│   ├── parser.go             # Org parsing logic
│   ├── types.go              # Domain types
│   └── orgscanner.go         # Main processing
├── lspstream/                 # LSP message streaming
│   └── stream.go             # LargeBufferStream implementation
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
    OrgScanRoot  string
    Scanner      *orgscanner.OrgScanner  // Incremental org file scanner
    OpenDocs     map[protocol.DocumentUri]*org.Document
    DocVersions  map[protocol.DocumentUri]int32
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

### Testza Usage
Always use testza assertions for clearer test output with colored diffs:
```go
// ❌ Avoid
if result == nil {
    t.Error("Expected non-nil")
}

// ✅ Use
testza.AssertNotNil(t, result, "Expected non-nil result")
testza.AssertEqual(t, expected, actual, "Values should match")
```

### LSPTestContext Pattern
Integration tests use `LSPTestContext` for isolated test environments:

```go
func TestFileLinkDefinition(t *testing.T) {
    Given("source and target files", t,
        func(t *testing.T) *LSPTestContext {
            tc := NewTestContext(t)
            tc.GivenFile("target.org", "* Target\nContent").
               GivenFile("source.org", "* Source\n[[file:target.org][link]]").
               GivenOpenFile("source.org")
            return tc
        },
        func(t *testing.T, tc *LSPTestContext) {
            params := protocol.DefinitionParams{...}
            
            When(t, tc, "requesting definition", "textDocument/definition", params, 
                func(t *testing.T, locs []protocol.Location) {
                    Then("returns target location", t, func(t *testing.T) {
                        testza.AssertEqual(t, 1, len(locs))
                        testza.AssertTrue(t, strings.Contains(string(locs[0].URI), "target.org"))
                    })
                })
        },
    )
}
```

**Key Features:**
- Each test gets its own temp directory and server instance
- `Given*` helpers are chainable for concise setup
- `When[T]` handles LSP calls with type-safe results
- Tests run in parallel by default (via `t.Parallel()` in `Given`)

### LSPTestContext Methods

**Setup (chainable):**
- `GivenFile(path, content)` - Creates a file in temp directory
- `GivenOpenFile(uri)` - Sends `textDocument/didOpen` notification
- `GivenSaveFile(uri)` - Sends `textDocument/didSave` notification

**Execution:**
- `When[T](t, tc, description, method, params, handler)` - Makes LSP call and passes result to handler

**Cleanup:**
- `Shutdown()` - Automatically called via `defer` in `Given`

### Gherkin Helpers
Structure tests with Given/When/Then for readable output:
```go
Given("context", t, setupFunc, testFunc)
When(t, tc, "action", method, params, handler)  // Included in lsp_test_context.go
Then("expected result", t, assertionFunc)
```

Output appears as:
```
=== RUN   TestName/given_context
=== RUN   TestName/given_context/when_action
=== RUN   TestName/given_context/when_action/then_expected_result
```

### Test Helpers in justfile
```bash
just test                    # Run all tests (with INFO logs, race detector ON)
just test-quiet             # Run tests (ERROR logs only, race detector ON)
just test <pattern>         # Run specific test pattern (race detector ON)
just test-quiet <pattern>   # Run specific test quietly (race detector ON)
```

**Important:** Always use `just` for testing, never `go test` directly.


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

### 5. Documentation-fetching

LSP Protocol types are in a versioned subdirectory (`protocol_3_16`). Use `go doc github.com/tliron/glsp/protocol_3_16 <TypeName>` to inspect types (e.g., `go doc github.com/tliron/glsp/protocol_3_16 WorkspaceSymbolFunc`).

### 6. Always use pathToURI when passing paths to an LSP client

All paths sent to an LSP client must be *absolute* URIs; for Helix at least, the
response won't even parse if they aren't, since Helix uses Rust's type system to
exactly enforce the proper LSP spec. But even if that wasn't the case, paths
wouldn't work right without being absolute!

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

### 2. Adding Features (Type-First BDD Workflow)

When the user proposes a new feature, follow this workflow where **executable tests** specify behavior and **types + doc comments** specify technical design:

#### Step 1: Agree on Behavior

Write natural language Gherkin scenarios and iterate with the user:

```gherkin
Given a source file with a file link
  And a target file exists
  When I request definition at the link position
    Then it should return the target file location
```

#### Step 2: Executable Specification

Translate to BDD tests in `integration/<featurename>_test.go` - this is the **living behavioral spec**:

```go
func TestFileLinkDefinition(t *testing.T) {
    Given("a source file with a file link", t, setupFunc, func(t *testing.T, tc *LSPTestContext) {
        When(t, tc, "requesting definition at link position", "textDocument/definition", params,
            func(t *testing.T, locs []protocol.Location) {
                Then("returns target file location", t, func(t *testing.T) {
                    testza.AssertEqual(t, 1, len(locs))
                })
            })
    })
}
```

Each top level Test* function should correspond to the collection of Gherkin "Given" scenarios that describe a total feature. Put each such function in its own file.

Run the test to confirm it fails (red): `just test TestFileLinkDefinition`

**Thread-Safety Verification Note:** All `just test*` commands run with:
- `-race` flag enabled (detects data races at runtime)
- `-parallel=4` (limited parallelism to catch concurrency bugs)
- `-timeout=60s` (catches deadlocks/hangs)

This means your tests automatically verify thread-safety! If you introduce a data race or deadlock, the tests will fail with a detailed report. Keep tests deterministic and avoid shared mutable state between parallel tests.

#### Step 3: Technical Specification (Type-First)

Define data types and function signatures with doc comments. The **types are the spec**, the documentation comments define specific algorithms and semantics:

```go
// IDLinkResolver finds target headings by UUID property.
// It searches the UUID index built by orgscanner during file scanning.
type IDLinkResolver struct {
    scanner *orgscanner.OrgScanner
}

// Resolve returns the location of the heading with the given ID.
// Returns nil if no heading with that ID exists.
// 
// Algorithm: look up the HeaderLocation on scanner.ProcessedFiles.UuidIndex map.
func (r *IDLinkResolver) Resolve(id string) *orgscanner.HeaderLocation
```

View generated docs: `go doc github.com/alexispurslane/org-lsp/orgscanner IDLinkResolver`

#### Step 4: Implement

Now implement to make tests pass:
1. Add types with doc comments (technical spec)
2. Add handler method to server.go
3. Update capabilities in initialize()
4. Run tests: `just test TestFeatureName`
5. Debug with: `ORG_LSP_LOG_LEVEL=DEBUG just test TestFeatureName`

#### Step 5: Verify & Document

- Run full test suite: `just test`
- **Update doc comments if implementation diverged from spec**
- The code now contains both executable behavior spec (tests) and technical spec (types + comments)

**Key Principle:** The only "spec documents" are:
- `integration/*_test.go` - executable behavior specs
- Go doc comments in the code - technical specs
- `ARCHITECTURE.md` - high-level navigation (optional)

No separate SPEC.md needed - the spec lives in the code and is always up-to-date!

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
1. Add method to server.go implementing the handler
2. Add capability in `initialize()`
3. Write integration test in `integration/lsp_test.go`:
   ```go
   func TestNewFeature(t *testing.T) {
       Given("setup context", t, setupFunc, func(t *testing.T, tc *LSPTestContext) {
           params := protocol.NewFeatureParams{...}
           When(t, tc, "using feature", "textDocument/newFeature", params,
               func(t *testing.T, result ExpectedType) {
                   Then("returns expected result", t, func(t *testing.T) {
                       testza.AssertEqual(t, expected, result)
                   })
               })
       })
   }
   ```

### Writing Integration Tests
1. Create test in `integration/lsp_test.go`
2. Use `Given/When/Then` structure for readability
3. Use chainable `Given*` helpers for setup
4. Use `When[T]` with appropriate type parameter for LSP calls
5. Use testza assertions for clear failure messages
6. Run with `just test TestName`

### Modifying Link Resolution
1. Update `resolveFileLink()` or `resolveIDLink()` in server.go
2. Ensure they return `orgscanner` domain types
3. Update `toProtocolLocation()` if format changes
4. Run `just test` to verify both definition and hover tests pass

### Debugging Tests
1. Run specific test: `ORG_LSP_LOG_LEVEL=DEBUG just test TestName`
2. Check server logs for handler execution
3. Verify LSPTestContext setup (files created, documents opened)
4. Check response types match expected Go types

## Key Files to Understand
- **server/server.go**: Request routing and handler logic
- **integration/lsp_test_context.go**: LSPTestContext implementation and When[T] function
- **integration/lsp_test.go**: Example tests using the new framework
- **orgscanner/parser.go**: How org files are parsed and indexed
- **SPEC.md**: Feature specifications and architecture diagrams

## Performance Notes
- orgscanner re-parses on file save (blocking operation)
- Hover extracts context lines via `os.ReadFile()` - consider caching for large files
- UUID index uses `sync.Map` for concurrent access during parsing
- Files map and TagMap are regular Go maps protected by scanner mutex (O(1) lookups)
- Document parsing happens on open/change/save
