package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"runtime/debug"

	"github.com/alexispurslane/org-lsp/lspstream"
	"github.com/alexispurslane/org-lsp/server"
	"go.lsp.dev/protocol"
	"go.uber.org/zap"
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

	// Create server implementation
	impl := &server.ServerImpl{}

	if tcp != "" {
		slog.Info("org-lsp server starting", "mode", "tcp", "address", tcp)
		if err := runTCP(impl, tcp); err != nil {
			fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
			os.Exit(1)
		}
	} else {
		slog.Info("org-lsp server starting", "mode", "stdio")
		if err := runStdio(impl); err != nil {
			fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
			os.Exit(1)
		}
	}
}

func runStdio(impl *server.ServerImpl) error {
	ctx := context.Background()
	logger, err := zap.NewProduction()
	if err != nil {
		return fmt.Errorf("failed to create logger: %w", err)
	}
	rwc := lspstream.NewReadWriteCloser(os.Stdin, os.Stdout, nil)
	stream := lspstream.NewLargeBufferStream(rwc)
	ctx, conn, _ := protocol.NewServer(ctx, impl, stream, logger)
	handler := protocol.ServerHandler(impl, nil)
	conn.Go(ctx, handler)
	<-conn.Done()
	return conn.Err()
}

func runTCP(impl *server.ServerImpl, addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	defer listener.Close()

	for {
		conn, err := listener.Accept()
		if err != nil {
			slog.Error("Failed to accept connection", "error", err)
			continue
		}

		go func() {
			defer conn.Close()
			ctx := context.Background()
			logger, _ := zap.NewProduction()
			stream := lspstream.NewLargeBufferStream(conn)
			ctx, srvConn, _ := protocol.NewServer(ctx, impl, stream, logger)
			handler := protocol.ServerHandler(impl, nil)
			srvConn.Go(ctx, handler)
			<-srvConn.Done()
		}()
	}
}
