package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"

	"github.com/alexispurslane/org-lsp/server"
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("PANIC", "error", r, "stack", string(debug.Stack()))
			os.Exit(1)
		}
	}()

	var (
		stdio bool
		tcp   string
	)
	flag.BoolVar(&stdio, "stdio", true, "Run in STDIO mode (default)")
	flag.StringVar(&tcp, "tcp", "", "Run in TCP mode with address (e.g., 127.0.0.1:9999)")
	flag.Parse()

	srv := server.New()

	if tcp != "" {
		slog.Info("org-lsp server starting", "mode", "tcp", "address", tcp)
		if err := srv.RunTCP(tcp); err != nil {
			fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
			os.Exit(1)
		}
	} else {
		slog.Info("org-lsp server starting", "mode", "stdio")
		if err := srv.RunStdio(); err != nil {
			fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
			os.Exit(1)
		}
	}
}
