package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"text/template"
	"time"

	"github.com/alexispurslane/org-lsp/lspstream"
	ourserver "github.com/alexispurslane/org-lsp/server"
	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.uber.org/zap"
)

// LSPTestContext manages server lifecycle and provides test helpers
type LSPTestContext struct {
	t            *testing.T
	conn         jsonrpc2.Conn
	ctx          context.Context
	cancel       context.CancelFunc
	tempDir      string
	rootURI      string
	server       *ourserver.ServerImpl
	done         chan struct{}
	listener     net.Listener
	TestData     map[string]string // Storage for test-specific data like UUIDs
	lastSaveTime time.Time         // Track when we last triggered a save for indexing polls
	docVersion   int               // Track document version for didChange notifications

	// Notification capture
	notificationsMu sync.RWMutex
	notifications   map[string][]json.RawMessage // method -> []params (keeps history)
}

// NewTestContext creates a temp directory in /tmp, starts the LSP server
// with that directory as root, and returns a context for testing.
func NewTestContext(t *testing.T) *LSPTestContext {
	t.Helper()

	// Create temp directory in /tmp for automatic OS cleanup
	tempDir, err := os.MkdirTemp("", "org-lsp-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

	// Create server instance
	srv := ourserver.New()

	// Start server on TCP in background
	done := make(chan struct{})
	ready := make(chan struct{})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		cancel()
		os.RemoveAll(tempDir)
		t.Fatalf("Failed to create TCP listener: %v", err)
	}
	addr := listener.Addr().String()

	go func(s *ourserver.ServerImpl) {
		defer close(done)
		close(ready) // Signal that listener is ready
		for {
			conn, err := listener.Accept()
			if err != nil {
				return // Listener closed
			}
			go func(c net.Conn, server *ourserver.ServerImpl) {
				defer c.Close()
				logger, _ := zap.NewProduction()
				stream := lspstream.NewLargeBufferStream(c)
				_, srvConn, client := protocol.NewServer(ctx, server, stream, logger)
				server.SetClient(client)
				<-srvConn.Done()
			}(conn, s)
		}
	}(srv)

	// Wait for server to be ready
	<-ready

	// Connect to server with retry (immediate retries, no sleep)

	clientConn, err := net.Dial("tcp", addr)

	if err != nil {
		cancel()
		listener.Close()
		os.RemoveAll(tempDir)
		t.Fatalf("Failed to connect to server: %v", err)
	}

	// Initialize server URI (must be defined before creating context)
	rootURI := "file://" + tempDir

	// Create client-side JSON-RPC connection (same pattern as existing tests)
	jsonrpcConn := jsonrpc2.NewConn(lspstream.NewLargeBufferStream(clientConn))

	// Create test context with notification capture
	tc := &LSPTestContext{
		t:             t,
		conn:          jsonrpcConn,
		ctx:           ctx,
		cancel:        cancel,
		tempDir:       tempDir,
		rootURI:       rootURI,
		server:        srv,
		done:          done,
		listener:      listener,
		TestData:      make(map[string]string),
		docVersion:    1,
		notifications: make(map[string][]json.RawMessage),
	}

	// Start background reader with notification capture
	jsonrpcConn.Go(ctx, tc.notificationHandler)

	// Initialize server
	initParams := protocol.InitializeParams{
		ProcessID: int32(os.Getpid()),
		RootURI:   protocol.DocumentURI(rootURI),
	}

	var initResult protocol.InitializeResult
	_, err = jsonrpcConn.Call(ctx, "initialize", initParams, &initResult)
	if err != nil {
		cancel()
		clientConn.Close()
		listener.Close()
		os.RemoveAll(tempDir)
		t.Fatalf("Initialize failed: %v", err)
	}

	// Send initialized notification
	err = jsonrpcConn.Notify(ctx, "initialized", protocol.InitializedParams{})
	if err != nil {
		cancel()
		clientConn.Close()
		listener.Close()
		os.RemoveAll(tempDir)
		t.Fatalf("Initialized notification failed: %v", err)
	}

	return tc
}

// GivenFile creates a file in the temp directory with template substitution.
// The path is relative to the temp directory root.
// Content is treated as a Go text/template, with tc.TestData as the data context.
// Use {{.KeyName}} to substitute values from TestData.
// notificationHandler handles incoming JSON-RPC notifications from the server
func (tc *LSPTestContext) notificationHandler(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
	// Only capture notifications (not calls)
	if _, isNotification := req.(*jsonrpc2.Notification); !isNotification {
		return nil
	}
	tc.notificationsMu.Lock()
	tc.notifications[req.Method()] = append(tc.notifications[req.Method()], req.Params())
	tc.notificationsMu.Unlock()
	// No reply needed for notifications
	return nil
}

func (tc *LSPTestContext) GivenFile(path, content string) *LSPTestContext {
	tc.t.Helper()

	fullPath := filepath.Join(tc.tempDir, path)
	dir := filepath.Dir(fullPath)

	err := os.MkdirAll(dir, 0755)
	if err != nil {
		tc.t.Fatalf("Failed to create directory %s: %v", dir, err)
	}

	// Execute template with TestData as context
	tmpl, err := template.New(path).Parse(content)
	if err != nil {
		tc.t.Fatalf("Failed to parse template for %s: %v", path, err)
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, tc.TestData)
	if err != nil {
		tc.t.Fatalf("Failed to execute template for %s: %v", path, err)
	}

	err = os.WriteFile(fullPath, buf.Bytes(), 0644)
	if err != nil {
		tc.t.Fatalf("Failed to create file %s: %v", path, err)
	}

	return tc
}

// GivenOpenFile opens a document in the LSP server.
// The uri should be relative like "test.org" (will be resolved to file://tempDir/test.org).
func (tc *LSPTestContext) GivenOpenFile(uri string) *LSPTestContext {
	tc.t.Helper()

	// Resolve relative URI to absolute
	fullURI := tc.resolveURI(uri)
	filePath := uriToPath(string(fullURI))

	content, err := os.ReadFile(filePath)
	if err != nil {
		tc.t.Fatalf("Failed to read file for didOpen: %v", err)
	}

	params := protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:        fullURI,
			LanguageID: "org",
			Version:    1,
			Text:       string(content),
		},
	}

	err = tc.conn.Notify(tc.ctx, "textDocument/didOpen", params)
	if err != nil {
		tc.t.Fatalf("didOpen failed: %v", err)
	}

	return tc
}

// GivenSaveFile triggers a didSave notification for the document.
func (tc *LSPTestContext) GivenSaveFile(uri string) *LSPTestContext {
	tc.t.Helper()

	fullURI := tc.resolveURI(uri)
	params := protocol.DidSaveTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: fullURI,
		},
	}

	err := tc.conn.Notify(tc.ctx, "textDocument/didSave", params)
	if err != nil {
		tc.t.Fatalf("didSave failed: %v", err)
	}

	// Record when we triggered this save so pollUntilIndexed can wait for it
	tc.lastSaveTime = time.Now()

	return tc
}

// GivenChangeDocument triggers a didChange notification with full document sync.
// The content parameter is the new full content of the document.
func (tc *LSPTestContext) GivenChangeDocument(uri, content string) *LSPTestContext {
	tc.t.Helper()

	fullURI := tc.resolveURI(uri)

	// Increment version for this change
	tc.docVersion++

	params := protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{
				URI: fullURI,
			},
			Version: int32(tc.docVersion),
		},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{
			{
				RangeLength: 0, // 0 indicates full document sync
				Text:        content,
			},
		},
	}

	err := tc.conn.Notify(tc.ctx, "textDocument/didChange", params)
	if err != nil {
		tc.t.Fatalf("didChange failed: %v", err)
	}

	return tc
}

// When performs an LSP operation and calls the handler with the result.
// It wraps the operation in t.Run with a "when " prefix for Gherkin-style output.
// For methods requiring indexed data, it polls internally until ready.
func When[T any](t *testing.T, tc *LSPTestContext, description string, method string, params any, handler func(*testing.T, T)) bool {
	return t.Run("when "+description, func(t *testing.T) {
		// Poll if needed for indexing
		if requiresIndexing(method) {
			tc.pollUntilIndexed(params)
		}

		var result T
		if _, err := tc.conn.Call(tc.ctx, method, params, &result); err != nil {
			t.Fatalf("LSP call %s failed: %v", method, err)
		}

		handler(t, result)
	})
}

// Shutdown gracefully shuts down the server and cleans up resources
func (tc *LSPTestContext) Shutdown() {
	tc.cancel()
	tc.conn.Close()
	tc.listener.Close()
	<-tc.done
}

// NotificationCount returns the number of captured notifications for a method
func (tc *LSPTestContext) NotificationCount(method string) int {
	tc.notificationsMu.RLock()
	defer tc.notificationsMu.RUnlock()
	return len(tc.notifications[method])
}

// GetNotifications returns all captured notifications for a method
func (tc *LSPTestContext) GetNotifications(method string) []json.RawMessage {
	tc.notificationsMu.RLock()
	defer tc.notificationsMu.RUnlock()

	result := make([]json.RawMessage, len(tc.notifications[method]))
	copy(result, tc.notifications[method])
	return result
}

// PollNotification waits for at least one notification of the given method
func (tc *LSPTestContext) PollNotification(method string, timeout time.Duration) []json.RawMessage {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		tc.notificationsMu.RLock()
		notifications := tc.notifications[method]
		tc.notificationsMu.RUnlock()

		if len(notifications) > 0 {
			result := make([]json.RawMessage, len(notifications))
			copy(result, notifications)
			return result
		}

		time.Sleep(5 * time.Millisecond)
	}
	return nil
}

// ClearNotifications clears captured notifications for a method (or all if method is empty)
func (tc *LSPTestContext) ClearNotifications(method string) {
	tc.notificationsMu.Lock()
	defer tc.notificationsMu.Unlock()

	if method == "" {
		tc.notifications = make(map[string][]json.RawMessage)
	} else {
		delete(tc.notifications, method)
	}
}

// requiresIndexing returns true if the method requires data to be indexed
func requiresIndexing(method string) bool {
	switch method {
	case "textDocument/definition", "textDocument/references", "textDocument/codeLens":
		return true
	default:
		return false
	}
}

// pollUntilIndexed waits for indexing to complete for the given parameters.
// It polls until the scanner's LastScanTime is after our last save time.
func (tc *LSPTestContext) pollUntilIndexed(params any) {
	if tc.server == nil {
		return
	}

	// Fast path: if we haven't saved anything, indexing should already be done
	// (the initial scan happens synchronously during Initialize)
	if tc.lastSaveTime.IsZero() {
		return
	}

	// Poll until the server's LastScanTime is after our last save time
	// This indicates that the scanner has processed our save
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if tc.server.LastScanTime().After(tc.lastSaveTime) {
			return // Indexing is complete
		}
		time.Sleep(5 * time.Millisecond) // Short yield between polls
	}

	// Timeout - indexing didn't complete in time
	tc.t.Logf("Warning: Indexing did not complete within 2 seconds after save at %v", tc.lastSaveTime)
}

// DocURI returns a DocumentURI for a file relative to the test root
func (tc *LSPTestContext) DocURI(filename string) protocol.DocumentURI {
	return protocol.DocumentURI(tc.rootURI + "/" + filename)
}

// PosAfter returns a Position just after the first occurrence of marker in the specified file
// Useful for placing cursor right after a prefix like "[[id:"
func (tc *LSPTestContext) PosAfter(filename, marker string) protocol.Position {
	content, err := os.ReadFile(filepath.Join(tc.tempDir, filename))
	if err != nil {
		tc.t.Fatalf("Failed to read file %s for PosAfter: %v", filename, err)
	}

	lines := strings.Split(string(content), "\n")
	for lineNum, line := range lines {
		if idx := strings.Index(line, marker); idx >= 0 {
			return protocol.Position{
				Line:      uint32(lineNum),
				Character: uint32(idx + len(marker)),
			}
		}
	}
	tc.t.Fatalf("Marker %q not found in file %s", marker, filename)
	return protocol.Position{Line: 0, Character: 0}
}

// PosBefore returns a Position just before the first occurrence of marker in the specified file
// Useful for placing cursor right before a suffix like "]]"
func (tc *LSPTestContext) PosBefore(filename, marker string) protocol.Position {
	content, err := os.ReadFile(filepath.Join(tc.tempDir, filename))
	if err != nil {
		tc.t.Fatalf("Failed to read file %s for PosBefore: %v", filename, err)
	}

	lines := strings.Split(string(content), "\n")
	for lineNum, line := range lines {
		if idx := strings.Index(line, marker); idx >= 0 {
			return protocol.Position{
				Line:      uint32(lineNum),
				Character: uint32(idx),
			}
		}
	}
	return protocol.Position{Line: 0, Character: 0}
}

// resolveURI converts a relative URI to an absolute file:// URI
func (tc *LSPTestContext) resolveURI(uri string) protocol.DocumentURI {
	if filepath.IsAbs(uri) {
		return protocol.DocumentURI("file://" + uri)
	}
	// Handle file:// prefix if present
	if len(uri) > 7 && uri[:7] == "file://" {
		// Already has file:// prefix, check if path is absolute
		pathPart := uri[7:]
		if filepath.IsAbs(pathPart) {
			return protocol.DocumentURI(uri)
		}
		// Relative path after file://
		fullPath := filepath.Join(tc.tempDir, pathPart)
		return protocol.DocumentURI("file://" + fullPath)
	}
	// No file:// prefix, treat as relative path
	fullPath := filepath.Join(tc.tempDir, uri)
	return protocol.DocumentURI("file://" + fullPath)
}

// uriToPath converts a file:// URI to a filesystem path
func uriToPath(uri string) string {
	u, _ := url.Parse(uri)
	path := u.Path

	// Handle Windows paths (remove leading slash if present and path starts with drive letter)
	if len(path) > 2 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}

	// URL decode to handle spaces (%20) and other encoded characters
	path, _ = url.QueryUnescape(path)
	return path
}
