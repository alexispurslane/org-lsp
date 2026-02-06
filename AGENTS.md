# Agent Development Guidelines for org-lsp

**⚠️ CRITICAL FOR AGENTS**: Always use `just` for building and testing (`just build`, `just test`). Never run `go build`/`go test` directly - `just` ensures proper module resolution.

This document serves as a knowledge base for AI agents and developers working on org-lsp. It captures architectural decisions, coding patterns, and common pitfalls to ensure consistent development.

## Project Overview

org-lsp is a minimal LSP server for org-mode files focused on navigation and linking capabilities. It uses `go.lsp.dev` for LSP protocol handling and a custom orgscanner package for parsing and indexing.

## Technology Stack

### Core Dependencies
- **LSP Framework**: `go.lsp.dev/protocol` - LSP protocol types and server framework
- **Org Parser**: `github.com/alexispurslane/go-org` (HEAD) - Org-mode AST parsing (fork of `niklasfasching` version)
- **Testing**: `github.com/MarvinJWendt/testza` v0.5.2 - Modern assertions with colored diffs
- **Logging**: `log/slog` - Structured logging (Go 1.21+)

### Build System
- **Task Runner**: `just` (justfile) - Modern alternative to make
- **Test Infrastructure**: `cmd/testcolor` for colorized test output highlighting
- **Commands**: `just <target>` (see justfile for available commands)

**Available Commands:**
- `just build` - Build the server binary
- `just install` - Install org-lsp to ~/.local/bin
- `just test <pattern>` - Run tests with colored output (INFO logs)
- `just test-quiet <pattern>` - Run tests quietly (ERROR logs only)
- `just fmt` - Format code with go fmt
- `just lint` - Run golangci-lint
- `just pre-commit` - Run fmt, lint, and test before committing
- `just watch-dev` - Watch for changes and rebuild (requires entr)
- `just init` - Initialize development environment

## Code Organization

```
org-lsp/
├── cmd/
│   ├── server/main.go        # LSP server entry point
│   └── testcolor/main.go     # Test output colorizer utility
├── server/                   # LSP server implementation (modular handlers)
│   ├── server.go             # Core server initialization & lifecycle
│   ├── <feature>.go          # LSP handler and any business logic (AST walking/transformation, usually) for the given <feature>
├── integration/              # Feature-specific integration tests
│   ├── lsp_test_context.go   # LSPTestContext and helpers
│   ├── gherkin.go            # Given/When/Then helpers
│   ├── test_helpers.go       # Shared test utilities
│   ├── <feature>_test.go     # BDD test specification for the given <feature>
├── orgscanner/               # File scanning & indexing
│   ├── scanner.go            # File discovery and scanning
│   ├── indexer.go            # Index building and management
│   ├── parser.go             # Org parsing logic
│   └── types.go              # Domain types (FileInfo, HeaderLocation, etc.)
├── lspstream/                # LSP message streaming
│   └── stream.go             # LargeBufferStream implementation
├── justfile                  # Build automation and run targets
├── ARCHITECTURE.md           # High-level architecture overview
├── AGENTS.md                 # This file - development guidelines
└── README.md                 # Project documentation for users
```

### Package Boundaries
- **server/**: LSP protocol handling, request routing, modular feature handlers.
- **orgscanner/**: Pure domain logic - parsing, indexing, no LSP awareness. Uses `sync.Map` for concurrent access.
- **cmd/server/**: CLI entry point only - minimal initialization.
- **cmd/testcolor/**: Developer utility for colored test output.
- **integration/**: Feature-specific BDD-style integration tests, isolated per feature. Each `_test.go` file tests its corresponding handler file.

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
Server handlers are organized by feature in separate files:
- `codeactions.go`: Text document/code action handlers  
- `completions.go`: Text document/completion handlers
- `definitions.go`: Text document/definition handlers
- `folding.go`: Text document/folding range handlers
- `formatting.go`: Text document/formatting handlers
- `symbols.go`: Document and workspace symbol handlers

All handlers follow this structure:
1. Validate `serverState != nil`
2. Extract document from `OpenDocs`
3. Perform operation
4. Return LSP-formatted response

Link resolution is shared between handlers:
- `resolveFileLink()` and `resolveIDLink()` in `utils.go` return `org.Position` (domain types)
- `toProtocolLocation()` converts to LSP protocol types
- This allows definition, hover, and references to share resolution logic

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
Integration tests are organized by feature and use `LSPTestContext` for isolated test environments:

- `definition_test.go`: Go-to-definition tests
- `hover_test.go`: Hover information tests
- `completion_test.go`: Code completion tests
- `symbols_test.go`: Document and workspace symbol tests
- `folding_test.go`: Text folding range tests
- `formatting_test.go`: Document formatting tests
- `references_test.go`: Find references tests
- `codeaction_test.go`: Available code action tests

Each test file follows this pattern:

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

**Helper Functions (in test_helpers.go):**
- `ptrTo[T any](v T) *T` - Generic pointer creation helper
- `strPtr(s string) *string` - String pointer helper

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

Output appears as (when run with testcolor):
```
=== RUN   TestName/given_context
=== RUN   TestName/given_context/when_action
=== RUN   TestName/given_context/when_action/then_expected_result
```

The `cmd/testcolor` utility provides colorized output highlighting:
- Test names in cyan
- `given_`/`Given_` paths in blue
- `when_`/`When_` paths in magenta  
- `then_`/`Then_` paths in yellow
- PASS results in green, FAIL in red

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
# Make changes (edit handler files as needed)
zed server/definitions.go
zed integration/definition_test.go

# Build (always via just)
just build

# Format code
just fmt

# Run quick tests with colored output
just test-quiet TestDefinition

# Run full test suite with colored output
just test

# Run development workflow (watch mode)
just watch-dev
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
1. Add types with doc comments (technical spec) to `types.go`
2. Add handler method to appropriate handler file (e.g., `definitions.go`)
3. Add any helper functions to `utils.go` if shared by multiple handlers
4. Update capabilities in `initialize()` method in `server.go`
5. Run tests: `just test-quiet TestFeatureName`
6. Debug with: `ORG_LSP_LOG_LEVEL=DEBUG just test TestFeatureName`

#### Step 5: Verify & Document

- Run full test suite: `just test`
- **Update doc comments if implementation diverged from spec**
- The code now contains both executable behavior spec (tests) and technical spec (types + comments)

**Key Principle:** The only "spec documents" are:
- `integration/*_test.go` - executable behavior specs
- Go doc comments in the code - technical specs
- `ARCHITECTURE.md` - high-level navigation overview

No separate SPEC.md needed - the spec lives in the code and is always up-to-date!

### 3. Debugging Tests

Run with DEBUG logs and colored output:
```bash
# Run specific test with DEBUG logs
ORG_LSP_LOG_LEVEL=DEBUG just test TestDefinition

# Run all tests for a feature
just test-quiet TestDefinition
```

The test output includes colorized highlighting:
- Test names in cyan
- Gherkin steps in different colors (blue/magenta/yellow)
- PASS/FAIL results in green/red
- Summary tally at the end


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
1. Create new handler file: `server/newfeature.go` (if doesn't exist)
2. Add handler method implementing the LSP protocol interface
3. Add capability in `initialize()` method in `server.go`
4. Create feature-specific test file: `integration/newfeature_test.go`:
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

**Modular Organization:** Each LSP feature has its own test file and handler file for clear separation of concerns.

### Writing Integration Tests
1. Create test in appropriate feature file (`integration/definition_test.go`, etc.)
2. Use `Given/When/Then` structure for readability
3. Use chainable `Given*` helpers for setup
4. Use `When[T]` with appropriate type parameter for LSP calls
5. Use testza assertions for clear failure messages with colored diffs
6. Run with `just test-quiet TestName` for quick feedback

### Modifying Link Resolution
1. Update `resolveFileLink()` or `resolveIDLink()` in `server/utils.go`
2. Ensure they return `orgscanner` domain types
3. Update `toProtocolLocation()` if format changes
4. Run `just test Definition Hover` to verify both handler tests pass

### Debugging Tests
1. Run specific test: `ORG_LSP_LOG_LEVEL=DEBUG just test TestName`
2. Check server logs for handler execution
3. Verify LSPTestContext setup (files created, documents opened)
4. Check response types match expected Go types

## Key Files to Understand
- **server/server.go**: Core server initialization, lifecycle, and capabilities setup
- **server/utils.go**: Shared utilities including link resolution (resolveFileLink, resolveIDLink)
- **server/definitions.go**, **server/completions.go**, etc.: Feature-specific handler implementations
- **integration/lsp_test_context.go**: LSPTestContext implementation and When[T] function
- **integration/test_helpers.go**: Shared test utilities and helper functions
- **integration/*_test.go**: Feature-specific integration tests (definition_test.go, hover_test.go, etc.)
- **orgscanner/parser.go**: How org files are parsed and indexed
- **orgscanner/scanner.go**: File discovery and workspace scanning logic
- **ARCHITECTURE.md**: High-level architecture overview and data flow diagrams
- **cmd/testcolor/main.go**: Test output colorizer for enhanced development experience

## Performance Notes
- orgscanner re-parses on file save (blocking operation)
- Hover extracts context lines via `os.ReadFile()` - consider caching for large files
- UUID index uses `sync.Map` for concurrent access during parsing
- Files map and TagMap are regular Go maps protected by scanner mutex (O(1) lookups)
- Document parsing happens on open/change/save

## Modern Go Practices

### Generic Functions (Go 1.18+)

Prefer generic functions from `slices` and `maps` packages over manual loops or reflection:

```go
import "slices"

// ❌ Old way - manual loop
func contains(slice []string, item string) bool {
    for _, s := range slice {
        if s == item {
            return true
        }
    }
    return false
}

// ✅ Modern way - use slices package
if slices.Contains(mySlice, "item") {
    // ...
}

// Other useful generic functions:
slices.Index(slice, item)        // Find index
slices.Equal(a, b)               // Compare slices
slices.Sort(slice)               // Sort in place
slices.Compact(slice)            // Remove consecutive duplicates
slices.Delete(slice, i, j)       // Delete range
```

```go
import "maps"

// ❌ Old way - manual copy
copied := make(map[string]int)
for k, v := range original {
    copied[k] = v
}

// ✅ Modern way - use maps package
copied := maps.Clone(original)

// Other useful functions:
maps.Equal(a, b)                 // Compare maps
maps.Keys(m)                     // Get all keys
maps.Values(m)                   // Get all values
maps.Copy(dst, src)              // Copy all entries
```

For filtering and transforming slices, use `slices.DeleteFunc` and helper functions:

```go
// Filter slice in place
nodes = slices.DeleteFunc(nodes, func(n org.Node) bool {
    p, ok := n.(org.Paragraph)
    return ok && len(p.Children) == 0
})
```

### Error Handling with errors.As (Go 1.13+)

Use structured error types and `errors.As` for type-safe error inspection instead of string matching:

```go
// Define structured error types
type NotFoundError struct {
    Resource string
    ID       string
}

func (e NotFoundError) Error() string {
    return fmt.Sprintf("%s not found: %s", e.Resource, e.ID)
}

// Return typed errors
func findHeading(id string) (*Heading, error) {
    // ...
    return nil, NotFoundError{Resource: "heading", ID: id}
}

// Check error type with errors.As
var notFound NotFoundError
if errors.As(err, &notFound) {
    // Handle not found specifically
    log.Printf("Resource missing: %s", notFound.Resource)
}
```

Checking multiple error types:

```go
var connErr *net.OpError
var dnsErr *net.DNSError

if errors.As(err, &connErr) {
    fmt.Println("Network operation failed:", connErr.Op)
} else if errors.As(err, &dnsErr) {
    fmt.Println("DNS resolution failed:", dnsErr.Name)
}
```

For simple error wrapping, use `fmt.Errorf` with `%w`:

```go
if err != nil {
    return fmt.Errorf("processing document %s: %w", uri, err)
}
```

### Advanced Testza Assertions

Use semantically meaningful assertions from testza for clearer test output over
generic ones like `AssertEqual` and `AssertTrue` (unless those really are the
ones that fit best). Run `go doc github.com/MarvinJWendt/testza | grep Assert`
to see all available assertions. 

```go
// ❌ Generic assertions - less helpful error messages
testza.AssertEqual(t, len(items), 3, "Expected 3 items")
testza.AssertEqual(t, result > 0, true, "Expected positive result")
testza.AssertEqual(t, strings.Contains(s, "prefix"), true, "Expected prefix")

// ✅ Semantically meaningful assertions - better error messages
testza.AssertLen(t, items, 3, "Expected 3 items")
testza.AssertGreater(t, result, 0, "Expected positive result")
testza.AssertContains(t, s, "prefix", "Expected prefix in string")
```

```go
// Nil/Empty checks
testza.AssertNil(t, obj, "Expected nil")
testza.AssertNotNil(t, obj, "Expected non-nil")
testza.AssertZero(t, n, "Expected zero value")

// Collection assertions
testza.AssertLen(t, slice, 5, "Expected 5 elements")
testza.AssertContains(t, slice, item, "Expected item in slice")
testza.AssertNotContains(t, slice, item, "Item should not be in slice")
testza.AssertSameElements(t, expected, actual, "Collections should have same elements")
testza.AssertSubset(t, list, subset, "Expected subset")
testza.AssertUnique(t, list, "Expected unique elements")

// String/c assertions
testza.AssertContains(t, str, substring, "Expected substring")
testza.AssertNotContains(t, str, substring, "Substring should not be present")
testza.AssertRegexp(t, regex, txt, "Expected regexp match")

// Numeric comparisons
testza.AssertGreater(t, a, b, "Expected a > b")
testza.AssertLess(t, a, b, "Expected a < b")
testza.AssertGreaterOrEqual(t, a, b, "Expected a >= b")
testza.AssertLessOrEqual(t, a, b, "Expected a <= b")
testza.AssertInRange(t, n, min, max, "Expected value in range")
testza.AssertNumeric(t, obj, "Expected numeric type")

// Type assertions
testza.AssertImplements(t, (*io.Reader)(nil), obj, "Should implement io.Reader")
testza.AssertKindOf(t, reflect.Slice, value, "Expected slice kind")
testza.AssertNil(t, value, "Expected nil interface")

// Error handling
testza.AssertNoError(t, err, "Expected no error")
testza.AssertErrorIs(t, err, targetErr, "Expected specific error type")

// File system
testza.AssertFileExists(t, path, "File should exist")
testza.AssertNoFileExists(t, path, "File should not exist")
testza.AssertDirExists(t, path, "Directory should exist")
testza.AssertDirNotEmpty(t, path, "Directory should not be empty")

// Behavior/performance
testza.AssertPanics(t, f, "Expected panic")
testza.AssertNotPanics(t, f, "Should not panic")
testza.AssertCompletesIn(t, 100*time.Millisecond, f, "Should complete quickly")

// Snapshots - useful for complex outputs
testza.SnapshotCreateOrValidate(t, "snapshot-name", result, "Should match snapshot")

// Custom complex conditions
testza.Assert(t, func() bool {
    for _, item := range items {
        if item.ID == expectedID {
            return true
        }
    }
    return false
}, "Expected to find item with ID %s", expectedID)
```

### Prefer Compile-Time Type Safety

Use generics and interfaces instead of reflection where possible:

```go
// ❌ Reflection - runtime errors, slower
func getChildren(n org.Node) []org.Node {
    v := reflect.ValueOf(n)
    field := v.FieldByName("Children")
    return field.Interface().([]org.Node)
}

// ✅ Generics - compile-time safety, faster
func filterNodes[T org.Node](nodes []org.Node, filter func(T) bool) []org.Node {
    result := make([]org.Node, 0)
    for _, n := range nodes {
        if t, ok := n.(T); ok && filter(t) {
            result = append(result, n)
        }
    }
    return result
}
```

When reflection is necessary (e.g., for AST traversal), use `reflect.TypeFor` (Go 1.22+) for type-safe reflection:

```go
// ❌ Old way - awkward syntax for getting interface types
errorType := reflect.TypeOf((*error)(nil)).Elem()
readerType := reflect.TypeOf((*io.Reader)(nil)).Elem()

// ✅ Modern way with reflect.TypeFor - clean and readable
errorType := reflect.TypeFor[error]()
readerType := reflect.TypeFor[io.Reader]()
```

`reflect.TypeFor` is especially useful when you need the reflect.Type of an interface, not a concrete type. Before Go 1.22, you had to use the awkward `reflect.TypeOf((*Interface)(nil)).Elem()` pattern.

Isolate reflection code and document why it's necessary:

```go
// formatChildren uses reflection to handle any node type with Children.
// This is necessary because org.Node is an interface with many implementations.
func formatChildren(n org.Node) org.Node {
    // reflection logic here...
}
```
