# org-lsp: Language Server Protocol Implementation for Org-Mode

## Executive Summary

org-lsp is a minimal LSP server for org-mode files focused on navigation and linking capabilities. It leverages the existing `orgscanner` package for parsing and indexing org-mode content, providing intelligent features like go-to-definition, backlinks, hover previews, and ID-link autocompletion.

## Core Goals (MVP)

1. **Simplest working LSP server** - Minimum viable product
2. **Go-to-definition** for `file:` links and `id:` links via UUID index
3. **Backlinks** - Show where current header's UUID is referenced in `id:` links
4. **Hover previews** - For links, show file previews from orgscanner
5. **ID-link autocompletion** - Complete ID: references only
6. **Tag completion** - Complete `:tags:` based on orgscanner tag index

## Architecture

```
┌─────────────────┐
│   LSP Client    │ (Emacs, Zed, etc.)
└────────┬────────┘
         │ stdio/JSON-RPC 2.0
         ↓
┌─────────────────────────────┐
│   glsp Server (protocol)    │ ← github.com/tliron/glsp
└────────┬────────────────────┘
         │
         ↓
┌─────────────────────────────┐
│   Server Logic (handlers)   │ ← LSP request handlers
└────────┬────────────────────┘
         │
         ↓
┌─────────────────────────────┐
│   orgscanner Integration    │ ← org parsing & indexing
│  - ProcessedFiles           │
│  - UuidIndex                │
│  - File metadata            │
└─────────────────────────────┘
```

## Data Model

### LSP Protocol Types

```go
type Position struct {
    Line      uint32    // 0-based line number
    Character uint32    // 0-based character offset
}

type Range struct {
    Start Position
    End   Position
}

type Location struct {
    URI   DocumentUri  // file:///path/to/file.org
    Range Range
}

type DocumentUri string
```

### OrgScanner to LSP Mapping

| OrgScanner Type | LSP Type | Purpose |
|-----------------|----------|---------|
| `org.Position` | `Position` | Direct mapping (both use 0-based coordinates) |
| `HeaderLocation` | `Location` | Full URI + range for definitions |
| `UUID` | string | Used for ID-link resolution and backlinks |
| `FileInfo.Preview` | `Hover` | Content preview on link hover |
| `FileInfo.Tags` | `CompletionItem` | Tag-based tag suggestions |
| `ProcessedFiles.TagMap` | `CompletionItem` | Global tag index |

### Server State

```go
type ServerState struct {
    RootDir     string                       // Workspace root path
    Processed   *ProcessedFiles              // From orgscanner.Process()
    OpenDocs    map[string]*org.Document     // Currently open documents
    DocVersions map[string]int32             // Document version tracking
}
```

### Node Finding Architecture

The `findNodeAtPosition` function uses depth-aware traversal to locate the most specific AST node at a given cursor position:

Key characteristics:
1. Essentially uses recursive tree structured spatial partitioning to find the node under the cursor
2. Prefers deeper/more specific nodes over ancestors: if a node that might work
   is found, we still recursively search it for anything on a deeper level that
   might also work (helps for nested headings)
3. Inline vs Block detection: the start and end columns of a block node are on
   different lines, so it doesn't really make sense to apply them to collision
   detection, since e.g. the start and end col could both be 0; but it does make
   sense for links and such. So inline and block nodes have different collision
   detection.

This allows accurate link detection even within complex nested structures.


## LSP Capabilities

### Required Capabilities (MVP)

```go
ServerCapabilities{
    TextDocumentSync: &TextDocumentSyncOptions{
        OpenClose: true,
        Change:    TextDocumentSyncKindFull,
        Save: &SaveOptions{
            IncludeText: true,
        },
    },
    DefinitionProvider:         true,   // File links, ID links
    ReferencesProvider:         true,   // Backlinks
    HoverProvider:              true,   // Link previews
    CompletionProvider: &CompletionOptions{
            TriggerCharacters: []string{
	            ":",  // Trigger on "id:" and ":tags:"
	            "_", // Trigger on begin_ and export_
            },
        },
}
```

### Capabilities Not in MVP

- CodeLensProvider
- FormattingProvider
- DiagnosticsProvider
- CallHierarchyProvider
- TypeHierarchyProvider
- All other advanced features

### Phase 7 Capabilities (Advanced Features)

```go
ServerCapabilities{
    // ... existing capabilities ...
    DocumentSymbolProvider:     true,   // Outline view (headings & blocks)
    WorkspaceSymbolProvider:    true,   // Cross-file symbol search
    CodeActionProvider:         true,   // Heading/list conversion, code evaluation
}
```

## Feature Specifications

### 1. Go-to-Definition (`textDocument/definition`)

**Goal:** Navigate to linked content

**Input:** `DefinitionParams{`
  - TextDocumentPositionParams{ TextDocument, Position }
  `}`

**Logic:**
1. Parse current line to detect link syntax:
   - `[[file:filename.org][description]]`
   - `[[id:UUID][description]]`
   - `[[file:filename.org]]`
   - `[[id:UUID]]`
2. For `file:` links:
   - Resolve relative to current document
   - Return `Location{ URI: absolute_path, Range: entire_file }`
3. For `id:` links:
   - Extract UUID from link
   - Lookup in `processed.UuidIndex`
   - Return `Location{ URI: file_path, Range: headline_range }`

**Output:** `Location | []Location | []LocationLink | nil`

### 2. Go-to-References (`textDocument/references`)

**Goal:** Find all places referencing this header's UUID

**Input:** `ReferenceParams{`
  - TextDocumentPositionParams
  - Context{ IncludeDeclaration: bool }
  `}`

**Logic:**
1. Get current headline position from cursor
2. Look up UUID at this position (from parsed `ProcessedFiles`)
3. If no UUID at cursor, return nil
4. Search workspace for all files for links with a type of "id" and a destination of "uuid" by walking ServerState.ProcessedFiles.FileInfo.Files[].ParsedOrg (see how we resolve UUIDs in the html writer of https://github.com/alexispurslane/oxen)
5. Get the file location from that AST node
6. Combine the lists of locations from all the files into one big location list and return it

**Output:** `[]Location | nil`

**Optimization:** 
- Index ID links during scan (future enhancement)

### 3. Hover Provider (`textDocument/hover`)

**Goal:** Show preview of linked content

**Input:** `HoverParams{ TextDocumentPositionParams }`

**Logic:**
1. Check if cursor is over a link (`file:` or `id:`)
2. Resolve target using definition logic
3. For `file:` links: Return `FileInfo.Preview`
4. For `id:` links: Return `FileInfo.Preview` (MVP)
5. Format as `MarkupContent{ Kind: MarkupKindMarkdown, Value: preview }`

**Output:** `Hover{ Contents: MarkupContent, Range: link_range }`

**MVP Limitation:** ID-link hover shows entire file preview, not heading-specific content.

### 4. Completion Provider (`textDocument/completion`)

**Goal:** Autocomplete ID references and tags

**Input:** `CompletionParams{`
  - TextDocumentPositionParams
  - Context Context
  `}`

**Logic for ID completion:**
1. Check trigger context - only complete after `"id:"`
2. Scan cursor context: must match `id:` prefix
3. Iterate through `processed.UuidIndex`
4. For each UUID:
   - Get header location
   - Find FileInfo from ServerState.ProcessedFiles.FileInfo[] by just linearly searching for a file with Path == the HeaderLocation path
   - Grab the title from that FileInfo
   - Create `CompletionItem{`
     - Label: `UUID` (first 8 chars for brevity?)
     - Detail: title or file path
     - InsertText: `full-uuid`
     - Kind: CompletionItemKindReference
   `}`

**Logic for tag completion:**
1. Check trigger context - complete after `":"` in headline
2. Verify cursor position is in headline line (before content)
3. Scan cursor context: must match `:[a-zA-Z0-9_]*$` (partial tag)
4. Iterate through `processed.TagMap` keys
5. For each tag:
   - Check if it matches prefix (case-insensitive)
   - Create `CompletionItem{`
     - Label: tag
     - Detail: "Tag"
     - InsertText: `tag:]`
     - Kind: CompletionItemKindProperty
   `}`
6. Return merged completion list (IDs + tags)

**Tag Syntax in Org-Mode:**
```org
* Headline Title              :tag1:tag2:tag3:
** Sub Headline                :tag1:
* Single tag                   :onlytag:
```

Trigger patterns:
- Initial tag: `:`
- Continue tag: `:` within headline tags section
- Complete tag with closing `:`

**Output:** `[]CompletionItem | CompletionList | nil`

**MVP Limitation:** No filtering, shows all UUIDs/tags in workspace.

### 5. Document Sync (`textDocument/*`)

**Handlers:**
- `didOpen`: Parse file, store in `OpenDocs`, track version
- `didChange`: Parse updated content, update `OpenDocs`
- `didClose`: Remove from `OpenDocs`
- `didSave`: Re-parse file via orgscanner, update global index

**Note:** For MVP, document contents are synced but not written back to disk.

### 6. Enhanced References (`textDocument/references` - ID Links)

**Goal:** Find references to a UUID even when cursor is on the ID link itself (not just the heading)

**Input:** `ReferenceParams` (same as standard references)

**Logic Extension:**
1. Check if cursor is on an `org.RegularLink` before checking for headline
2. If link protocol is `id:`, extract UUID from link URL
3. Call existing `findIDReferences()` with that UUID
4. Falls back to existing headline UUID extraction if not on a link

**Implementation Notes:**
- Reuse `findNodeAtPosition[org.RegularLink]()` to detect link under cursor
- Reuse existing `findIDReferences()` function - no new search logic needed
- Add conditional before headline detection in references handler

**Output:** `[]Location | nil`

### 7. File Link Completion

**Goal:** Autocomplete file paths after `[[file:`

**Input:** `CompletionParams` with `[[file:` prefix before cursor

**Logic:**
1. Detect `[[file:` context (similar to `[[id:` detection)
2. Extract partial path already typed after `file:`
3. Search workspace files for matches:
   - Match against `ProcessedFiles.Files[].Path`
   - Filter by partial path prefix (case-insensitive)
4. Return `CompletionItem` for each match:
   - Label: filename or relative path
   - InsertText: relative path from current file
   - Kind: `CompletionItemKindFile`

**Trigger:** `[[file:` pattern detected in `detectCompletionContext()`

**Output:** `[]CompletionItem | CompletionList | nil`

### 8. Block Type Completion ✅

**Goal:** Autocomplete block types after `#+begin_`

**Block Types Supported:**
- `quote` - Quotation block
- `src` - Source code block
- `verse` - Verse/poetry block

**Logic:**
1. Detect `#+begin_` prefix at cursor position
2. Return static list of supported block types as `CompletionItemKindKeyword`
3. Insert selected type, then add newline and `#+end_<type>` on subsequent line

**Note:** Simple static completion - no dynamic behavior.

**Output:** `[]CompletionItem | CompletionList | nil`

### 8b. Export Block Completion ✅

**Goal:** Autocomplete export block types after `#+begin_export_`

**Export Types Supported:**
- `html` - HTML export block
- `latex` - LaTeX export block

**Logic:**
1. Detect `#+begin_export_` prefix at cursor position
2. Return static list of export types as `CompletionItemKindKeyword`
3. Insert selected type, then add newline and `#+end_export` on subsequent line

**Output:** `[]CompletionItem | CompletionList | nil`

### 9. Document Symbols (`textDocument/documentSymbol`) ✅

**Goal:** Provide Zed outline view with headings

**Input:** `DocumentSymbolParams{ TextDocument }`

**Logic:**
1. Get document from `OpenDocs` (unsaved changes) or `ProcessedFiles` (saved)
2. Use `Document.Outline` which is a tree of `org.Section` nodes
3. Each `Section` has:
   - `Headline *Headline` - the heading
   - `Parent *Section` - parent section (for hierarchy)
   - `Children []*Section` - child sections (subheadings)
4. Recursively traverse `Outline.Children`:
   - Convert each `Section.Headline` to `DocumentSymbol`
   - Use `Headline.Title` (render to string) as symbol name
   - Use `Headline.Lvl` to determine `SymbolKind` (e.g., level 1 = Namespace, level 2 = Class, etc.)
   - Use `Headline.Pos` for position/range
   - Recursively set `Children` field from `Section.Children`
5. Return symbol tree for outline view

**Output:** `DocumentSymbol[] | nil`

### 10. Workspace Symbols (`workspace/symbol`) ✅

**Goal:** Search across all workspace files for headings (flat list, no hierarchy)

**Input:** `WorkspaceSymbolParams{ Query: string }`

**Logic:**
1. Iterate through `ProcessedFiles.UuidIndex` (sync.Map of UUID → `HeaderLocation`)
2. For each entry:
   - `HeaderLocation` contains `FilePath`, `Position`, and `Title`
   - Match `Title` against query (substring/fuzzy match)
   - Create `SymbolInformation` with:
     - Name: heading title
     - Kind: `SymbolKindInterface` (flat, all same kind)
     - Location: file URI from `FilePath` + `Position`
3. Return flat list of matches

**Note:** No blocks, no hierarchy - just a searchable flat list of all UUID'd headings across workspace.

**Output:** `[]SymbolInformation | nil`

### 11. Code Actions: Heading/List Conversion

**Goal:** Structural transformations between headings and lists

**Actions:**
1. **Heading → Ordered List:** Convert current heading and its subtree to numbered list
2. **Heading → Unordered List:** Convert current heading and its subtree to bullet list
3. **List → Heading:** Convert current list item and subitems to heading hierarchy

**Input:** `CodeActionParams` with range covering heading or list item

**Logic:**
1. Detect node at position:
   - `org.Headline` → convert to list (preserving subtree as nested items)
   - `org.ListItem` → convert to heading (preserving subitems as nested headings)
2. Generate text edit replacing original content
3. Handle level arithmetic (heading levels map to list nesting depth)

**Edge Cases:**
- Mixed content in subtree (preserve non-heading/list content)
- Multiple list items at same level (only convert the one under cursor)

**Output:** `[]CodeAction | nil`

### 12. Code Actions: Evaluate Code Block

**Goal:** Execute src block and insert results below (org-babel style)

**Action:** "Evaluate code block and insert result"

**Input:** Cursor within `org.Block` of type `src`

**Logic:**
1. Detect src block at cursor position
2. Extract:
   - Language from block parameters (e.g., `#+begin_src python`)
   - Code content from block body
3. Map language to executable (hardcoded MVP mapping):
   - `python` → `python3`
   - `bash`/`sh` → `bash`
   - `js`/`javascript` → `node`
   - Additional languages as needed
4. Execute code via `os/exec`, capture stdout
5. Insert result below block as:
   ```org
   #+results:
   <captured output>
   ```
6. Handle errors by showing stderr in hover/message

**Safety:** No sandboxing in MVP - direct execution like org-babel.

**Output:** `[]CodeAction | nil`

## Implementation Phases

### Phase 0: Foundation ✅ (Complete)
- [x] orgscanner package with UUID index
- [x] Position normalization
- [x] Server stub with glsp integration

### Phase 1: Core LSP Setup ✅ (Complete)
- [x] Wire up lifecycle handlers (initialize, shutdown)
- [x] Implement document sync (open, save)
- [x] Implement document sync (change, close)
- [x] Configure server capabilities correctly
- [x] Handle workspace root initialization
- [x] Call orgscanner.Process(root) at startup

### Phase 2: Go-to-Definition ✅ (Complete)
- [x] Implement link detection via AST walking
- [x] Handle `file:` link resolution
- [x] Handle `id:` link resolution via UuidIndex
- [x] Convert org Positions to LSP Positions/Ranges
- [x] Test with real org files

### Phase 3: Hover Previews ✅ (Complete)
- [x] Advertise HoverProvider capability
- [x] Implement link detection for hover
- [x] Return FileInfo.Preview for file links
- [x] Return FileInfo.Preview for ID links
- [x] Format as Markdown
- [x] Add integration tests
- [x] **Enhanced**: Extract actual context lines from target files
- [x] **Refactored**: Shared path resolution logic between definition and hover, resolve ~ and environment variables for file links as part of that

### Phase 4: Backlinks (References) ✅ (Complete)
- [x] Advertise ReferencesProvider capability
- [x] Extract UUID from current headline
- [x] Implement AST walking search for ID references
- [x] Parse referenced files to find link positions
- [x] Return location array with file URIs and ranges
- [x] Add integration tests
- [x] **Architecture**: Reuse existing `findNodeAtPosition` pattern and shared `toProtocolLocation` conversion

### Phase 5: Completion (IDs & Tags) ✅ (Complete)
- [x] Advertise CompletionProvider capability
- [x] Detect completion context (ID in any content, tags in headlines)
- [x] Iterate UuidIndex for ID completion
- [x] Iterate TagMap for tag completion
- [x] Generate CompletionItems with proper formatting (truncated labels, full UUIDs)
- [x] Add integration tests with auto-position finding
- [x] **Refinement**: ID completion shows heading titles (not UUIDs) as labels
- [x] **Refinement**: ID completion details use hover-style context previews
- [x] **Refinement**: Require "[[id:" prefix before cursor for ID completion
- [x] **Refinement**: Auto-close brackets for ID completion items (only if ]] not already present)
- [x] **Refinement**: Filter completion based on already-typed characters
- [x] **Refinement**: Proper tag position detection (only after text, not at beginning)

**Implementation Notes**:
- go-org parser updated: HeaderLocation now includes Title field
- ID completion workflow: Type `[[id:` → see heading titles → select one → full UUID inserted with `]]` if needed
- Only `[[id:` triggers completion (not bare `id:` or `[[`)
- Brackets auto-close only when `]]` doesn't already exist after cursor
- Reused hover preview generation (extracted to shared functions) for completion item details
- Header context shows header line + content below (not arbitrary context above)

### Phase 6: Polish & Testing ✅ (Complete)
- [x] **CRITICAL FIX**: URL-decode file:// URIs to handle spaces (%20) in paths
- [x] Integration testing with real editors (Emacs/Zed)
- [x] Document sync for change/close handlers
- [x] **Architecture**: Depth-aware node finding with inline vs block detection

### Phase 7: Advanced Features 🚧 (In Progress)
- [x] Enhanced References (ID Links) ✅
- [x] File Link Completion ✅
- [x] Block Type Completion (quote, src, verse) ✅
- [x] Export Block Completion (html, latex) ✅
- [x] Document Symbols (Outline View)
- [x] Workspace Symbols
- [ ] Code Actions: Heading<->List Conversion
- [ ] Code Actions: Evaluate Code Block

### Logging Strategy

- Use `slog` (structured logging)
- Log levels:
  - Debug: Link parsing details, index updates
  - Info: Client connect/disconnect, file changes
  - Warn: Missing files, invalid UUIDs
  - Error: Parse failures, panics

## Dependencies

| Package | Version | Purpose |
|---------|---------|---------|
| `github.com/tliron/glsp` | v0.2.2 | LSP protocol server |
| `github.com/tliron/glsp/protocol_3_16` | v0.2.2 | LSP 3.16 types |
| `github.com/alexispurslane/go-org` | v1.9.1 | Org-mode parsing (fork with position support) |
| `github.com/alexispurslane/org-lsp/orgscanner` | local | File scanning, UUID index |
| `golang.org/x/net` | v0.38.0 | Transitive dependency |

## Launch Command

```bash
org-lsp --stdio
```

The server runs on stdio by default for LSP client compatibility.

---

**Version:** 0.1.0  
**Last Updated:** 2026-01-30
**Status:** Phase 7 In Progress - Advanced Features (Link References, Completions, Symbols, Code Actions)
