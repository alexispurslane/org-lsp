package main

import (
	"github.com/alexispurslane/org-lsp/server"
)

func main() {
	// Create the LSP server
	srv := server.New()

	// Run on stdio (standard LSP interface)
	srv.RunStdio()
}
