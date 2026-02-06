# org-lsp

A minimal Language Server Protocol (LSP) implementation for
[org-mode](https://orgmode.org/) files, implemented with BDD
specification-driven development.

The point of this language server is not to implement all of the agenda, todo list, logbook, punching, and other project, scheduling, and life-management features of org-mode, but instead to focus on purely the personal wiki/PKMS aspect, hence why I'm calling it "minimal," despite having a pretty large range of features. This is primarily because, while most PKMS features can be implemented via the LSP, since the kind of navigation and completion functionality you want for code looks a lot like what you want for a PKMS, the kind of more comprehensive, flexible interfaces you want for a scheduling/life-management/project-management system like org-agenda is well out of scope for a language server.

(Although I might eventually implement some basic capabilities in that direction using terminal commands; we'll see).

## Features

- **Navigation**: Go-to-definition for `file:` and `id:` links, document symbols, workspace symbols, backlink references, link hover previews
- **Completion**: Tags, file links, id links, block types, export formats
- **Editing**: Document formatting (spacing, alignment, UUID injection), folding ranges
- **Sync**: Full support for open, change, save, and close operations

## Installation

### From Source

```bash
git clone https://github.com/alexispurslane/org-lsp.git
cd org-lsp
just install  # Installs to ~/.local/bin/org-lsp
```

### Requirements

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

```bash
just build          # Build the server
just test           # Run all tests
just fmt            # Format code
```

**IMPORTANT**: Always use `just` for building/testing - it ensures proper build flags.

## Contributing

See [AGENTS.md](AGENTS.md) for development guidelines, testing patterns, and architectural decisions.

## License

BSD Zero-Clause (Public Domain equivalent) - see [LICENSE](LICENSE) file for details.

For complete feature specifications, see the BDD tests in `integration/`
