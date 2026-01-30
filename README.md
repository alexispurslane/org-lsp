# org-lsp

A minimal Language Server Protocol (LSP) implementation for [org-mode](https://orgmode.org/) files, focused on navigation and linking capabilities.

## Features

- **Go-to-definition**: Navigate to `file:` and `id:` links
- **Backlinks**: Find all references to the header ID under your cursor across the workspace (references are `id:` links that point to the current header's UUID)
- **Hover previews**: See context when hovering over links
- **ID-link autocompletion**: Complete `id:` references with heading titles, insert full UUIDs
- **Tag completion**: Autocomplete `:tags:` based on scanned org files
- **Document sync**: Full support for open, change, save, and close operations

## Installation

### From Source

```bash
# Clone and build
git clone https://github.com/alexispurslane/org-lsp.git
cd org-lsp
just install  # Installs to ~/.local/bin/org-lsp
```

### Dependencies

- Go 1.25.6 or later
- [`just`](https://just.systems/) task runner

## Usage

### Running the Server

```bash
# Standard LSP mode (stdio)
org-lsp --stdio

# TCP mode for debugging
org-lsp --tcp 127.0.0.1:9999
```

### Editor Configuration

#### Emacs (with eglot)

```elisp
(use-package eglot
  :ensure t)

;; Define a custom major mode for org files with LSP support and no interference
;; from built-in org-mode features
(define-derived-mode org-lsp-mode fundamental-mode "Org+LSP"
  "Major mode for org files with LSP support."
  (eglot-ensure))

;; Use org-lsp-mode for .org files
(add-to-list 'auto-mode-alist '("\\.org\\'" . org-lsp-mode))

;; Register org-lsp server with eglot
(with-eval-after-load 'eglot
  (add-to-list 'eglot-server-programs
               `(org-lsp-mode . ("/path/to/org-lsp" "--stdio"))))
```

#### Zed

Add to `settings.json`:

```json
{
  "lsp": {
    "org": {
      "initialization_options": {},
      "binary": {
        "path": "/path/to/org-lsp",
        "arguments": ["--stdio"]
      }
    }
  }
}
```

#### NeoVim

In your `init.lua` or `~/.config/nvim/init.lua`:

```lua
local lspconfig = require('lspconfig')

lspconfig.org_lsp.setup {
  cmd = { "/path/to/org-lsp", "--stdio" },
  filetypes = { "org" },
  root_dir = lspconfig.util.root_pattern(".git"),
}
```

Or using Neovim's native LSP:

```lua
vim.api.nvim_create_autocmd("FileType", {
  pattern = "org",
  callback = function()
    vim.lsp.start {
      name = "org-lsp",
      cmd = { "/path/to/org-lsp", "--stdio" },
      root_dir = vim.fs.root(0, ".git"),
    }
  end,
})
```

#### Helix

In `~/.config/helix/languages.toml` (make sure org-lsp is in your PATH for Helix
to find it --- you can use `just install` for that):

```toml
[language-server.org-lsp]
command = "org-lsp"
args = ["--stdio"]

[[language]]
name = "org"
language-id = "org"
file-types = ["org"]
roots = [".git"]
language-servers = ["org-lsp"]
```

## Development

### Building

```bash
just build          # Build the server
just test           # Run all tests (INFO logs)
just test-quiet     # Run tests (ERROR logs only)
just fmt            # Format code
just lint           # Run linters
just clean          # Remove build artifacts
```

### Project Structure

```
org-lsp/
├── cmd/server/          # CLI entry point
├── server/              # LSP protocol handlers and server logic
├── orgscanner/          # File scanning, parsing, and indexing
├── testdata/            # Test fixtures
├── SPEC.md              # Detailed feature specification
├── AGENTS.md            # Development guidelines for AI agents
└── justfile             # Build automation
```

### Architecture

org-lsp uses a layered architecture:

1. **LSP Layer** (`glsp`) - Protocol-3.16 compliant LSP server
2. **Server Layer** (`server/`) - Request routing and handler logic
3. **Domain Layer** (`orgscanner/`) - Org-mode parsing and indexing (LSP-agnostic)

Key design principle: Keep orgscanner pure of LSP concerns. All protocol-to-domain type conversions happen at the server boundary.

### Testing

Tests use `github.com/stretchr/testify` for clear assertions:

```go
require.NotNil(t, result, "Expected non-nil result")
assert.Equal(t, expected, actual, "Values should match")
```

Run specific tests:
```bash
just test HoverFileLink
just test-quiet TestServerLifecycle
```

## Contributing

Please see [AGENTS.md](AGENTS.md) for detailed development guidelines. Key points:

- Use `just` for all build/test operations (never `go build` directly)
- Respect package boundaries (server/ vs orgscanner/)
- Add tests for new features
- Ensure heading syntax is correct (no leading whitespace before `*`)
- Follow established patterns for handler implementation

## License

BSD Zero-Clause (Public Domain equivalent) - see [LICENSE](LICENSE) file for details.

## Status

**Version:** 0.0.1  
**Last Updated:** 2026-01-30  
**Status:** MVP Complete - All core features implemented and tested

For complete feature specifications and implementation details, see [SPEC.md](SPEC.md).
