package server_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ourserver "github.com/alexispurslane/org-lsp/server"
	"github.com/sourcegraph/jsonrpc2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// findCursorPosition finds the line and column of a marker string in content
// Returns 0-based line and character position (column right after the marker)
func findCursorPosition(content, marker string) (line uint32, char uint32) {
	lines := strings.Split(content, "\n")
	for i, l := range lines {
		if idx := strings.Index(l, marker); idx >= 0 {
			return uint32(i), uint32(idx + len(marker))
		}
	}
	return 0, 0
}

func TestServerLifecycle(t *testing.T) {
	// Setup test directory
	testDir := "testdata"
	err := os.MkdirAll(testDir, 0755)
	require.NoError(t, err, "Failed to create test directory")

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
		if err := srv.RunTCP(addr); err != nil {
			t.Logf("Server error: %v", err)
		}
	}()

	// Connect to server with retry logic for reliable startup
	t.Log("Connecting to server...")
	var conn net.Conn
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
	err = jsonrpcConn.Call(ctx, "initialize", initParams, &initResult)
	require.NoError(t, err, "Initialize failed")
	t.Log("Initialize request succeeded")

	// Send initialized notification
	t.Log("Sending initialized notification...")
	initNotification := protocol.InitializedParams{}
	err = jsonrpcConn.Notify(ctx, "initialized", &initNotification)
	require.NoError(t, err, "Initialized notification failed")
	t.Log("Initialized notification succeeded")

	// Give server a moment to process initialization
	time.Sleep(100 * time.Millisecond)

	// Test 1: Validate initialization
	t.Run("Initialization", func(t *testing.T) {
		// Validate capabilities
		require.NotNil(t, initResult.Capabilities.TextDocumentSync, "Expected TextDocumentSync capability")

		syncOpts, ok := initResult.Capabilities.TextDocumentSync.(protocol.TextDocumentSyncOptions)
		require.True(t, ok, "Expected TextDocumentSync to be TextDocumentSyncOptions, got %T", initResult.Capabilities.TextDocumentSync)

		assert.True(t, *syncOpts.OpenClose, "Expected OpenClose to be true")
		require.NotNil(t, syncOpts.Change, "Expected Change to be set")
		assert.Equal(t, protocol.TextDocumentSyncKindFull, *syncOpts.Change, "Expected Change to be TextDocumentSyncKindFull")
		require.NotNil(t, syncOpts.Save, "Expected Save to be set")

		// Check HoverProvider (type: any - must be bool)
		hoverProvider, ok := initResult.Capabilities.HoverProvider.(bool)
		require.True(t, ok, "Expected HoverProvider to be bool, got %T", initResult.Capabilities.HoverProvider)
		assert.True(t, hoverProvider, "Expected HoverProvider to be true")

		// Check DefinitionProvider (type: any - must be bool)
		definitionProvider, ok := initResult.Capabilities.DefinitionProvider.(bool)
		require.True(t, ok, "Expected DefinitionProvider to be bool, got %T", initResult.Capabilities.DefinitionProvider)
		assert.True(t, definitionProvider, "Expected DefinitionProvider to be true")

		// Check ReferencesProvider (type: any - must be bool)
		referencesProvider, ok := initResult.Capabilities.ReferencesProvider.(bool)
		require.True(t, ok, "Expected ReferencesProvider to be bool, got %T", initResult.Capabilities.ReferencesProvider)
		assert.True(t, referencesProvider, "Expected ReferencesProvider to be true")

		require.NotNil(t, initResult.Capabilities.CompletionProvider, "Expected CompletionProvider capability")

		// Validate server info
		require.NotNil(t, initResult.ServerInfo, "Expected ServerInfo")
		assert.Equal(t, "org-lsp", initResult.ServerInfo.Name, "Server name mismatch")
		require.NotNil(t, initResult.ServerInfo.Version, "Expected server version")
	})

	// Test 3: Go-to-Definition
	t.Run("GoToDefinition", func(t *testing.T) {
		// Create a test file with a file: link
		testFile := "testdata/test-file.org"
		testContent := "* Test Heading\nThis is a link to [[file:another.org][another file]]"

		err := os.WriteFile(testFile, []byte(testContent), 0644)
		require.NoError(t, err, "Failed to create test file")
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
		err = jsonrpcConn.Call(ctx, "textDocument/definition", params, &result)
		require.NoError(t, err, "Definition request failed")

		// Basic validation - result should be a Location or nil
		if result != nil {
			loc, ok := result.(protocol.Location)
			if ok {
				assert.NotEmpty(t, loc.URI, "Expected non-empty URI in location")
			}
		}
	})

	// Test 3: File link resolution with didOpen
	t.Run("FileLinkDefinition", func(t *testing.T) {
		t.Log("Testing file link definition with didOpen...")

		// Create target file
		targetFile := "testdata/target.org"
		targetContent := "* Target File\nThis is the target file."
		err := os.WriteFile(targetFile, []byte(targetContent), 0644)
		require.NoError(t, err, "Failed to create target file")
		defer os.Remove(targetFile)

		// Verify file was created
		_, err = os.Stat(targetFile)
		if err != nil {
			t.Logf("WARNING: Target file not found after creation: %v", err)
		} else {
			t.Logf("Target file created successfully")
		}

		// Create source file with link to target
		sourceFile := "testdata/source.org"
		sourceContent := "* Source File\nSee [[file:./target.org][the target]] for details."
		err = os.WriteFile(sourceFile, []byte(sourceContent), 0644)
		require.NoError(t, err, "Failed to create source file")
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
		err = jsonrpcConn.Notify(ctx, "textDocument/didOpen", didOpenParams)
		require.NoError(t, err, "Failed to send didOpen")

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
		err = jsonrpcConn.Call(ctx, "textDocument/definition", params, &result)
		require.NoError(t, err, "Definition request failed")

		require.NotNil(t, result, "Definition returned nil, expected a location")

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

		err := os.WriteFile(targetFile, []byte(targetContent), 0644)
		require.NoError(t, err, "Failed to create target file")

		// Save the file to trigger orgscanner indexing
		saveParams := protocol.DidSaveTextDocumentParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: protocol.DocumentUri("file://" + absTargetFile),
			},
		}
		t.Logf("Sending didSave notification for: %s", absTargetFile)
		err = jsonrpcConn.Notify(ctx, "textDocument/didSave", saveParams)
		require.NoError(t, err, "Failed to send didSave")

		// Give scanner time to index
		time.Sleep(200 * time.Millisecond)
		t.Log("Waited 200ms for indexing")

		// Create source file with id: link to UUID
		sourceFile := "testdata/uuid-source.org"
		absSourceFile, _ := filepath.Abs(sourceFile)
		t.Logf("Creating source file: %s", absSourceFile)
		sourceContent := fmt.Sprintf("* Source File\nSee [[id:%s][the target heading]] for details.", testUUID)
		err = os.WriteFile(sourceFile, []byte(sourceContent), 0644)
		require.NoError(t, err, "Failed to create source file")

		// Send didOpen to parse the document
		didOpenParams := protocol.DidOpenTextDocumentParams{
			TextDocument: protocol.TextDocumentItem{
				URI:        protocol.DocumentUri("file://" + sourceFile),
				LanguageID: "org",
				Version:    1,
				Text:       sourceContent,
			},
		}
		err = jsonrpcConn.Notify(ctx, "textDocument/didOpen", didOpenParams)
		require.NoError(t, err, "Failed to send didOpen")

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
		err = jsonrpcConn.Call(ctx, "textDocument/definition", params, &result)
		require.NoError(t, err, "Definition request failed")

		require.NotNil(t, result, "Definition returned nil, expected a location")

		// Log the successful result
		t.Logf("✅ UUID Definition successful! Link found and resolved to: %v", result)
	})

	// Test 5: Hover Previews for file: links
	t.Run("HoverFileLink", func(t *testing.T) {
		// Create target file with context
		targetFile := "testdata/hover-target.org"
		targetContent := `* Target File
	This is the target file with some content.
	** Subheading
	More content here.
	*** Another level
	Even more content.`

		err := os.WriteFile(targetFile, []byte(targetContent), 0644)
		require.NoError(t, err, "Failed to create target file")
		defer os.Remove(targetFile)

		absTargetFile, _ := filepath.Abs(targetFile)
		t.Logf("Created target file: %s (abs: %s)", targetFile, absTargetFile)

		// Create source file with link to target
		sourceFile := "testdata/hover-source.org"
		absSourceFile, _ := filepath.Abs(sourceFile)
		sourceContent := "* Source File\nHover over [[file:hover-target.org][this link]] to see preview."

		err = os.WriteFile(sourceFile, []byte(sourceContent), 0644)
		require.NoError(t, err, "Failed to create source file")
		defer os.Remove(sourceFile)

		t.Logf("Created source file: %s (abs: %s)", sourceFile, absSourceFile)

		// Open source document
		didOpenParams := protocol.DidOpenTextDocumentParams{
			TextDocument: protocol.TextDocumentItem{
				URI:        protocol.DocumentUri("file://" + absSourceFile),
				LanguageID: "org",
				Version:    1,
				Text:       sourceContent,
			},
		}
		err = jsonrpcConn.Notify(ctx, "textDocument/didOpen", didOpenParams)
		require.NoError(t, err, "Failed to send didOpen")

		time.Sleep(100 * time.Millisecond)

		// Request hover at link position
		hoverParams := protocol.HoverParams{
			TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: protocol.DocumentUri("file://" + absSourceFile),
				},
				Position: protocol.Position{
					Line:      1,
					Character: 15,
				},
			},
		}

		var result *protocol.Hover
		err = jsonrpcConn.Call(ctx, "textDocument/hover", hoverParams, &result)
		require.NoError(t, err, "Hover request failed")

		require.NotNil(t, result, "Hover returned nil, expected hover content")

		// Type assert Contents to MarkupContent
		markupContent, ok := result.Contents.(protocol.MarkupContent)
		require.True(t, ok, "Expected Contents to be MarkupContent, got %T", result.Contents)

		// Validate hover structure
		assert.Equal(t, protocol.MarkupKindMarkdown, markupContent.Kind, "Expected markdown content")

		// Check content includes key elements
		content := markupContent.Value
		assert.Contains(t, content, "FILE Link", "Expected 'FILE Link' in content")
		assert.Contains(t, content, "hover-target.org", "Expected target filename in content")
		assert.Contains(t, content, "```org", "Expected org code block")

		t.Logf("✅ Hover successful! Content preview:\n%s", content)
	})

	// Test 6: Hover Previews for id: links
	t.Run("HoverIDLink", func(t *testing.T) {

		// Generate a test UUID
		testUUID := "87654321-4321-4321-4321-cba987654321"

		// Create target file with UUID heading
		targetFile := "testdata/hover-id-target.org"
		absTargetFile, _ := filepath.Abs(targetFile)
		targetContent := fmt.Sprintf(`Before heading

* UUID Target Heading :test:tag:
:PROPERTIES:
:ID:       %s
:END:
This heading has UUID property.
Some more content below.
** Subheading with details
Even more nested content here.`, testUUID)

		err := os.WriteFile(targetFile, []byte(targetContent), 0644)
		require.NoError(t, err, "Failed to create target file")
		defer os.Remove(targetFile)

		// Save to trigger indexing
		saveParams := protocol.DidSaveTextDocumentParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: protocol.DocumentUri("file://" + absTargetFile),
			},
		}
		err = jsonrpcConn.Notify(ctx, "textDocument/didSave", saveParams)
		require.NoError(t, err, "Failed to send didSave")

		time.Sleep(200 * time.Millisecond)

		// Create source file with id link
		sourceFile := "testdata/hover-id-source.org"
		absSourceFile, _ := filepath.Abs(sourceFile)
		sourceContent := fmt.Sprintf("* Source\nSee [[id:%s][UUID target]] for info.", testUUID)

		err = os.WriteFile(sourceFile, []byte(sourceContent), 0644)
		require.NoError(t, err, "Failed to create source file")
		defer os.Remove(sourceFile)

		// Open source document
		didOpenParams := protocol.DidOpenTextDocumentParams{
			TextDocument: protocol.TextDocumentItem{
				URI:        protocol.DocumentUri("file://" + absSourceFile),
				LanguageID: "org",
				Version:    1,
				Text:       sourceContent,
			},
		}
		err = jsonrpcConn.Notify(ctx, "textDocument/didOpen", didOpenParams)
		require.NoError(t, err, "Failed to send didOpen")

		time.Sleep(200 * time.Millisecond)

		// Request hover
		hoverParams := protocol.HoverParams{
			TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: protocol.DocumentUri("file://" + absSourceFile),
				},
				Position: protocol.Position{
					Line:      1,
					Character: 10,
				},
			},
		}

		var result *protocol.Hover
		err = jsonrpcConn.Call(ctx, "textDocument/hover", hoverParams, &result)
		require.NoError(t, err, "Hover request failed")

		require.NotNil(t, result, "Hover returned nil, expected hover content")

		markupContent, ok := result.Contents.(protocol.MarkupContent)
		require.True(t, ok, "Expected Contents to be MarkupContent, got %T", result.Contents)

		content := markupContent.Value
		t.Logf("Hover response: %s", content)

		// Validate ID link hover content
		require.Contains(t, content, "ID Link", "Expected 'ID Link' in content")
		require.Contains(t, content, "hover-id-target.org", "Expected target filename in hover content")
		require.Contains(t, content, "```org", "Expected org code block with context lines")
	})

	// Test 7: Backlinks (References)
	t.Run("Backlinks", func(t *testing.T) {
		t.Log("Testing backlinks to UUID...")

		// Create target file with UUID
		targetFile := "testdata/backlink-target.org"
		absTargetFile, _ := filepath.Abs(targetFile)
		targetContent := `* Target Heading :tag:
:PROPERTIES:
:ID:       11111111-1111-1111-1111-111111111111
:END:
This is the target file.`

		err = os.WriteFile(targetFile, []byte(targetContent), 0644)
		require.NoError(t, err, "Failed to create target file")
		defer os.Remove(targetFile)

		// Create first source file with id: link
		sourceFile1 := "testdata/backlink-source1.org"
		absSourceFile1, _ := filepath.Abs(sourceFile1)
		sourceContent1 := `* Source File 1
This file references the target: [[id:11111111-1111-1111-1111-111111111111][target heading]]

** Subsection
Another reference [[id:11111111-1111-1111-1111-111111111111]] here.`

		err = os.WriteFile(sourceFile1, []byte(sourceContent1), 0644)
		require.NoError(t, err, "Failed to create source file 1")
		defer os.Remove(sourceFile1)

		// Create second source file with id: link
		sourceFile2 := "testdata/backlink-source2.org"
		absSourceFile2, _ := filepath.Abs(sourceFile2)
		sourceContent2 := `* Source File 2
Different file with [[id:11111111-1111-1111-1111-111111111111][another reference]].`

		err = os.WriteFile(sourceFile2, []byte(sourceContent2), 0644)
		require.NoError(t, err, "Failed to create source file 2")
		defer os.Remove(sourceFile2)

		// Re-scan to index new files
		didSaveParams := protocol.DidSaveTextDocumentParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: protocol.DocumentUri("file://" + absTargetFile),
			},
		}
		if err := jsonrpcConn.Notify(ctx, "textDocument/didSave", didSaveParams); err != nil {
			t.Fatalf("Failed to send didSave for target: %v", err)
		}

		time.Sleep(200 * time.Millisecond) // Wait for re-scan

		// Open target document
		didOpenParams := protocol.DidOpenTextDocumentParams{
			TextDocument: protocol.TextDocumentItem{
				URI:        protocol.DocumentUri("file://" + absTargetFile),
				LanguageID: "org",
				Version:    1,
				Text:       targetContent,
			},
		}
		if err := jsonrpcConn.Notify(ctx, "textDocument/didOpen", didOpenParams); err != nil {
			t.Fatalf("Failed to open target file: %v", err)
		}

		time.Sleep(100 * time.Millisecond)

		// Request references from the target headline (line 0, the heading)
		referenceParams := protocol.ReferenceParams{
			TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: protocol.DocumentUri("file://" + absTargetFile),
				},
				Position: protocol.Position{
					Line:      0, // On the heading
					Character: 5,
				},
			},
			Context: protocol.ReferenceContext{
				IncludeDeclaration: false,
			},
		}

		var result []protocol.Location
		err = jsonrpcConn.Call(ctx, "textDocument/references", referenceParams, &result)
		require.NoError(t, err, "References request failed")
		require.NotNil(t, result, "Expected non-nil references result")
		require.Len(t, result, 3, "Expected 3 references (2 in source1, 1 in source2)")

		// Verify references are from the expected files
		sourceURIs := make(map[string]bool)
		for _, loc := range result {
			sourceURIs[string(loc.URI)] = true
		}

		require.Contains(t, sourceURIs, "file://"+absSourceFile1, "Should have reference from source1")
		require.Contains(t, sourceURIs, "file://"+absSourceFile2, "Should have reference from source2")
		t.Logf("✅ Backlinks successful! Found %d references from %d files", len(result), len(sourceURIs))
	})

	// Test 7: Hover with non-link text (should return nil)
	t.Run("HoverNoLink", func(t *testing.T) {
		t.Log("Testing hover on non-link text...")

		sourceFile := "testdata/hover-no-link.org"
		absSourceFile, _ := filepath.Abs(sourceFile)
		sourceContent := "* Heading\nJust regular text without links."

		err = os.WriteFile(sourceFile, []byte(sourceContent), 0644)
		require.NoError(t, err, "Failed to create test file")
		defer os.Remove(sourceFile)

		// Open document
		didOpenParams := protocol.DidOpenTextDocumentParams{
			TextDocument: protocol.TextDocumentItem{
				URI:        protocol.DocumentUri("file://" + absSourceFile),
				LanguageID: "org",
				Version:    1,
				Text:       sourceContent,
			},
		}
		if err := jsonrpcConn.Notify(ctx, "textDocument/didOpen", didOpenParams); err != nil {
			t.Fatalf("Failed to send didOpen: %v", err)
		}

		time.Sleep(100 * time.Millisecond)

		// Request hover on regular text
		hoverParams := protocol.HoverParams{
			TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: protocol.DocumentUri("file://" + absSourceFile),
				},
				Position: protocol.Position{
					Line:      1,
					Character: 5,
				},
			},
		}

		var result *protocol.Hover
		err = jsonrpcConn.Call(ctx, "textDocument/hover", hoverParams, &result)
		require.NoError(t, err, "Hover request failed")
		require.Nil(t, result, "Expected nil hover for non-link text")
	})

	// Test 8: Completion (IDs & Tags)
	t.Run("Completion", func(t *testing.T) {
		t.Log("Testing completion for IDs and tags...")

		// Create test files with UUIDs and tags
		targetFile := "testdata/completion-target.org"
		absTargetFile, _ := filepath.Abs(targetFile)
		targetContent := `* Target Heading :testtag:anothertag:
	:PROPERTIES:
	:ID:       22222222-2222-2222-2222-222222222222
	:END:
	Content here.`

		err := os.WriteFile(targetFile, []byte(targetContent), 0644)
		require.NoError(t, err, "Failed to create target file")
		defer os.Remove(targetFile)

		// Re-scan to index the new file
		didSaveParams := protocol.DidSaveTextDocumentParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: protocol.DocumentUri("file://" + absTargetFile),
			},
		}
		if err := jsonrpcConn.Notify(ctx, "textDocument/didSave", didSaveParams); err != nil {
			t.Fatalf("Failed to send didSave for target: %v", err)
		}

		time.Sleep(200 * time.Millisecond) // Wait for re-scan

		// Test ID completion - file must have "[[id:" to trigger it
		idSourceFile := "testdata/completion-id-source.org"
		absIDSourceFile, _ := filepath.Abs(idSourceFile)
		idSourceContent := "* Source Heading\nSome text with [[id:" // Cursor goes after "[[id:"

		err = os.WriteFile(idSourceFile, []byte(idSourceContent), 0644)
		require.NoError(t, err, "Failed to create ID source file")
		defer os.Remove(idSourceFile)

		// Create tag source file for tag completion test
		tagSourceFile := "testdata/completion-tag-source.org"
		absTagSourceFile, _ := filepath.Abs(tagSourceFile)
		tagSourceContent := "* Source Heading :" // Cursor goes after ":" for tag completion

		err = os.WriteFile(tagSourceFile, []byte(tagSourceContent), 0644)
		require.NoError(t, err, "Failed to create tag source file")
		defer os.Remove(tagSourceFile)

		// Find cursor positions using helper
		idLine, idChar := findCursorPosition(idSourceContent, "[[id:")
		tagLine, tagChar := findCursorPosition(tagSourceContent, ":")

		// Open ID document
		didOpenParams := protocol.DidOpenTextDocumentParams{
			TextDocument: protocol.TextDocumentItem{
				URI:        protocol.DocumentUri("file://" + absIDSourceFile),
				LanguageID: "org",
				Version:    1,
				Text:       idSourceContent,
			},
		}
		if err := jsonrpcConn.Notify(ctx, "textDocument/didOpen", didOpenParams); err != nil {
			t.Fatalf("Failed to open ID source file: %v", err)
		}

		time.Sleep(100 * time.Millisecond)

		// Test ID completion - cursor positioned AFTER "id:"
		t.Log("Testing ID completion...")
		idCompletionParams := protocol.CompletionParams{
			TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: protocol.DocumentUri("file://" + absIDSourceFile),
				},
				Position: protocol.Position{
					Line:      idLine,
					Character: idChar,
				},
			},
		}

		var completionResult *protocol.CompletionList
		err = jsonrpcConn.Call(ctx, "textDocument/completion", idCompletionParams, &completionResult)
		require.NoError(t, err, "Completion request failed")
		require.NotNil(t, completionResult, "Expected completion result, got nil - document may not be in OpenDocs")

		// Find ID completion items
		var idItems []protocol.CompletionItem
		for _, item := range completionResult.Items {
			if item.Kind != nil && *item.Kind == protocol.CompletionItemKindReference {
				idItems = append(idItems, item)
			}
		}
		require.NotEmpty(t, idItems, "Expected ID completion items")

		// Find our test UUID - completion now shows heading titles, inserts UUIDs
		foundTestUUID := false
		for _, item := range idItems {
			if item.InsertText != nil && strings.HasPrefix(*item.InsertText, "22222222-2222-2222-2222-222222222222") {
				foundTestUUID = true
				// Label should be the heading title, not the UUID
				require.Equal(t, "Target Heading", item.Label, "Expected heading title as label")
				break
			}
		}
		require.True(t, foundTestUUID, "Expected to find test UUID in completion")

		// Test tag completion in headline - need to open tag file
		t.Log("Testing tag completion...")
		tagDidOpenParams := protocol.DidOpenTextDocumentParams{
			TextDocument: protocol.TextDocumentItem{
				URI:        protocol.DocumentUri("file://" + absTagSourceFile),
				LanguageID: "org",
				Version:    1,
				Text:       tagSourceContent,
			},
		}
		if err := jsonrpcConn.Notify(ctx, "textDocument/didOpen", tagDidOpenParams); err != nil {
			t.Fatalf("Failed to open tag source file: %v", err)
		}

		time.Sleep(100 * time.Millisecond)

		tagCompletionParams := protocol.CompletionParams{
			TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: protocol.DocumentUri("file://" + absTagSourceFile),
				},
				Position: protocol.Position{
					Line:      tagLine,
					Character: tagChar,
				},
			},
		}

		err = jsonrpcConn.Call(ctx, "textDocument/completion", tagCompletionParams, &completionResult)
		require.NoError(t, err, "Tag completion request failed")
		require.NotNil(t, completionResult, "Expected tag completion result")

		// Find tag completion items
		var tagItems []protocol.CompletionItem
		for _, item := range completionResult.Items {
			if item.Kind != nil && *item.Kind == protocol.CompletionItemKindProperty {
				tagItems = append(tagItems, item)
			}
		}
		require.NotEmpty(t, tagItems, "Expected tag completion items")

		// Check for our test tags
		foundTestTag := false
		foundAnotherTag := false
		for _, item := range tagItems {
			if item.Label == "testtag" {
				foundTestTag = true
				break
			}
		}
		for _, item := range tagItems {
			if item.Label == "anothertag" {
				foundAnotherTag = true
				break
			}
		}
		require.True(t, foundTestTag && foundAnotherTag, "Expected to find test tags in completion")

		// Test bracket closing for [[id: context
		t.Log("Testing bracket closing in [[id: context...")
		bracketSourceFile := "testdata/completion-bracket-source.org"
		absBracketSourceFile, _ := filepath.Abs(bracketSourceFile)
		bracketSourceContent := "* Bracket Test\nSome text [[id:"

		err = os.WriteFile(bracketSourceFile, []byte(bracketSourceContent), 0644)
		require.NoError(t, err, "Failed to create bracket source file")
		defer os.Remove(bracketSourceFile)

		// Find cursor position after "[[id:"
		bracketLine, bracketChar := findCursorPosition(bracketSourceContent, "[[id:")

		// Open bracket document
		bracketDidOpenParams := protocol.DidOpenTextDocumentParams{
			TextDocument: protocol.TextDocumentItem{
				URI:        protocol.DocumentUri("file://" + absBracketSourceFile),
				LanguageID: "org",
				Version:    1,
				Text:       bracketSourceContent,
			},
		}
		if err := jsonrpcConn.Notify(ctx, "textDocument/didOpen", bracketDidOpenParams); err != nil {
			t.Fatalf("Failed to open bracket source file: %v", err)
		}

		time.Sleep(100 * time.Millisecond)

		bracketCompletionParams := protocol.CompletionParams{
			TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: protocol.DocumentUri("file://" + absBracketSourceFile),
				},
				Position: protocol.Position{
					Line:      bracketLine,
					Character: bracketChar,
				},
			},
		}

		err = jsonrpcConn.Call(ctx, "textDocument/completion", bracketCompletionParams, &completionResult)
		require.NoError(t, err, "Bracket completion request failed")
		require.NotNil(t, completionResult, "Expected bracket completion result")

		// Verify bracket completion items have closing brackets when none exist
		foundBracketItem := false
		for _, item := range completionResult.Items {
			if item.Kind != nil && *item.Kind == protocol.CompletionItemKindReference {
				foundBracketItem = true
				// Should have ]] suffix
				require.NotNil(t, item.InsertText, "Expected InsertText")
				require.True(t, strings.HasSuffix(*item.InsertText, "]]"),
					"Expected InsertText to end with ]], got %s", *item.InsertText)
				break
			}
		}
		require.True(t, foundBracketItem, "Expected ID completion items with bracket closing")

		// Test NO bracket closing when closing brackets already exist
		t.Log("Testing no bracket closing when ]] already exists...")
		existingBracketSourceFile := "testdata/completion-existing-bracket-source.org"
		absExistingBracketSourceFile, _ := filepath.Abs(existingBracketSourceFile)
		existingBracketSourceContent := "* Existing Bracket Test\nSome text [[id:]] more"

		err = os.WriteFile(existingBracketSourceFile, []byte(existingBracketSourceContent), 0644)
		require.NoError(t, err, "Failed to create existing bracket source file")
		defer os.Remove(existingBracketSourceFile)

		// Find cursor position after "[[id:" but before "]]"
		existingBracketLine, existingBracketChar := findCursorPosition(existingBracketSourceContent, "[[id:")

		// Open existing bracket document
		existingBracketDidOpenParams := protocol.DidOpenTextDocumentParams{
			TextDocument: protocol.TextDocumentItem{
				URI:        protocol.DocumentUri("file://" + absExistingBracketSourceFile),
				LanguageID: "org",
				Version:    1,
				Text:       existingBracketSourceContent,
			},
		}
		if err := jsonrpcConn.Notify(ctx, "textDocument/didOpen", existingBracketDidOpenParams); err != nil {
			t.Fatalf("Failed to open existing bracket source file: %v", err)
		}

		time.Sleep(100 * time.Millisecond)

		existingBracketCompletionParams := protocol.CompletionParams{
			TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: protocol.DocumentUri("file://" + absExistingBracketSourceFile),
				},
				Position: protocol.Position{
					Line:      existingBracketLine,
					Character: existingBracketChar,
				},
			},
		}

		err = jsonrpcConn.Call(ctx, "textDocument/completion", existingBracketCompletionParams, &completionResult)
		require.NoError(t, err, "Existing bracket completion request failed")

		if completionResult != nil && len(completionResult.Items) > 0 {
			// Verify bracket completion items do NOT have closing brackets when they already exist
			for _, item := range completionResult.Items {
				if item.Kind != nil && *item.Kind == protocol.CompletionItemKindReference {
					require.NotNil(t, item.InsertText, "Expected InsertText")
					require.False(t, strings.HasSuffix(*item.InsertText, "]]"),
						"Expected InsertText to NOT end with ]] when brackets already exist, got %s", *item.InsertText)
					break
				}
			}
		}

		// Test filtering by header title
		t.Log("Testing filtering by header title...")
		filterSourceFile := "testdata/completion-filter-source.org"
		absFilterSourceFile, _ := filepath.Abs(filterSourceFile)
		filterSourceContent := "* Filter Test\nSome text [[id:Target" // Filter for "Target"

		err = os.WriteFile(filterSourceFile, []byte(filterSourceContent), 0644)
		require.NoError(t, err, "Failed to create filter source file")
		defer os.Remove(filterSourceFile)

		// Find cursor position after "[[id:Target"
		filterLine, filterChar := findCursorPosition(filterSourceContent, "[[id:Target")

		// Open filter document
		filterDidOpenParams := protocol.DidOpenTextDocumentParams{
			TextDocument: protocol.TextDocumentItem{
				URI:        protocol.DocumentUri("file://" + absFilterSourceFile),
				LanguageID: "org",
				Version:    1,
				Text:       filterSourceContent,
			},
		}
		if err := jsonrpcConn.Notify(ctx, "textDocument/didOpen", filterDidOpenParams); err != nil {
			t.Fatalf("Failed to open filter source file: %v", err)
		}

		time.Sleep(100 * time.Millisecond)

		filterCompletionParams := protocol.CompletionParams{
			TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: protocol.DocumentUri("file://" + absFilterSourceFile),
				},
				Position: protocol.Position{
					Line:      filterLine,
					Character: filterChar,
				},
			},
		}

		err = jsonrpcConn.Call(ctx, "textDocument/completion", filterCompletionParams, &completionResult)
		require.NoError(t, err, "Filter completion request failed")
		require.NotNil(t, completionResult, "Expected filter completion result")

		// Verify filtering works - should find Target Heading
		foundFilteredItem := false
		for _, item := range completionResult.Items {
			if item.Label == "Target Heading" {
				foundFilteredItem = true
				break
			}
		}
		// Note: This test may need adjustment based on actual workspace content
		t.Logf("Filter test found %d items", len(completionResult.Items))
		if len(completionResult.Items) > 0 {
			t.Logf("First item: Label=%s", completionResult.Items[0].Label)
		}
		require.True(t, foundFilteredItem, "Expected to find 'Target Heading' when filtering by 'Target'")

		// Test NO completion without [[id: prefix
		t.Log("Testing no completion without [[id: prefix...")
		noPrefixSourceFile := "testdata/completion-noprefix-source.org"
		absNoPrefixSourceFile, _ := filepath.Abs(noPrefixSourceFile)
		noPrefixSourceContent := "* No Prefix Test\nSome text with id:" // Has id: but not [[id:

		err = os.WriteFile(noPrefixSourceFile, []byte(noPrefixSourceContent), 0644)
		require.NoError(t, err, "Failed to create no-prefix source file")
		defer os.Remove(noPrefixSourceFile)

		// Find cursor position at end of "id:"
		noPrefixLine, noPrefixChar := findCursorPosition(noPrefixSourceContent, "id:")

		// Open no-prefix document
		noPrefixDidOpenParams := protocol.DidOpenTextDocumentParams{
			TextDocument: protocol.TextDocumentItem{
				URI:        protocol.DocumentUri("file://" + absNoPrefixSourceFile),
				LanguageID: "org",
				Version:    1,
				Text:       noPrefixSourceContent,
			},
		}
		if err := jsonrpcConn.Notify(ctx, "textDocument/didOpen", noPrefixDidOpenParams); err != nil {
			t.Fatalf("Failed to open no-prefix source file: %v", err)
		}

		time.Sleep(100 * time.Millisecond)

		noPrefixCompletionParams := protocol.CompletionParams{
			TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: protocol.DocumentUri("file://" + absNoPrefixSourceFile),
				},
				Position: protocol.Position{
					Line:      noPrefixLine,
					Character: noPrefixChar,
				},
			},
		}

		err = jsonrpcConn.Call(ctx, "textDocument/completion", noPrefixCompletionParams, &completionResult)
		require.NoError(t, err, "No-prefix completion request failed")
		// Without [[id: prefix, should get nil or empty result
		if completionResult != nil {
			// Check if we got any ID link items - we shouldn't have any
			for _, item := range completionResult.Items {
				if item.Kind != nil && *item.Kind == protocol.CompletionItemKindReference {
					t.Error("Expected no ID completion items without [[id: prefix")
					break
				}
			}
		}

		// Test NO completion with just [[ prefix (no id:)
		t.Log("Testing no completion with just [[ prefix...")
		justBracketSourceFile := "testdata/completion-just-bracket-source.org"
		absJustBracketSourceFile, _ := filepath.Abs(justBracketSourceFile)
		justBracketSourceContent := "* Just Bracket Test\nSome text with [[" // Has [[ but not [[id:

		err = os.WriteFile(justBracketSourceFile, []byte(justBracketSourceContent), 0644)
		require.NoError(t, err, "Failed to create just-bracket source file")
		defer os.Remove(justBracketSourceFile)

		// Find cursor position at end of "[["
		justBracketLine, justBracketChar := findCursorPosition(justBracketSourceContent, "[[")

		// Open just-bracket document
		justBracketDidOpenParams := protocol.DidOpenTextDocumentParams{
			TextDocument: protocol.TextDocumentItem{
				URI:        protocol.DocumentUri("file://" + absJustBracketSourceFile),
				LanguageID: "org",
				Version:    1,
				Text:       justBracketSourceContent,
			},
		}
		if err := jsonrpcConn.Notify(ctx, "textDocument/didOpen", justBracketDidOpenParams); err != nil {
			t.Fatalf("Failed to open just-bracket source file: %v", err)
		}

		time.Sleep(100 * time.Millisecond)

		justBracketCompletionParams := protocol.CompletionParams{
			TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: protocol.DocumentUri("file://" + absJustBracketSourceFile),
				},
				Position: protocol.Position{
					Line:      justBracketLine,
					Character: justBracketChar,
				},
			},
		}

		err = jsonrpcConn.Call(ctx, "textDocument/completion", justBracketCompletionParams, &completionResult)
		require.NoError(t, err, "Just-bracket completion request failed")
		// With [[ but no id:, should get nil or empty result
		if completionResult != nil {
			// Check if we got any ID link items - we shouldn't have any
			for _, item := range completionResult.Items {
				if item.Kind != nil && *item.Kind == protocol.CompletionItemKindReference {
					t.Error("Expected no ID completion items with just [[ prefix (need [[id:)")
					break
				}
			}
		}
	})

	// Test 4: Enhanced References (cursor on ID link itself)
	t.Run("EnhancedReferencesFromIDLink", func(t *testing.T) {
		t.Log("Testing enhanced references - finding references from ID link...")

		// Create target file with UUID
		targetFile := "testdata/enhanced-ref-target.org"
		absTargetFile, _ := filepath.Abs(targetFile)
		targetContent := `* Target Heading
:PROPERTIES:
:ID:       33333333-3333-3333-3333-333333333333
:END:
This is the target.`

		err = os.WriteFile(targetFile, []byte(targetContent), 0644)
		require.NoError(t, err, "Failed to create target file")
		defer os.Remove(targetFile)

		// Create source file 1 with ID link - cursor will be placed here
		sourceFile1 := "testdata/enhanced-ref-source1.org"
		absSourceFile1, _ := filepath.Abs(sourceFile1)
		sourceContent1 := `* Source File 1
This file has the [[id:33333333-3333-3333-3333-333333333333][target link]] we'll query from.`

		err = os.WriteFile(sourceFile1, []byte(sourceContent1), 0644)
		require.NoError(t, err, "Failed to create source file 1")
		defer os.Remove(sourceFile1)

		// Create source file 2 with another reference to same UUID
		sourceFile2 := "testdata/enhanced-ref-source2.org"
		absSourceFile2, _ := filepath.Abs(sourceFile2)
		sourceContent2 := `* Source File 2
Another reference to [[id:33333333-3333-3333-3333-333333333333]].`

		err = os.WriteFile(sourceFile2, []byte(sourceContent2), 0644)
		require.NoError(t, err, "Failed to create source file 2")
		defer os.Remove(sourceFile2)

		// Re-scan to index new files
		didSaveParams := protocol.DidSaveTextDocumentParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: protocol.DocumentUri("file://" + absTargetFile),
			},
		}
		if err := jsonrpcConn.Notify(ctx, "textDocument/didSave", didSaveParams); err != nil {
			t.Fatalf("Failed to send didSave for target: %v", err)
		}

		time.Sleep(200 * time.Millisecond)

		// Open source file 1 where the ID link is
		didOpenParams := protocol.DidOpenTextDocumentParams{
			TextDocument: protocol.TextDocumentItem{
				URI:        protocol.DocumentUri("file://" + absSourceFile1),
				LanguageID: "org",
				Version:    1,
				Text:       sourceContent1,
			},
		}
		if err := jsonrpcConn.Notify(ctx, "textDocument/didOpen", didOpenParams); err != nil {
			t.Fatalf("Failed to open source file 1: %v", err)
		}

		time.Sleep(100 * time.Millisecond)

		// Find cursor position on the ID link itself
		// Place cursor right after "[[id:" to be on the link
		idLinkLine, idLinkChar := findCursorPosition(sourceContent1, "[[id:")

		// Request references from the ID link itself (not from headline)
		referenceParams := protocol.ReferenceParams{
			TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: protocol.DocumentUri("file://" + absSourceFile1),
				},
				Position: protocol.Position{
					Line:      idLinkLine,
					Character: idLinkChar + 5, // Inside the UUID part of the link
				},
			},
			Context: protocol.ReferenceContext{
				IncludeDeclaration: false,
			},
		}

		var result []protocol.Location
		err = jsonrpcConn.Call(ctx, "textDocument/references", referenceParams, &result)
		require.NoError(t, err, "References request failed")
		require.NotNil(t, result, "Expected non-nil references result")

		// Should find 2 references: one in source1 (the link we're on), one in source2
		// Note: the link we're querying from should be included in results
		require.Len(t, result, 2, "Expected 2 references (1 in source1, 1 in source2)")

		// Verify references are from the expected files
		sourceURIs := make(map[string]bool)
		for _, loc := range result {
			sourceURIs[string(loc.URI)] = true
		}

		require.Contains(t, sourceURIs, "file://"+absSourceFile1, "Should have reference from source1")
		require.Contains(t, sourceURIs, "file://"+absSourceFile2, "Should have reference from source2")
		t.Logf("✅ Enhanced references successful! Found %d references from %d files when cursor was on ID link", len(result), len(sourceURIs))
	})

	// Test 5: File Link Completion
	t.Run("FileLinkCompletion", func(t *testing.T) {
		t.Log("Testing file link completion...")

		// Create some test files that should appear in completion
		testFile1 := "testdata/completion-file-target1.org"
		absTestFile1, _ := filepath.Abs(testFile1)
		err = os.WriteFile(testFile1, []byte("* Test File 1\nContent here."), 0644)
		require.NoError(t, err, "Failed to create test file 1")
		defer os.Remove(testFile1)

		testFile2 := "testdata/completion-file-target2.org"
		err = os.WriteFile(testFile2, []byte("* Test File 2\nMore content."), 0644)
		require.NoError(t, err, "Failed to create test file 2")
		defer os.Remove(testFile2)

		// Re-scan to index new files
		didSaveParams := protocol.DidSaveTextDocumentParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: protocol.DocumentUri("file://" + absTestFile1),
			},
		}
		if err := jsonrpcConn.Notify(ctx, "textDocument/didSave", didSaveParams); err != nil {
			t.Fatalf("Failed to send didSave: %v", err)
		}
		time.Sleep(200 * time.Millisecond)

		// Create source file with [[file: prefix
		sourceFile := "testdata/completion-file-source.org"
		absSourceFile, _ := filepath.Abs(sourceFile)
		sourceContent := "* Source File\nLink to file: [[file:"

		err = os.WriteFile(sourceFile, []byte(sourceContent), 0644)
		require.NoError(t, err, "Failed to create source file")
		defer os.Remove(sourceFile)

		// Find cursor position after [[file:
		fileLine, fileChar := findCursorPosition(sourceContent, "[[file:")

		// Open source document
		didOpenParams := protocol.DidOpenTextDocumentParams{
			TextDocument: protocol.TextDocumentItem{
				URI:        protocol.DocumentUri("file://" + absSourceFile),
				LanguageID: "org",
				Version:    1,
				Text:       sourceContent,
			},
		}
		if err := jsonrpcConn.Notify(ctx, "textDocument/didOpen", didOpenParams); err != nil {
			t.Fatalf("Failed to open source file: %v", err)
		}
		time.Sleep(100 * time.Millisecond)

		// Test file link completion
		t.Log("Testing file link completion...")
		completionParams := protocol.CompletionParams{
			TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: protocol.DocumentUri("file://" + absSourceFile),
				},
				Position: protocol.Position{
					Line:      fileLine,
					Character: fileChar,
				},
			},
		}

		var completionResult *protocol.CompletionList
		err = jsonrpcConn.Call(ctx, "textDocument/completion", completionParams, &completionResult)
		require.NoError(t, err, "Completion request failed")
		require.NotNil(t, completionResult, "Expected completion result, got nil")

		// Should have file completion items
		var fileItems []protocol.CompletionItem
		for _, item := range completionResult.Items {
			if item.Kind != nil && *item.Kind == protocol.CompletionItemKindFile {
				fileItems = append(fileItems, item)
			}
		}
		require.NotEmpty(t, fileItems, "Expected file completion items")

		// Check that our test files are in the results
		foundFile1 := false
		foundFile2 := false
		for _, item := range fileItems {
			if strings.Contains(item.Label, "completion-file-target1.org") {
				foundFile1 = true
			}
			if strings.Contains(item.Label, "completion-file-target2.org") {
				foundFile2 = true
			}
		}
		require.True(t, foundFile1, "Expected to find completion-file-target1.org in file completions")
		require.True(t, foundFile2, "Expected to find completion-file-target2.org in file completions")
		t.Logf("✅ File link completion successful! Found %d file items", len(fileItems))
	})

	// Test 6: Block Type Completion
	t.Run("BlockTypeCompletion", func(t *testing.T) {
		t.Log("Testing block type completion...")

		// Create source file with #+begin_ prefix
		sourceFile := "testdata/completion-block-source.org"
		absSourceFile, _ := filepath.Abs(sourceFile)
		sourceContent := "#+begin_"

		err = os.WriteFile(sourceFile, []byte(sourceContent), 0644)
		require.NoError(t, err, "Failed to create source file")
		defer os.Remove(sourceFile)

		// Find cursor position after #+begin_
		blockLine, blockChar := findCursorPosition(sourceContent, "#+begin_")

		// Open source document
		didOpenParams := protocol.DidOpenTextDocumentParams{
			TextDocument: protocol.TextDocumentItem{
				URI:        protocol.DocumentUri("file://" + absSourceFile),
				LanguageID: "org",
				Version:    1,
				Text:       sourceContent,
			},
		}
		if err := jsonrpcConn.Notify(ctx, "textDocument/didOpen", didOpenParams); err != nil {
			t.Fatalf("Failed to open source file: %v", err)
		}
		time.Sleep(100 * time.Millisecond)

		// Test block type completion
		t.Log("Testing block type completion...")
		completionParams := protocol.CompletionParams{
			TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: protocol.DocumentUri("file://" + absSourceFile),
				},
				Position: protocol.Position{
					Line:      blockLine,
					Character: blockChar,
				},
			},
		}

		var completionResult *protocol.CompletionList
		err = jsonrpcConn.Call(ctx, "textDocument/completion", completionParams, &completionResult)
		require.NoError(t, err, "Completion request failed")
		require.NotNil(t, completionResult, "Expected completion result, got nil")

		// Should have block type completion items (keyword kind)
		var blockItems []protocol.CompletionItem
		for _, item := range completionResult.Items {
			if item.Kind != nil && *item.Kind == protocol.CompletionItemKindKeyword {
				blockItems = append(blockItems, item)
			}
		}
		require.NotEmpty(t, blockItems, "Expected block type completion items")

		// Check that we have the expected block types
		foundTypes := make(map[string]bool)
		for _, item := range blockItems {
			foundTypes[item.Label] = true
		}

		require.True(t, foundTypes["#+begin_quote"], "Expected '#+begin_quote' block type")
		require.True(t, foundTypes["#+begin_src"], "Expected '#+begin_src' block type")
		require.True(t, foundTypes["#+begin_verse"], "Expected '#+begin_verse' block type")
		t.Logf("✅ Block type completion successful! Found %d block types", len(blockItems))
	})

	// Test 7: Export Block Completion
	t.Run("ExportBlockCompletion", func(t *testing.T) {
		t.Log("Testing export block completion...")

		// Create source file with #+begin_export_ prefix
		sourceFile := "testdata/completion-export-source.org"
		absSourceFile, _ := filepath.Abs(sourceFile)
		sourceContent := "#+begin_export_"

		err = os.WriteFile(sourceFile, []byte(sourceContent), 0644)
		require.NoError(t, err, "Failed to create source file")
		defer os.Remove(sourceFile)

		// Find cursor position after #+begin_export_
		exportLine, exportChar := findCursorPosition(sourceContent, "#+begin_export_")

		// Open source document
		didOpenParams := protocol.DidOpenTextDocumentParams{
			TextDocument: protocol.TextDocumentItem{
				URI:        protocol.DocumentUri("file://" + absSourceFile),
				LanguageID: "org",
				Version:    1,
				Text:       sourceContent,
			},
		}
		if err := jsonrpcConn.Notify(ctx, "textDocument/didOpen", didOpenParams); err != nil {
			t.Fatalf("Failed to open source file: %v", err)
		}
		time.Sleep(100 * time.Millisecond)

		// Test export block completion
		t.Log("Testing export block completion...")
		completionParams := protocol.CompletionParams{
			TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: protocol.DocumentUri("file://" + absSourceFile),
				},
				Position: protocol.Position{
					Line:      exportLine,
					Character: exportChar,
				},
			},
		}

		var completionResult *protocol.CompletionList
		err = jsonrpcConn.Call(ctx, "textDocument/completion", completionParams, &completionResult)
		require.NoError(t, err, "Completion request failed")
		require.NotNil(t, completionResult, "Expected completion result, got nil")

		// Should have export block completion items (keyword kind)
		var exportItems []protocol.CompletionItem
		for _, item := range completionResult.Items {
			if item.Kind != nil && *item.Kind == protocol.CompletionItemKindKeyword {
				exportItems = append(exportItems, item)
			}
		}
		require.NotEmpty(t, exportItems, "Expected export block completion items")

		// Check that we have the expected export types
		foundTypes := make(map[string]bool)
		for _, item := range exportItems {
			foundTypes[item.Label] = true
		}

		require.True(t, foundTypes["#+begin_export_html"], "Expected '#+begin_export_html' export type")
		require.True(t, foundTypes["#+begin_export_latex"], "Expected '#+begin_export_latex' export type")
		t.Logf("✅ Export block completion successful! Found %d export types", len(exportItems))
	})

	// Test 9: Document Symbols
	t.Run("DocumentSymbols", func(t *testing.T) {
		t.Log("Testing document symbols...")

		// Create source file with nested headings
		sourceFile := "testdata/document-symbols.org"
		absSourceFile, _ := filepath.Abs(sourceFile)
		sourceContent := `* First Level Heading
Some content here.

** Second Level Heading
More content.

*** Third Level Heading
Deep content.

** Another Second Level
More stuff.

* Another First Level
Final content.
`

		err = os.WriteFile(sourceFile, []byte(sourceContent), 0644)
		require.NoError(t, err, "Failed to create source file")
		defer os.Remove(sourceFile)

		// Open source document
		didOpenParams := protocol.DidOpenTextDocumentParams{
			TextDocument: protocol.TextDocumentItem{
				URI:        protocol.DocumentUri("file://" + absSourceFile),
				LanguageID: "org",
				Version:    1,
				Text:       sourceContent,
			},
		}
		if err := jsonrpcConn.Notify(ctx, "textDocument/didOpen", didOpenParams); err != nil {
			t.Fatalf("Failed to open source file: %v", err)
		}
		time.Sleep(100 * time.Millisecond)

		// Request document symbols
		t.Log("Requesting document symbols...")
		symbolParams := protocol.DocumentSymbolParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: protocol.DocumentUri("file://" + absSourceFile),
			},
		}

		var symbols []protocol.DocumentSymbol
		err = jsonrpcConn.Call(ctx, "textDocument/documentSymbol", symbolParams, &symbols)
		require.NoError(t, err, "Document symbol request failed")
		require.NotNil(t, symbols, "Expected symbols result")
		require.NotEmpty(t, symbols, "Expected non-empty symbols")

		// Check top-level symbols
		require.GreaterOrEqual(t, len(symbols), 2, "Expected at least 2 top-level headings")

		// Find first level heading
		var foundFirstLevel bool
		var foundSecondLevel bool
		var foundThirdLevel bool

		for _, sym := range symbols {
			if sym.Name == "First Level Heading" {
				foundFirstLevel = true
				assert.Equal(t, protocol.SymbolKindNamespace, sym.Kind, "First level should be Namespace")
				assert.GreaterOrEqual(t, len(sym.Children), 2, "First level should have children")

				// Check children
				for _, child := range sym.Children {
					if child.Name == "Second Level Heading" {
						foundSecondLevel = true
						assert.Equal(t, protocol.SymbolKindClass, child.Kind, "Second level should be Class")
						assert.GreaterOrEqual(t, len(child.Children), 1, "Second level should have children")

						// Check grandchild
						for _, grandchild := range child.Children {
							if grandchild.Name == "Third Level Heading" {
								foundThirdLevel = true
								assert.Equal(t, protocol.SymbolKindMethod, grandchild.Kind, "Third level should be Method")
							}
						}
					}
				}
			}
		}

		require.True(t, foundFirstLevel, "Expected to find 'First Level Heading'")
		require.True(t, foundSecondLevel, "Expected to find 'Second Level Heading'")
		require.True(t, foundThirdLevel, "Expected to find 'Third Level Heading'")

		t.Logf("✅ Document symbols successful! Found %d top-level symbols", len(symbols))
	})

	// Test 9b: Workspace Symbols
	t.Run("WorkspaceSymbols", func(t *testing.T) {
		t.Log("Testing workspace symbols...")

		// Create first file with UUID headings
		file1 := "testdata/workspace-symbols-1.org"
		absFile1, _ := filepath.Abs(file1)
		content1 := `* Project Alpha :work:
:PROPERTIES:
:ID:       11111111-1111-1111-1111-111111111111
:END:
First project description.

* Meeting Notes
:PROPERTIES:
:ID:       22222222-2222-2222-2222-222222222222
:END:
Meeting content here.
** Action Items
Some actions.
`

		err = os.WriteFile(file1, []byte(content1), 0644)
		require.NoError(t, err, "Failed to create workspace file 1")
		defer os.Remove(file1)

		// Create second file with UUID headings
		file2 := "testdata/workspace-symbols-2.org"
		content2 := `* Project Beta :personal:
:PROPERTIES:
:ID:       33333333-3333-3333-3333-333333333333
:END:
Second project description.

* Shopping List
:PROPERTIES:
:ID:       44444444-4444-4444-4444-444444444444
:END:
Items to buy.
`

		err = os.WriteFile(file2, []byte(content2), 0644)
		require.NoError(t, err, "Failed to create workspace file 2")
		defer os.Remove(file2)

		// Trigger re-scan by saving one file to index UUIDs
		t.Log("Triggering re-scan to index UUIDs...")
		didSaveParams := protocol.DidSaveTextDocumentParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: protocol.DocumentUri("file://" + absFile1),
			},
		}
		if err := jsonrpcConn.Notify(ctx, "textDocument/didSave", didSaveParams); err != nil {
			t.Fatalf("Failed to notify save: %v", err)
		}
		time.Sleep(300 * time.Millisecond) // Wait for re-scan

		// Test 1: Empty query should return all symbols
		t.Log("Testing workspace symbols with empty query...")
		var allSymbols []protocol.SymbolInformation
		workspaceParams := protocol.WorkspaceSymbolParams{Query: ""}
		err = jsonrpcConn.Call(ctx, "workspace/symbol", workspaceParams, &allSymbols)
		require.NoError(t, err, "Workspace symbol request failed")
		require.NotNil(t, allSymbols, "Expected symbols result")
		require.GreaterOrEqual(t, len(allSymbols), 4, "Expected at least 4 indexed headings")

		// Verify we can find our specific headings
		var foundProjectAlpha, foundProjectBeta, foundMeeting, foundShopping bool
		for _, sym := range allSymbols {
			if sym.Name == "Project Alpha" {
				foundProjectAlpha = true
				assert.Equal(t, protocol.SymbolKindInterface, sym.Kind, "Workspace symbols should be Interface kind")
				assert.Contains(t, string(sym.Location.URI), "workspace-symbols-1.org")
			}
			if sym.Name == "Project Beta" {
				foundProjectBeta = true
				assert.Contains(t, string(sym.Location.URI), "workspace-symbols-2.org")
			}
			if sym.Name == "Meeting Notes" {
				foundMeeting = true
			}
			if sym.Name == "Shopping List" {
				foundShopping = true
			}
		}
		require.True(t, foundProjectAlpha, "Expected to find 'Project Alpha'")
		require.True(t, foundProjectBeta, "Expected to find 'Project Beta'")
		require.True(t, foundMeeting, "Expected to find 'Meeting Notes'")
		require.True(t, foundShopping, "Expected to find 'Shopping List'")

		// Test 2: Query filtering - search for "Project"
		t.Log("Testing workspace symbols with 'Project' query...")
		var projectSymbols []protocol.SymbolInformation
		workspaceParams = protocol.WorkspaceSymbolParams{Query: "Project"}
		err = jsonrpcConn.Call(ctx, "workspace/symbol", workspaceParams, &projectSymbols)
		require.NoError(t, err, "Workspace symbol query failed")
		require.NotEmpty(t, projectSymbols, "Expected filtered results for 'Project'")

		for _, sym := range projectSymbols {
			assert.Contains(t, strings.ToLower(sym.Name), "project", "All results should contain 'project'")
		}

		// Test 3: Query filtering - search for "shopping" (case insensitive)
		t.Log("Testing workspace symbols with 'shopping' query...")
		var shoppingSymbols []protocol.SymbolInformation
		workspaceParams = protocol.WorkspaceSymbolParams{Query: "shopping"}
		err = jsonrpcConn.Call(ctx, "workspace/symbol", workspaceParams, &shoppingSymbols)
		require.NoError(t, err, "Workspace symbol query failed")
		require.Len(t, shoppingSymbols, 1, "Expected exactly 1 result for 'shopping'")
		assert.Equal(t, "Shopping List", shoppingSymbols[0].Name)

		t.Logf("✅ Workspace symbols successful! Found %d total symbols, %d project matches", len(allSymbols), len(projectSymbols))
	})

	// Test 10: Code Actions - Heading to List conversion
	t.Run("CodeActionHeadingToList", func(t *testing.T) {
		t.Log("Testing code actions for heading to list conversion...")

		// Create test file with headings
		testFile := "testdata/codeaction-headings.org"
		absTestFile, _ := filepath.Abs(testFile)
		testContent := `* First Heading
Content for first heading.

* Second Heading
Content for second heading.
** Nested Heading
Nested content.
`

		err = os.WriteFile(testFile, []byte(testContent), 0644)
		require.NoError(t, err, "Failed to create test file")
		defer os.Remove(testFile)

		// Open the document
		didOpenParams := protocol.DidOpenTextDocumentParams{
			TextDocument: protocol.TextDocumentItem{
				URI:        protocol.DocumentUri("file://" + absTestFile),
				LanguageID: "org",
				Version:    1,
				Text:       testContent,
			},
		}
		if err := jsonrpcConn.Notify(ctx, "textDocument/didOpen", didOpenParams); err != nil {
			t.Fatalf("Failed to open test file: %v", err)
		}
		time.Sleep(100 * time.Millisecond)

		// Request code actions for heading range (lines 0-2, covering first heading)
		codeActionParams := protocol.CodeActionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: protocol.DocumentUri("file://" + absTestFile),
			},
			Range: protocol.Range{
				Start: protocol.Position{Line: 0, Character: 0},
				End:   protocol.Position{Line: 2, Character: 0},
			},
			Context: protocol.CodeActionContext{},
		}

		var codeActions []protocol.CodeAction
		err = jsonrpcConn.Call(ctx, "textDocument/codeAction", codeActionParams, &codeActions)
		require.NoError(t, err, "CodeAction request failed")

		// Should have at least 2 actions (ordered list and bullet list)
		require.GreaterOrEqual(t, len(codeActions), 2, "Expected at least 2 code actions for headings")

		// Find the heading conversion actions
		var foundOrderedList, foundBulletList bool
		for _, action := range codeActions {
			if action.Title == "Convert headings to ordered list" {
				foundOrderedList = true
				require.NotNil(t, action.Kind, "Action should have a kind")
				assert.Equal(t, protocol.CodeActionKindRefactorRewrite, *action.Kind)
				require.NotNil(t, action.Edit, "Action should have an edit")
				require.NotNil(t, action.Edit.Changes, "Edit should have changes")

				// Verify the transformation produces correct output
				changes := action.Edit.Changes
				edit, ok := changes[protocol.DocumentUri("file://"+absTestFile)]
				require.True(t, ok, "Expected changes for the document")
				require.GreaterOrEqual(t, len(edit), 1, "Expected at least one text edit")
				require.NotEmpty(t, edit[0].NewText, "Transformation should produce non-empty output")
				assert.Equal(t, "1. First Heading\n   Content for first heading.\n\n", edit[0].NewText, "Expected ordered list transformation output")
			}
			if action.Title == "Convert headings to bullet list" {
				foundBulletList = true
				require.NotNil(t, action.Kind, "Action should have a kind")
				assert.Equal(t, protocol.CodeActionKindRefactorRewrite, *action.Kind)

				// Verify the transformation produces correct output
				changes := action.Edit.Changes
				edit, ok := changes[protocol.DocumentUri("file://"+absTestFile)]
				require.True(t, ok, "Expected changes for the document")
				require.GreaterOrEqual(t, len(edit), 1, "Expected at least one text edit")
				require.NotEmpty(t, edit[0].NewText, "Expected non-empty transformation output")
				assert.Equal(t, "- First Heading\n  Content for first heading.\n\n", edit[0].NewText, "Expected bullet list transformation output")
			}
		}

		require.True(t, foundOrderedList, "Expected 'Convert headings to ordered list' action")
		require.True(t, foundBulletList, "Expected 'Convert headings to bullet list' action")

		t.Logf("✅ CodeAction heading to list conversion successful! Found %d actions", len(codeActions))
	})

	// Test 10b: Code Actions - List to Heading conversion
	t.Run("CodeActionListToHeading", func(t *testing.T) {
		t.Log("Testing code actions for list to heading conversion...")

		// Create test file with a list (no heading, to avoid parser line position issues)
		testFile := "testdata/codeaction-list.org"
		absTestFile, _ := filepath.Abs(testFile)
		testContent := `- First list item
- Second list item
  - Nested item
`

		err = os.WriteFile(testFile, []byte(testContent), 0644)
		require.NoError(t, err, "Failed to create test file")
		defer os.Remove(testFile)

		// Open the document
		didOpenParams := protocol.DidOpenTextDocumentParams{
			TextDocument: protocol.TextDocumentItem{
				URI:        protocol.DocumentUri("file://" + absTestFile),
				LanguageID: "org",
				Version:    1,
				Text:       testContent,
			},
		}
		if err := jsonrpcConn.Notify(ctx, "textDocument/didOpen", didOpenParams); err != nil {
			t.Fatalf("Failed to open test file: %v", err)
		}
		time.Sleep(100 * time.Millisecond)

		// Request code actions with cursor on the first list item
		// Note: The parser reports all nodes at line 0, so we use line 0
		codeActionParams := protocol.CodeActionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: protocol.DocumentUri("file://" + absTestFile),
			},
			Range: protocol.Range{
				Start: protocol.Position{Line: 2, Character: 5},
				End:   protocol.Position{Line: 2, Character: 5},
			},
			Context: protocol.CodeActionContext{},
		}

		var codeActions []protocol.CodeAction
		err = jsonrpcConn.Call(ctx, "textDocument/codeAction", codeActionParams, &codeActions)
		require.NoError(t, err, "CodeAction request failed")

		// Should have at least 1 action (list to heading conversion)
		require.GreaterOrEqual(t, len(codeActions), 1, "Expected at least 1 code action for list")

		// Find the list conversion action
		var foundListToHeading bool
		for _, action := range codeActions {
			if action.Title == "Convert list to headings" {
				foundListToHeading = true
				require.NotNil(t, action.Kind, "Action should have a kind")
				assert.Equal(t, protocol.CodeActionKindRefactorRewrite, *action.Kind)
				require.NotNil(t, action.Edit, "Action should have an edit")

				// Verify the transformation produces correct output
				changes := action.Edit.Changes
				key := protocol.DocumentUri("file://" + absTestFile)
				edit, ok := changes[key]
				require.True(t, ok, "Expected changes for the document")
				require.GreaterOrEqual(t, len(edit), 1, "Expected at least one text edit")
				require.NotEmpty(t, edit[0].NewText, "Expected non-empty transformation output")
				assert.Equal(t, "* Nested item\n", edit[0].NewText, "Expected list to heading transformation output")
			}
		}

		require.True(t, foundListToHeading, "Expected 'Convert list to headings' action")

		t.Logf("✅ CodeAction list to heading conversion successful! Found %d actions", len(codeActions))
	})

	// Test 10c: Code Actions - Code block evaluation
	t.Run("CodeActionCodeBlockEval", func(t *testing.T) {
		t.Log("Testing code actions for code block evaluation...")

		// Create test file with a code block
		testFile := "testdata/codeaction-codeblock.org"
		absTestFile, _ := filepath.Abs(testFile)
		testContent := `* Test Heading

#+begin_src python
print("hello")
#+end_src
`

		err = os.WriteFile(testFile, []byte(testContent), 0644)
		require.NoError(t, err, "Failed to create test file")
		defer os.Remove(testFile)

		// Open the document
		didOpenParams := protocol.DidOpenTextDocumentParams{
			TextDocument: protocol.TextDocumentItem{
				URI:        protocol.DocumentUri("file://" + absTestFile),
				LanguageID: "org",
				Version:    1,
				Text:       testContent,
			},
		}
		if err := jsonrpcConn.Notify(ctx, "textDocument/didOpen", didOpenParams); err != nil {
			t.Fatalf("Failed to open test file: %v", err)
		}
		time.Sleep(100 * time.Millisecond)

		// Request code actions with cursor on the #+begin_src line (line 2 in 0-indexed)
		codeActionParams := protocol.CodeActionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: protocol.DocumentUri("file://" + absTestFile),
			},
			Range: protocol.Range{
				Start: protocol.Position{Line: 2, Character: 10},
				End:   protocol.Position{Line: 2, Character: 10},
			},
			Context: protocol.CodeActionContext{},
		}

		var codeActions []protocol.CodeAction
		err = jsonrpcConn.Call(ctx, "textDocument/codeAction", codeActionParams, &codeActions)
		require.NoError(t, err, "CodeAction request failed")

		// Should have at least 1 action (evaluate code block)
		require.GreaterOrEqual(t, len(codeActions), 1, "Expected at least 1 code action for code block")

		// Find the code block evaluation action
		var foundEvalAction bool
		for _, action := range codeActions {
			if action.Title == "Evaluate python code block" {
				foundEvalAction = true
				require.NotNil(t, action.Kind, "Action should have a kind")
				assert.Equal(t, protocol.CodeActionKindQuickFix, *action.Kind)
				require.NotNil(t, action.Command, "Action should have a command")
				assert.Equal(t, "org.executeCodeBlock", action.Command.Command)
			}
		}

		require.True(t, foundEvalAction, "Expected 'Evaluate python code block' action")

		t.Logf("✅ CodeAction code block evaluation successful! Found %d actions", len(codeActions))
	})

	// Test 11: Shutdown
	t.Run("Shutdown", func(t *testing.T) {
		params := struct{}{}
		var result any
		err := jsonrpcConn.Call(ctx, "shutdown", &params, &result)
		require.NoError(t, err, "Shutdown failed")
		assert.Nil(t, result, "Expected nil result from shutdown")
	})

	// Exit notification to clean shutdown
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
