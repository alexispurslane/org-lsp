package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/alexispurslane/org-lsp/server"
)

func main() {
	var (
		stdio bool
		tcp   string
	)
	flag.BoolVar(&stdio, "stdio", true, "Run in STDIO mode (default)")
	flag.StringVar(&tcp, "tcp", "", "Run in TCP mode with address (e.g., 127.0.0.1:9999)")
	flag.Parse()

	srv := server.New()

	if tcp != "" {
		if err := srv.RunTCP(tcp); err != nil {
			fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
			os.Exit(1)
		}
	} else {
		srv.RunStdio()
	}
}
