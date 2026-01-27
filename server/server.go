// Package server provides the LSP server implementation for org-mode files.
package server

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"reflect"
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

// State represents the server's runtime state and cached data.
type State struct {
	// OrgScanRoot is the root directory for org-mode file scanning.
	OrgScanRoot string
	// ProcessedFiles contains the scanned org files and indexes
	ProcessedFiles *orgscanner.ProcessedFiles
	// OpenDocs tracks currently open documents by URI
	OpenDocs map[protocol.DocumentUri]*org.Document
	// DocVersions tracks document version numbers by URI
	DocVersions map[protocol.DocumentUri]int32
}

// LinkNode represents a link node in the org-mode AST with its position
type LinkNode struct {
	Node     org.Node
	URL      string
	Protocol string
	Position org.Position
}

// New creates and returns a new LSP server instance.
func New() *server.Server {
	handler := protocol.Handler{
		Initialize:             initialize,
		Initialized:            initialized,
		Shutdown:               shutdown,
		SetTrace:               setTrace,
		TextDocumentDidOpen:    textDocumentDidOpen,
		TextDocumentDidChange:  textDocumentDidChange,
		TextDocumentDidClose:   textDocumentDidClose,
		TextDocumentDidSave:    textDocumentDidSave,
		TextDocumentDefinition: textDocumentDefinition,
	}
	return server.NewServer(&handler, serverName, false)
}

// initialize handles the LSP initialize request.
func initialize(context *glsp.Context, params *protocol.InitializeParams) (any, error) {
	serverState = &State{}
	serverState.OpenDocs = make(map[protocol.DocumentUri]*org.Document)
	serverState.DocVersions = make(map[protocol.DocumentUri]int32)

	if params.RootURI != nil && *params.RootURI != "" {
		// Convert URI to filesystem path (strip file:// prefix)
		rootURI := string(*params.RootURI)
		if len(rootURI) > 7 && rootURI[:7] == "file://" {
			serverState.OrgScanRoot = rootURI[7:]
		} else {
			serverState.OrgScanRoot = rootURI
		}

		// Process org files from root directory
		slog.Info("Starting org file scan", "root", serverState.OrgScanRoot)
		procFiles, err := orgscanner.Process(serverState.OrgScanRoot)
		if err != nil {
			slog.Error("Failed to scan org files", "error", err)
		} else {
			slog.Info("Completed org file scan", "files_scanned", len(procFiles.Files), "uuids_indexed", countUUIDs(procFiles))
			serverState.ProcessedFiles = procFiles
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
		HoverProvider:      truePtr,
		DefinitionProvider: truePtr,
		ReferencesProvider: truePtr,
		CompletionProvider: &protocol.CompletionOptions{
			TriggerCharacters: []string{":"},
		},
	}

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

// shutdown handles the LSP shutdown request.
func shutdown(context *glsp.Context) error {
	return nil
}

// setTrace handles the LSP $/setTrace request.
func setTrace(context *glsp.Context, params *protocol.SetTraceParams) error {
	protocol.SetTraceValue(params.Value)
	return nil
}

// textDocumentDidOpen handles the LSP textDocument/didOpen notification.
func textDocumentDidOpen(context *glsp.Context, params *protocol.DidOpenTextDocumentParams) error {
	if serverState == nil {
		return nil
	}

	uri := params.TextDocument.URI
	slog.Info("Opening document", "uri", uri, "version", params.TextDocument.Version)

	// Parse the document content
	doc := org.New().Parse(strings.NewReader(params.TextDocument.Text), string(uri))

	serverState.OpenDocs[uri] = doc
	serverState.DocVersions[uri] = params.TextDocument.Version
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
		// Type assert to access Text field
		if changeEvent, ok := change.(protocol.TextDocumentContentChangeEvent); ok {
			text := changeEvent.Text

			doc := org.New().Parse(strings.NewReader(text), string(uri))

			serverState.OpenDocs[uri] = doc
			serverState.DocVersions[uri] = params.TextDocument.Version
		} else {
			slog.Error("Failed to cast content change event", "uri", uri)
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
	return nil
}

// textDocumentDidSave handles the LSP textDocument/didSave notification.
func textDocumentDidSave(context *glsp.Context, params *protocol.DidSaveTextDocumentParams) error {
	if serverState.OrgScanRoot != "" {
		slog.Info("Re-scanning org files on save", "file", params.TextDocument.URI)
		procFiles, err := orgscanner.Process(serverState.OrgScanRoot)
		if err != nil {
			slog.Error("Failed to re-scan org files", "error", err)
		} else {
			slog.Info("Completed org file re-scan", "files_scanned", len(procFiles.Files), "uuids_indexed", countUUIDs(procFiles))
			serverState.ProcessedFiles = procFiles
		}
	}
	return nil
}

// textDocumentDefinition handles the LSP textDocument/definition request.
func textDocumentDefinition(context *glsp.Context, params *protocol.DefinitionParams) (any, error) {
	if serverState == nil {
		return nil, nil
	}

	uri := params.TextDocument.URI
	doc, found := serverState.OpenDocs[uri]
	if !found {
		slog.Debug("Document not in OpenDocs", "uri", uri)
		return nil, nil
	}

	// Find link at cursor position
	linkNode := findLinkAtPosition(doc, params.Position)
	if linkNode == nil {
		return nil, nil
	}

	// Resolve based on link type
	switch linkNode.Protocol {
	case "file":
		return resolveFileLink(uri, linkNode.URL)
	case "id":
		return resolveIDLink(linkNode.URL)
	}

	return nil, nil
}

// findLinkAtPosition searches the AST for a RegularLink node at the given position
func findLinkAtPosition(doc *org.Document, pos protocol.Position) *LinkNode {
	if doc == nil {
		return nil
	}

	targetLine := int(pos.Line) + 1     // Convert to 1-based for org.Position
	targetCol := int(pos.Character) + 1 // Convert to 1-based for org.Position

	slog.Info("Searching for link at position", "line", targetLine, "col", targetCol, "docNodes", len(doc.Nodes))

	var foundLink *LinkNode
	var depth int

	var walkNodes func(node org.Node, currentDepth int)
	walkNodes = func(node org.Node, currentDepth int) {
		depth = currentDepth
		if foundLink != nil {
			return // Already found, stop searching
		}

		// Use reflection to access Pos field on any node type
		nodeVal := reflect.ValueOf(node)
		var nodePos org.Position
		hasPos := false

		if nodeVal.Kind() == reflect.Struct {
			posField := nodeVal.FieldByName("Pos")
			if posField.IsValid() && posField.Type() == reflect.TypeFor[org.Position]() {
				nodePos = posField.Interface().(org.Position)
				hasPos = true
				slog.Info("Walking node", "depth", currentDepth, "type", fmt.Sprintf("%T", node), "pos", fmt.Sprintf("(%d,%d)-(%d,%d)", nodePos.StartLine, nodePos.StartColumn, nodePos.EndLine, nodePos.EndColumn))
			}
		}

		if !hasPos {
			slog.Info("Walking node without position", "depth", currentDepth, "type", fmt.Sprintf("%T", node))
			// Always walk children even without position info
			if children := getChildren(node); children != nil {
				slog.Info("Walking children for node without position", "depth", currentDepth, "type", fmt.Sprintf("%T", node), "childCount", len(children))
				for i, child := range children {
					slog.Info("Processing child", "depth", currentDepth, "childIndex", i, "childType", fmt.Sprintf("%T", child))
					walkNodes(child, currentDepth+1)
				}
			}
			return
		}

		// Check if cursor is within this node's range
		cursorInNode := targetLine >= nodePos.StartLine && targetLine <= nodePos.EndLine &&
			targetCol >= nodePos.StartColumn && targetCol <= nodePos.EndColumn

		if cursorInNode {
			// Check if this node is a RegularLink
			if link, ok := node.(org.RegularLink); ok {
				// Log link details
				slog.Info("Found RegularLink", "url", link.URL, "protocol", link.Protocol, "pos", fmt.Sprintf("(%d,%d)-(%d,%d)", link.Pos.StartLine, link.Pos.StartColumn, link.Pos.EndLine, link.Pos.EndColumn))

				slog.Info("Found link at cursor position", "url", link.URL, "protocol", link.Protocol)
				foundLink = &LinkNode{
					Node:     node,
					URL:      link.URL,
					Protocol: link.Protocol,
					Position: link.Pos,
				}
				return
			}

			// Not a link, but cursor is here - walk children to go deeper
			if children := getChildren(node); children != nil {
				slog.Info("Walking children for cursor-containing node", "depth", currentDepth, "type", fmt.Sprintf("%T", node), "childCount", len(children))
				for i, child := range children {
					slog.Info("Processing child", "depth", currentDepth, "childIndex", i, "childType", fmt.Sprintf("%T", child))
					walkNodes(child, currentDepth+1)
				}
			}
		}
	}

	// Walk all document nodes
	slog.Info("Starting AST walk", "topLevelNodeCount", len(doc.Nodes))
	for i, node := range doc.Nodes {
		slog.Info("Walking top-level node", "index", i, "type", fmt.Sprintf("%T", node))
		walkNodes(node, 0)
		if foundLink != nil {
			slog.Info("Found link, stopping walk early")
			break
		}
	}

	if foundLink == nil {
		slog.Info("No link found at position", "line", targetLine, "col", targetCol, "finalDepth", depth)
	}

	return foundLink
}

// getChildren extracts child nodes from a node if it has them
func getChildren(node org.Node) []org.Node {
	switch n := node.(type) {
	case org.Headline:
		return n.Children
	case org.Block:
		return n.Children
	case org.Paragraph:
		return n.Children
	case org.Emphasis:
		return n.Content
	case org.List:
		return n.Items
	case org.Drawer:
		return n.Children
	case interface{ GetNodes() []org.Node }:
		return n.GetNodes()
	default:
		return nil
	}
}

// resolveFileLink resolves a file: link to an absolute path
func resolveFileLink(currentURI protocol.DocumentUri, linkURL string) (any, error) {
	// Convert URI to path
	currentPath := string(currentURI)[7:] // Remove file:// prefix

	// Resolve relative to current document directory
	if !strings.HasPrefix(linkURL, "/") {
		currentDir := filepath.Dir(currentPath)
		linkURL = filepath.Join(currentDir, linkURL)
	}

	absPath, err := filepath.Abs(linkURL)
	if err != nil {
		return nil, nil
	}

	// Convert to file URI
	fileURI := "file://" + absPath

	// For MVP, return location pointing to start of file
	return protocol.Location{
		URI: protocol.DocumentUri(fileURI),
		Range: protocol.Range{
			Start: protocol.Position{Line: 0, Character: 0},
			End:   protocol.Position{Line: 0, Character: 0},
		},
	}, nil
}

// resolveIDLink resolves an id: link via UUID index
func resolveIDLink(uuid string) (any, error) {
	if serverState.ProcessedFiles == nil {
		return nil, nil
	}

	uuid = uuid[3:] // remove "id:"

	// Look up UUID in index
	if locInterface, found := serverState.ProcessedFiles.UuidIndex.Load(orgscanner.UUID(uuid)); found {
		fmt.Println("Found UUID in database!")
		if location, ok := locInterface.(orgscanner.HeaderLocation); ok {
			// Convert to file URI
			fileURI := "file://" + location.FilePath

			// Use the stored position (1-based line numbers from orgscanner)
			line := uint32(location.Position.StartLine - 1)
			col := uint32(location.Position.StartColumn - 1)

			return protocol.Location{
				URI: protocol.DocumentUri(fileURI),
				Range: protocol.Range{
					Start: protocol.Position{Line: line, Character: col},
					End:   protocol.Position{Line: line, Character: col},
				},
			}, nil
		}
	}

	// UUID not found
	return nil, nil
}

// countUUIDs returns the total number of UUIDs in the ProcessedFiles.
func countUUIDs(procFiles *orgscanner.ProcessedFiles) int {
	count := 0
	procFiles.UuidIndex.Range(func(key, value any) bool {
		count++
		return true
	})
	return count
}
