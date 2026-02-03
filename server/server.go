// Package server provides the LSP server implementation for org-mode files.
package server

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/alexispurslane/go-org/org"
	"github.com/alexispurslane/org-lsp/orgscanner"
	glsp "github.com/tliron/glsp"
	"github.com/tliron/glsp/server"
)

const (
	serverName = "org-lsp"
)

var serverVer = "0.0.1" // Must be var to take address for LSP protocol

// serverState holds the global server state (glsp.Context doesn't have State field)
var serverState *State

// New creates and returns a new LSP server instance.
func New() *server.Server {
	handler := protocol.Handler{
		Initialize:                      initialize,
		Initialized:                     initialized,
		Shutdown:                        shutdown,
		SetTrace:                        setTrace,
		WorkspaceDidChangeConfiguration: workspaceDidChangeConfiguration,
		WorkspaceSymbol:                 workspaceSymbol,
		WorkspaceExecuteCommand:         workspaceExecuteCommand,
		TextDocumentDidOpen:             textDocumentDidOpen,
		TextDocumentDidChange:           textDocumentDidChange,
		TextDocumentDidClose:            textDocumentDidClose,
		TextDocumentDidSave:             textDocumentDidSave,
		TextDocumentDefinition:          textDocumentDefinition,
		TextDocumentHover:               textDocumentHover,
		TextDocumentReferences:          textDocumentReferences,
		TextDocumentCompletion:          textDocumentCompletion,
		TextDocumentDocumentSymbol:      textDocumentDocumentSymbol,
		TextDocumentCodeAction:          textDocumentCodeAction,
	}
	slog.Debug("Handler created", "TextDocumentDefinition", handler.TextDocumentDefinition != nil, "TextDocumentHover", handler.TextDocumentHover != nil)
	return server.NewServer(&handler, serverName, false)
}

// initialize handles the LSP initialize request.
func initialize(context *glsp.Context, params *protocol.InitializeParams) (any, error) {
	// Configure logging level from environment
	logLevel := os.Getenv("ORG_LSP_LOG_LEVEL")
	level := slog.LevelDebug // default
	if logLevel != "" {
		switch logLevel {
		case "DEBUG":
			level = slog.LevelDebug
		case "INFO":
			level = slog.LevelInfo
		case "WARN":
			level = slog.LevelWarn
		case "ERROR":
			level = slog.LevelError
		}
	}
	slog.SetLogLoggerLevel(level)

	// Log client info and capabilities
	if params.ClientInfo != nil {
		slog.Info("🔌 Client connected", "name", params.ClientInfo.Name, "version", params.ClientInfo.Version)
	} else {
		slog.Info("🔌 Client connected (no client info)")
	}

	// Check workspace symbol client capabilities
	if params.Capabilities.Workspace != nil && params.Capabilities.Workspace.Symbol != nil {
		slog.Info("📋 Client workspace symbol capabilities", "dynamicRegistration", params.Capabilities.Workspace.Symbol.DynamicRegistration)
	} else {
		slog.Info("📋 Client has no workspace symbol capabilities")
	}

	serverState = &State{}
	serverState.OpenDocs = make(map[protocol.DocumentUri]*org.Document)
	serverState.DocVersions = make(map[protocol.DocumentUri]int32)
	serverState.RawContent = make(map[protocol.DocumentUri]string)

	if params.RootURI != nil && *params.RootURI != "" {
		// Convert URI to filesystem path
		serverState.OrgScanRoot = uriToPath(*params.RootURI)

		// Process org files from root directory
		slog.Info("Starting org file scan", "root", serverState.OrgScanRoot)
		serverState.Scanner = orgscanner.NewOrgScanner(serverState.OrgScanRoot)
		err := serverState.Scanner.Process()
		if err != nil {
			slog.Error("Failed to scan org files", "error", err)
		} else {
			fileCount := 0
			serverState.Scanner.ProcessedFiles.Files.Range(func(_, _ any) bool {
				fileCount++
				return true
			})
			slog.Info("Completed org file scan", "files_scanned", fileCount, "uuids_indexed", countUUIDs(serverState.Scanner.ProcessedFiles))
		}
	}

	// Helper pointers for LSP protocol (fields must be pointers for optionality)
	trueBool := true
	truePtr := &trueBool
	syncFull := protocol.TextDocumentSyncKindFull

	// MVP capabilities only per SPEC.md Phase 1
	capabilities := protocol.ServerCapabilities{
		TextDocumentSync: &protocol.TextDocumentSyncOptions{
			OpenClose: truePtr,
			Change:    &syncFull,
			Save: &protocol.SaveOptions{
				IncludeText: truePtr,
			},
		},
		HoverProvider:           truePtr,
		DefinitionProvider:      truePtr,
		ReferencesProvider:      truePtr,
		DocumentSymbolProvider:  truePtr,
		WorkspaceSymbolProvider: truePtr,
		CompletionProvider: &protocol.CompletionOptions{
			TriggerCharacters: []string{":", "_"},
		},
		CodeActionProvider: truePtr,
		ExecuteCommandProvider: &protocol.ExecuteCommandOptions{
			Commands: []string{"org.executeCodeBlock"},
		},
	}

	slog.Info("📤 Initialize response",
		"DefinitionProvider", capabilities.DefinitionProvider != nil,
		"HoverProvider", capabilities.HoverProvider != nil,
		"DocumentSymbolProvider", capabilities.DocumentSymbolProvider != nil,
		"WorkspaceSymbolProvider", capabilities.WorkspaceSymbolProvider != nil,
		"ReferencesProvider", capabilities.ReferencesProvider != nil,
		"CompletionProvider", capabilities.CompletionProvider != nil)
	return protocol.InitializeResult{
		Capabilities: capabilities,
		ServerInfo: &protocol.InitializeResultServerInfo{
			Name:    serverName,
			Version: &serverVer,
		},
	}, nil
}

// initialized handles the LSP initialized notification.
func initialized(context *glsp.Context, params *protocol.InitializedParams) error {
	return nil
}

// workspaceExecuteCommand handles workspace/executeCommand requests
func workspaceExecuteCommand(context *glsp.Context, params *protocol.ExecuteCommandParams) (any, error) {
	slog.Info("Executing command", "command", params.Command, "args", params.Arguments)

	// Dispatch to the appropriate command handler
	switch params.Command {
	case "org.executeCodeBlock":
		// Extract arguments: uri (string), line (int), column (int)
		if len(params.Arguments) != 3 {
			return nil, fmt.Errorf("org.executeCodeBlock requires 3 arguments: uri, line, column")
		}

		// Extract and cast the arguments
		uri, ok := params.Arguments[0].(string)
		if !ok {
			return nil, fmt.Errorf("invalid uri argument")
		}

		line, ok := params.Arguments[1].(float64)
		if !ok {
			// Try as int64 for glsp/protocol handling
			lineInt, okInt := params.Arguments[1].(int64)
			if !okInt {
				return nil, fmt.Errorf("invalid line argument")
			}
			line = float64(lineInt)
		}

		column, ok := params.Arguments[2].(float64)
		if !ok {
			// Try as int64 for glsp/protocol handling
			colInt, okInt := params.Arguments[2].(int64)
			if !okInt {
				return nil, fmt.Errorf("invalid column argument")
			}
			column = float64(colInt)
		}

		// Call ExecuteCodeBlock with the extracted arguments
		return ExecuteCodeBlock(protocol.DocumentUri(uri), int(line), int(column))
	default:
		return nil, fmt.Errorf("unknown command: %s", params.Command)
	}
}

// shutdown handles the LSP shutdown request.
func shutdown(context *glsp.Context) error {
	return nil
}

// setTrace handles the LSP $/setTrace request.
func setTrace(context *glsp.Context, params *protocol.SetTraceParams) error {
	protocol.SetTraceValue(params.Value)
	return nil
}

// workspaceDidChangeConfiguration handles the workspace/didChangeConfiguration notification.
// Silently ignored - we don't use configuration changes currently.
func workspaceDidChangeConfiguration(context *glsp.Context, params *protocol.DidChangeConfigurationParams) error {
	slog.Debug("Received workspace/didChangeConfiguration (ignored)")
	return nil
}

// textDocumentDidOpen handles the LSP textDocument/didOpen notification.
func textDocumentDidOpen(context *glsp.Context, params *protocol.DidOpenTextDocumentParams) error {
	slog.Debug("textDocument/didOpen handler called")
	if serverState == nil {
		slog.Error("Server state is nil in didOpen")
		return nil
	}

	uri := params.TextDocument.URI
	slog.Info("Opening document", "uri", uri, "version", params.TextDocument.Version, "textLength", len(params.TextDocument.Text))

	// Parse the document content
	text := params.TextDocument.Text
	doc := org.New().Parse(strings.NewReader(text), string(uri))

	serverState.OpenDocs[uri] = doc
	serverState.DocVersions[uri] = params.TextDocument.Version
	serverState.RawContent[uri] = text
	return nil
}

// textDocumentDidChange handles the LSP textDocument/didChange notification.
func textDocumentDidChange(context *glsp.Context, params *protocol.DidChangeTextDocumentParams) error {
	if serverState == nil {
		return nil
	}

	uri := params.TextDocument.URI
	slog.Info("Changing document", "uri", uri, "version", params.TextDocument.Version)

	// For MVP, we only support full document sync
	// Re-parse the entire document with the new content
	if len(params.ContentChanges) > 0 {
		change := params.ContentChanges[0]
		slog.Debug("Change received", "type", fmt.Sprintf("%T", change), "change", change)
		// Type assert to access Text field
		if changeEvent, ok := change.(protocol.TextDocumentContentChangeEventWhole); ok {
			slog.Debug("Type assertion succeeded for TextDocumentContentChangeEventWhole")
			text := changeEvent.Text
			slog.Debug("Document change received", "uri", uri, "textLen", len(text), "first100", text[:min(100, len(text))])

			doc := org.New().Parse(strings.NewReader(text), string(uri))

			serverState.OpenDocs[uri] = doc
			serverState.DocVersions[uri] = params.TextDocument.Version
			serverState.RawContent[uri] = text
			slog.Debug("RawContent updated", "uri", uri, "contentLen", len(text))
		} else {
			slog.Error("Type assertion failed for TextDocumentContentChangeEventWhole", "actualType", fmt.Sprintf("%T", change))
		}
	}

	return nil
}

// textDocumentDidClose handles the LSP textDocument/didClose notification.
func textDocumentDidClose(context *glsp.Context, params *protocol.DidCloseTextDocumentParams) error {
	if serverState == nil {
		return nil
	}

	uri := params.TextDocument.URI
	slog.Info("Closing document", "uri", uri)

	delete(serverState.OpenDocs, uri)
	delete(serverState.DocVersions, uri)
	delete(serverState.RawContent, uri)
	return nil
}

// textDocumentDidSave handles the LSP textDocument/didSave notification.
func textDocumentDidSave(context *glsp.Context, params *protocol.DidSaveTextDocumentParams) error {
	if serverState.Scanner != nil {
		slog.Info("Re-scanning org files on save", "file", params.TextDocument.URI)
		err := serverState.Scanner.Process()
		if err != nil {
			slog.Error("Failed to re-scan org files", "error", err)
		} else {
			fileCount := 0
			serverState.Scanner.ProcessedFiles.Files.Range(func(_, _ any) bool {
				fileCount++
				return true
			})
			slog.Info("Completed org file re-scan", "files_scanned", fileCount, "uuids_indexed", countUUIDs(serverState.Scanner.ProcessedFiles))
		}
	}
	return nil
}
