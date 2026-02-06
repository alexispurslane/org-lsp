// Package server provides the LSP server implementation for org-mode files.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	protocol "go.lsp.dev/protocol"

	"github.com/alexispurslane/go-org/org"
	"github.com/alexispurslane/org-lsp/orgscanner"
)

const (
	serverName = "org-lsp"
)

var serverVer = "0.0.1" // Must be var to take address for LSP protocol

// serverState holds the global server state (glsp.Context doesn't have State field)
var serverState *State

// ServerImpl implements the protocol.Server interface for org-lsp
type ServerImpl struct {
	// We'll reference the global serverState during migration
}

// New creates a new ServerImpl instance
func New() *ServerImpl {
	return &ServerImpl{}
}

////////////////////////// NEW GO.LSP.DEV STUBS

// ensure serverImpl implements protocol.Server interface
// Ensure ServerImpl implements protocol.Server interface
var _ protocol.Server = (*ServerImpl)(nil)

func (s *ServerImpl) Initialize(ctx context.Context, params *protocol.InitializeParams) (result *protocol.InitializeResult, err error) {
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
	serverState.OpenDocs = make(map[protocol.DocumentURI]*org.Document)
	serverState.DocVersions = make(map[protocol.DocumentURI]int32)
	serverState.RawContent = make(map[protocol.DocumentURI]string)

	// Check if RootURI is provided (it's a string in go.lsp.dev/protocol, not a pointer)
	if params.RootURI != "" {
		// Convert URI to filesystem path
		serverState.OrgScanRoot = uriToPath(string(params.RootURI))

		// Process org files from root directory
		slog.Info("Starting org file scan", "root", serverState.OrgScanRoot)
		serverState.Scanner = orgscanner.NewOrgScanner(serverState.OrgScanRoot)
		err := serverState.Scanner.Process()
		if err != nil {
			slog.Error("Failed to scan org files", "error", err)
			return nil, err
		} else {
			fileCount := 0
			serverState.Scanner.ProcessedFiles.Files.Range(func(_, _ any) bool {
				fileCount++
				return true
			})
			slog.Info("Completed org file scan", "files_scanned", fileCount, "uuids_indexed", countUUIDs(serverState.Scanner.ProcessedFiles))
		}
	}

	// MVP capabilities only per SPEC.md Phase 1
	capabilities := protocol.ServerCapabilities{
		TextDocumentSync: &protocol.TextDocumentSyncOptions{
			OpenClose: true,
			Change:    protocol.TextDocumentSyncKindFull,
			Save: &protocol.SaveOptions{
				IncludeText: true,
			},
		},
		HoverProvider:              true,
		DefinitionProvider:         true,
		DocumentFormattingProvider: true,
		ReferencesProvider:         true,
		DocumentSymbolProvider:     true,
		WorkspaceSymbolProvider:    true,
		FoldingRangeProvider:       true,
		CompletionProvider: &protocol.CompletionOptions{
			TriggerCharacters: []string{":", "_"},
		},
		CodeActionProvider: true,
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
		"DocumentFormattingProvider", capabilities.DocumentFormattingProvider != nil,
		"FoldingRangeProvider", capabilities.FoldingRangeProvider != nil,
		"CompletionProvider", capabilities.CompletionProvider != nil)
	return &protocol.InitializeResult{
		Capabilities: capabilities,
		ServerInfo: &protocol.ServerInfo{
			Name:    serverName,
			Version: serverVer,
		},
	}, nil
}

func (s *ServerImpl) Exit(ctx context.Context) (err error) {
	return nil
}

func (s *ServerImpl) Shutdown(ctx context.Context) error {
	return nil
}

func (s *ServerImpl) Initialized(ctx context.Context, params *protocol.InitializedParams) (err error) {
	slog.Info("Server initialized")
	return nil
}

func (s *ServerImpl) SetTrace(ctx context.Context, params *protocol.SetTraceParams) error {
	slog.Info("Set trace", "value", params.Value)
	return nil
}

func (s *ServerImpl) LogTrace(ctx context.Context, params *protocol.LogTraceParams) (err error) {
	return nil
}

func (s *ServerImpl) CodeLens(ctx context.Context, params *protocol.CodeLensParams) (result []protocol.CodeLens, err error) {
	return nil, nil
}
func (s *ServerImpl) CodeLensResolve(ctx context.Context, params *protocol.CodeLens) (result *protocol.CodeLens, err error) {
	return nil, nil
}
func (s *ServerImpl) ColorPresentation(ctx context.Context, params *protocol.ColorPresentationParams) (result []protocol.ColorPresentation, err error) {
	return nil, nil
}

func (s *ServerImpl) CompletionResolve(ctx context.Context, params *protocol.CompletionItem) (result *protocol.CompletionItem, err error) {
	return nil, nil
}

func (s *ServerImpl) Declaration(ctx context.Context, params *protocol.DeclarationParams) (result []protocol.Location /* Declaration | DeclarationLink[] | null */, err error) {
	return nil, nil
}

func (s *ServerImpl) DidChange(ctx context.Context, params *protocol.DidChangeTextDocumentParams) (err error) {
	if serverState == nil {
		return nil
	}
	serverState.Mu.Lock()
	defer serverState.Mu.Unlock()

	uri := params.TextDocument.URI
	slog.Info("Changing document", "uri", uri, "version", params.TextDocument.Version)

	// For MVP, we only support full document sync through ContentChanges
	if len(params.ContentChanges) > 0 {
		change := params.ContentChanges[0]
		slog.Debug("Change received", "change", change)

		// Check if this is a full document change (RangeLength == 0 indicates full doc)
		if change.RangeLength == 0 {
			// Full document sync
			text := change.Text
			slog.Debug("Document change received (full sync)", "uri", uri, "textLen", len(text))

			doc := org.New().Parse(strings.NewReader(text), string(uri))

			serverState.OpenDocs[uri] = doc
			serverState.DocVersions[uri] = params.TextDocument.Version
			serverState.RawContent[uri] = text
			slog.Debug("RawContent updated", "uri", uri, "contentLen", len(text))
		} else {
			slog.Warn("Incremental document changes not supported", "uri", uri)
		}
	}

	return nil
}
func (s *ServerImpl) DidChangeConfiguration(ctx context.Context, params *protocol.DidChangeConfigurationParams) (err error) {
	slog.Debug("Received workspace/didChangeConfiguration (ignored)")
	return nil
}

func (s *ServerImpl) DidChangeWatchedFiles(ctx context.Context, params *protocol.DidChangeWatchedFilesParams) (err error) {
	return nil
}

func (s *ServerImpl) DidChangeWorkspaceFolders(ctx context.Context, params *protocol.DidChangeWorkspaceFoldersParams) (err error) {
	return nil
}

func (s *ServerImpl) DidClose(ctx context.Context, params *protocol.DidCloseTextDocumentParams) (err error) {
	if serverState == nil {
		return nil
	}
	serverState.Mu.Lock()
	defer serverState.Mu.Unlock()

	uri := params.TextDocument.URI
	slog.Info("Closing document", "uri", uri)

	delete(serverState.OpenDocs, uri)
	delete(serverState.DocVersions, uri)
	delete(serverState.RawContent, uri)
	return nil
}

func (s *ServerImpl) DidOpen(ctx context.Context, params *protocol.DidOpenTextDocumentParams) (err error) {
	slog.Debug("textDocument/didOpen handler called")
	if serverState == nil {
		slog.Error("Server state is nil in didOpen")
		return nil
	}
	serverState.Mu.Lock()
	defer serverState.Mu.Unlock()

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

func (s *ServerImpl) DidSave(ctx context.Context, params *protocol.DidSaveTextDocumentParams) (err error) {
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

func (s *ServerImpl) DocumentColor(ctx context.Context, params *protocol.DocumentColorParams) (result []protocol.ColorInformation, err error) {
	return nil, nil
}

func (s *ServerImpl) DocumentHighlight(ctx context.Context, params *protocol.DocumentHighlightParams) (result []protocol.DocumentHighlight, err error) {
	return nil, nil
}
func (s *ServerImpl) DocumentLink(ctx context.Context, params *protocol.DocumentLinkParams) (result []protocol.DocumentLink, err error) {
	return nil, nil
}

func (s *ServerImpl) DocumentLinkResolve(ctx context.Context, params *protocol.DocumentLink) (result *protocol.DocumentLink, err error) {
	return nil, nil
}

func (s *ServerImpl) ExecuteCommand(ctx context.Context, params *protocol.ExecuteCommandParams) (result interface{}, err error) {
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
		return ExecuteCodeBlock(protocol.DocumentURI(uri), int(line), int(column))
	default:
		return nil, fmt.Errorf("unknown command: %s", params.Command)
	}
}

func (s *ServerImpl) Implementation(ctx context.Context, params *protocol.ImplementationParams) (result []protocol.Location, err error) {
	return nil, nil
}

func (s *ServerImpl) OnTypeFormatting(ctx context.Context, params *protocol.DocumentOnTypeFormattingParams) (result []protocol.TextEdit, err error) {
	return nil, nil
}

func (s *ServerImpl) PrepareRename(ctx context.Context, params *protocol.PrepareRenameParams) (result *protocol.Range, err error) {
	return nil, nil
}

func (s *ServerImpl) Rename(ctx context.Context, params *protocol.RenameParams) (result *protocol.WorkspaceEdit, err error) {
	return nil, nil
}

func (s *ServerImpl) SignatureHelp(ctx context.Context, params *protocol.SignatureHelpParams) (result *protocol.SignatureHelp, err error) {
	return nil, nil
}

func (s *ServerImpl) TypeDefinition(ctx context.Context, params *protocol.TypeDefinitionParams) (result []protocol.Location, err error) {
	return nil, nil
}

func (s *ServerImpl) WillSave(ctx context.Context, params *protocol.WillSaveTextDocumentParams) (err error) {
	return nil
}

func (s *ServerImpl) ShowDocument(ctx context.Context, params *protocol.ShowDocumentParams) (result *protocol.ShowDocumentResult, err error) {
	return nil, nil
}

func (s *ServerImpl) WillCreateFiles(ctx context.Context, params *protocol.CreateFilesParams) (result *protocol.WorkspaceEdit, err error) {
	return nil, nil
}

func (s *ServerImpl) DidCreateFiles(ctx context.Context, params *protocol.CreateFilesParams) (err error) {
	return nil
}

func (s *ServerImpl) WillRenameFiles(ctx context.Context, params *protocol.RenameFilesParams) (result *protocol.WorkspaceEdit, err error) {
	return nil, nil
}

func (s *ServerImpl) DidRenameFiles(ctx context.Context, params *protocol.RenameFilesParams) (err error) {
	return nil
}

func (s *ServerImpl) WillDeleteFiles(ctx context.Context, params *protocol.DeleteFilesParams) (result *protocol.WorkspaceEdit, err error) {
	return nil, nil
}

func (s *ServerImpl) DidDeleteFiles(ctx context.Context, params *protocol.DeleteFilesParams) (err error) {
	return nil
}

func (s *ServerImpl) CodeLensRefresh(ctx context.Context) (err error) {
	return nil
}

func (s *ServerImpl) PrepareCallHierarchy(ctx context.Context, params *protocol.CallHierarchyPrepareParams) (result []protocol.CallHierarchyItem, err error) {
	return nil, nil
}

func (s *ServerImpl) IncomingCalls(ctx context.Context, params *protocol.CallHierarchyIncomingCallsParams) (result []protocol.CallHierarchyIncomingCall, err error) {
	return nil, nil
}

func (s *ServerImpl) OutgoingCalls(ctx context.Context, params *protocol.CallHierarchyOutgoingCallsParams) (result []protocol.CallHierarchyOutgoingCall, err error) {
	return nil, nil
}

func (s *ServerImpl) SemanticTokensFull(ctx context.Context, params *protocol.SemanticTokensParams) (result *protocol.SemanticTokens, err error) {
	return nil, nil
}

func (s *ServerImpl) SemanticTokensFullDelta(ctx context.Context, params *protocol.SemanticTokensDeltaParams) (result interface{} /* SemanticTokens | SemanticTokensDelta */, err error) {
	return nil, nil
}

func (s *ServerImpl) SemanticTokensRange(ctx context.Context, params *protocol.SemanticTokensRangeParams) (result *protocol.SemanticTokens, err error) {
	return nil, nil
}

func (s *ServerImpl) SemanticTokensRefresh(ctx context.Context) (err error) {
	return nil
}

func (s *ServerImpl) LinkedEditingRange(ctx context.Context, params *protocol.LinkedEditingRangeParams) (result *protocol.LinkedEditingRanges, err error) {
	return nil, nil
}

func (s *ServerImpl) Moniker(ctx context.Context, params *protocol.MonikerParams) (result []protocol.Moniker, err error) {
	return nil, nil
}

func (s *ServerImpl) Request(ctx context.Context, method string, params interface{}) (result interface{}, err error) {
	return nil, nil
}

func (s *ServerImpl) WorkDoneProgressCancel(ctx context.Context, params *protocol.WorkDoneProgressCancelParams) (err error) {
	return nil
}

// LastScanTime returns the time when the scanner last completed a scan.
// This is used by tests to poll for indexing completion.
func (s *ServerImpl) LastScanTime() time.Time {
	if serverState == nil || serverState.Scanner == nil {
		return time.Time{}
	}
	return serverState.Scanner.GetLastScanTime()
}
