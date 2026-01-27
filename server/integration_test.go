package server_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	ourserver "github.com/alexispurslane/org-lsp/server"
	"github.com/sourcegraph/jsonrpc2"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func TestServerLifecycle(t *testing.T) {
	// Setup test directory
	testDir := "testdata"
	if err := os.MkdirAll(testDir, 0755); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	// Create server instance
	t.Log("Creating server instance...")
	srv := ourserver.New()

	// Context with timeout for all operations
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Run server on TCP in background (more reliable for testing than stdio)
	serverDone := make(chan struct{})
	addr := "127.0.0.1:9999" // Use a fixed, non-standard port
	go func() {
		defer close(serverDone)
		t.Logf("Starting server on %s...", addr)
		if err := srv.RunTCP(addr); err != nil {
			t.Logf("Server error: %v", err)
		}
	}()

	// Connect to server with retry logic for reliable startup
	t.Log("Connecting to server...")
	var conn net.Conn
	var err error
	for i := 0; i < 20; i++ { // Retry for up to 2 seconds
		conn, err = net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("Failed to connect to server at %s after retries: %v", addr, err)
	}
	defer conn.Close()
	t.Log("Connected to server")

	// Create JSON-RPC connection
	jsonrpcConn := jsonrpc2.NewConn(ctx, jsonrpc2.NewBufferedStream(conn, jsonrpc2.VSCodeObjectCodec{}), nil)

	// Initialize the server before running subtests
	t.Log("Initializing server...")
	// Get current working directory and use testdata subdirectory
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	testDataPath := filepath.Join(cwd, "testdata")
	rootURI := "file://" + testDataPath
	t.Logf("Server rootURI: %s", rootURI)
	processID := protocol.Integer(12345)
	initParams := protocol.InitializeParams{
		ProcessID: &processID,
		RootURI:   &rootURI,
		Capabilities: protocol.ClientCapabilities{
			TextDocument: &protocol.TextDocumentClientCapabilities{
				Hover: &protocol.HoverClientCapabilities{
					ContentFormat: []protocol.MarkupKind{"markdown", "plaintext"},
				},
			},
		},
	}

	var initResult protocol.InitializeResult
	if err := jsonrpcConn.Call(ctx, "initialize", initParams, &initResult); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	t.Log("Initialize request succeeded")

	// Send initialized notification
	t.Log("Sending initialized notification...")
	initNotification := protocol.InitializedParams{}
	if err := jsonrpcConn.Notify(ctx, "initialized", &initNotification); err != nil {
		t.Fatalf("Initialized notification failed: %v", err)
	}
	t.Log("Initialized notification succeeded")

	// Give server a moment to process initialization
	time.Sleep(100 * time.Millisecond)

	// Test 1: Validate initialization
	t.Run("Initialization", func(t *testing.T) {
		t.Log("Validating initialization results...")

		// Validate capabilities
		if initResult.Capabilities.TextDocumentSync == nil {
			t.Error("Expected TextDocumentSync capability")
		} else {
			syncOpts, ok := initResult.Capabilities.TextDocumentSync.(protocol.TextDocumentSyncOptions)
			if !ok {
				t.Errorf("Expected TextDocumentSync to be TextDocumentSyncOptions, got %T", initResult.Capabilities.TextDocumentSync)
			} else {
				if syncOpts.OpenClose == nil || !*syncOpts.OpenClose {
					t.Errorf("Expected OpenClose to be true, got %v", syncOpts.OpenClose)
				}
				if syncOpts.Change == nil {
					t.Error("Expected Change to be set")
				} else if *syncOpts.Change != protocol.TextDocumentSyncKindFull {
					t.Errorf("Expected Change to be TextDocumentSyncKindFull, got %v", *syncOpts.Change)
				}
				if syncOpts.Save == nil {
					t.Error("Expected Save to be set")
				}
			}
		}

		// Check HoverProvider (type: any - must be bool)
		if hoverProvider, ok := initResult.Capabilities.HoverProvider.(bool); !ok {
			t.Errorf("Expected HoverProvider to be bool, got %T", initResult.Capabilities.HoverProvider)
		} else if !hoverProvider {
			t.Error("Expected HoverProvider to be true")
		}

		// Check DefinitionProvider (type: any - must be bool)
		if definitionProvider, ok := initResult.Capabilities.DefinitionProvider.(bool); !ok {
			t.Errorf("Expected DefinitionProvider to be bool, got %T", initResult.Capabilities.DefinitionProvider)
		} else if !definitionProvider {
			t.Error("Expected DefinitionProvider to be true")
		}

		// Check ReferencesProvider (type: any - must be bool)
		if referencesProvider, ok := initResult.Capabilities.ReferencesProvider.(bool); !ok {
			t.Errorf("Expected ReferencesProvider to be bool, got %T", initResult.Capabilities.ReferencesProvider)
		} else if !referencesProvider {
			t.Error("Expected ReferencesProvider to be true")
		}

		if initResult.Capabilities.CompletionProvider == nil {
			t.Error("Expected CompletionProvider capability")
		}

		// Validate server info
		if initResult.ServerInfo == nil {
			t.Error("Expected ServerInfo")
		} else {
			if initResult.ServerInfo.Name != "org-lsp" {
				t.Errorf("Expected server name 'org-lsp', got '%s'", initResult.ServerInfo.Name)
			}
			if initResult.ServerInfo.Version == nil {
				t.Error("Expected server version")
			}
		}
	})

	// Test 3: Go-to-Definition
	t.Run("GoToDefinition", func(t *testing.T) {
		t.Log("Sending definition request...")

		// Create a test file with a file: link
		testFile := "testdata/test-file.org"
		testContent := "* Test Heading\nThis is a link to [[file:another.org][another file]]"

		if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
		defer os.Remove(testFile)

		params := protocol.DefinitionParams{
			TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: protocol.DocumentUri("file://" + testFile),
				},
				Position: protocol.Position{
					Line:      1,
					Character: 20,
				},
			},
		}

		var result any
		if err := jsonrpcConn.Call(ctx, "textDocument/definition", params, &result); err != nil {
			t.Fatalf("Definition request failed: %v", err)
		}

		t.Logf("Definition response: %v", result)

		// Basic validation - result should be a Location or nil
		if result == nil {
			t.Log("Definition returned nil (no link found or not fully implemented)")
		} else if loc, ok := result.(protocol.Location); ok {
			// Validate location structure
			if loc.URI == "" {
				t.Error("Expected non-empty URI in location")
			}
			t.Logf("Location: %s", loc.URI)
		} else {
			t.Logf("Definition returned non-nil: %T", result)
		}
	})

	// Test 3: File link resolution with didOpen
	t.Run("FileLinkDefinition", func(t *testing.T) {
		t.Log("Testing file link definition with didOpen...")

		// Create target file
		targetFile := "testdata/target.org"
		targetContent := "* Target File\nThis is the target file."
		if err := os.WriteFile(targetFile, []byte(targetContent), 0644); err != nil {
			t.Fatalf("Failed to create target file: %v", err)
		}
		defer os.Remove(targetFile)

		// Verify file was created
		if _, err := os.Stat(targetFile); err != nil {
			t.Logf("WARNING: Target file not found after creation: %v", err)
		} else {
			t.Logf("Target file created successfully")
		}

		// Create source file with link to target
		sourceFile := "testdata/source.org"
		sourceContent := "* Source File\nSee [[file:target.org][the target]] for details."
		if err := os.WriteFile(sourceFile, []byte(sourceContent), 0644); err != nil {
			t.Fatalf("Failed to create source file: %v", err)
		}
		defer os.Remove(sourceFile)

		// Send didOpen to parse the document
		didOpenParams := protocol.DidOpenTextDocumentParams{
			TextDocument: protocol.TextDocumentItem{
				URI:        protocol.DocumentUri("file://" + sourceFile),
				LanguageID: "org",
				Version:    1,
				Text:       sourceContent,
			},
		}
		if err := jsonrpcConn.Notify(ctx, "textDocument/didOpen", didOpenParams); err != nil {
			t.Fatalf("Failed to send didOpen: %v", err)
		}

		// Give it a moment to process
		time.Sleep(100 * time.Millisecond)

		// Now request definition at the link position (line 1, around column 15)
		params := protocol.DefinitionParams{
			TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: protocol.DocumentUri("file://" + sourceFile),
				},
				Position: protocol.Position{
					Line:      1,
					Character: 15,
				},
			},
		}

		var result any
		if err := jsonrpcConn.Call(ctx, "textDocument/definition", params, &result); err != nil {
			t.Fatalf("Definition request failed: %v", err)
		}

		if result == nil {
			t.Error("Definition returned nil, expected a location")
			return
		}

		// Log the successful result and pass - AST walker is working!
		t.Logf("✅ Definition successful! Link found and resolved to: %v", result)
	})

	// Test 4: UUID link definition
	t.Run("UUIDLinkDefinition", func(t *testing.T) {
		t.Log("Testing UUID id: link definition...")

		// Generate a test UUID
		testUUID := "12345678-1234-1234-1234-123456789abc"

		// Create target file with UUID property
		targetFile := "testdata/uuid-target.org"
		absTargetFile, _ := filepath.Abs(targetFile)
		t.Logf("Creating target file: %s", absTargetFile)
		targetContent := fmt.Sprintf(`Foo, bar, baz


* Target Heading
:PROPERTIES:
:ID:       %s
:END:
This is a target file with UUID.`, testUUID)

		if err := os.WriteFile(targetFile, []byte(targetContent), 0644); err != nil {
			t.Fatalf("Failed to create target file: %v", err)
		}

		// Save the file to trigger orgscanner indexing
		saveParams := protocol.DidSaveTextDocumentParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: protocol.DocumentUri("file://" + absTargetFile),
			},
		}
		t.Logf("Sending didSave notification for: %s", absTargetFile)
		if err := jsonrpcConn.Notify(ctx, "textDocument/didSave", saveParams); err != nil {
			t.Fatalf("Failed to send didSave: %v", err)
		}

		// Give scanner time to index
		time.Sleep(200 * time.Millisecond)
		t.Log("Waited 200ms for indexing")

		// Create source file with id: link to UUID
		sourceFile := "testdata/uuid-source.org"
		absSourceFile, _ := filepath.Abs(sourceFile)
		t.Logf("Creating source file: %s", absSourceFile)
		sourceContent := fmt.Sprintf("* Source File\nSee [[id:%s][the target heading]] for details.", testUUID)
		if err := os.WriteFile(sourceFile, []byte(sourceContent), 0644); err != nil {
			t.Fatalf("Failed to create source file: %v", err)
		}

		// Send didOpen to parse the document
		didOpenParams := protocol.DidOpenTextDocumentParams{
			TextDocument: protocol.TextDocumentItem{
				URI:        protocol.DocumentUri("file://" + sourceFile),
				LanguageID: "org",
				Version:    1,
				Text:       sourceContent,
			},
		}
		if err := jsonrpcConn.Notify(ctx, "textDocument/didOpen", didOpenParams); err != nil {
			t.Fatalf("Failed to send didOpen: %v", err)
		}

		// Give it a moment to process
		time.Sleep(100 * time.Millisecond)

		// Now request definition at the id: link position (line 1, around column 15)
		params := protocol.DefinitionParams{
			TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: protocol.DocumentUri("file://" + sourceFile),
				},
				Position: protocol.Position{
					Line:      1,
					Character: 15,
				},
			},
		}

		var result any
		if err := jsonrpcConn.Call(ctx, "textDocument/definition", params, &result); err != nil {
			t.Fatalf("Definition request failed: %v", err)
		}

		if result == nil {
			t.Error("Definition returned nil, expected a location")
			return
		}

		// Log the successful result
		t.Logf("✅ UUID Definition successful! Link found and resolved to: %v", result)
	})

	// Test 4: Shutdown
	t.Run("Shutdown", func(t *testing.T) {
		t.Log("Sending shutdown request...")
		params := struct{}{}
		var result any
		if err := jsonrpcConn.Call(ctx, "shutdown", &params, &result); err != nil {
			t.Fatalf("Shutdown failed: %v", err)
		}
		t.Log("Shutdown request succeeded")

		// Shutdown should return null/nil
		if result != nil {
			t.Errorf("Expected nil result from shutdown, got %v", result)
		}
	})

	// Exit notification to clean shutdown
	t.Log("Sending exit notification...")
	if err := jsonrpcConn.Notify(ctx, "exit", nil); err != nil {
		t.Logf("Exit notification error: %v", err)
	}

	// Wait for server to finish
	t.Log("Waiting for server to exit...")
	select {
	case <-time.After(5 * time.Second):
		t.Log("Server didn't exit in time")
	case <-serverDone:
		t.Log("Server exited cleanly")
	}
}
