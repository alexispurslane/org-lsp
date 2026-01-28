// Package server provides the LSP server implementation for org-mode files.
package server

import (
	"fmt"
	"log/slog"
	"os"
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

// Generic helper to find a node of type T at a given cursor position
func findNodeAtPosition[T org.Node](doc *org.Document, pos protocol.Position) (*T, bool) {
	if doc == nil {
		var zero T
		return &zero, false
	}

	targetLine := int(pos.Line) + 1     // Convert to 1-based for org.Position
	targetCol := int(pos.Character) + 1 // Convert to 1-based for org.Position

	var foundNode *T

	var walkNodes func(node org.Node, currentDepth int)
	walkNodes = func(node org.Node, currentDepth int) {
		if foundNode != nil {
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
			}
		}

		if !hasPos {
			// Always walk children even without position info
			if children := getChildren(node); children != nil {
				for _, child := range children {
					walkNodes(child, currentDepth+1)
				}
			}
			return
		}

		// Check if cursor is within this node's range
		cursorInNode := targetLine >= nodePos.StartLine && targetLine <= nodePos.EndLine &&
			targetCol >= nodePos.StartColumn && targetCol <= nodePos.EndColumn

		if cursorInNode {
			// Check if this node matches the target type
			if typedNode, ok := node.(T); ok {
				foundNode = &typedNode
				return
			}

			// Not the target type, but cursor is here - walk children to go deeper
			if children := getChildren(node); children != nil {
				for _, child := range children {
					walkNodes(child, currentDepth+1)
				}
			}
		}
	}

	// Walk all document nodes
	for _, node := range doc.Nodes {
		walkNodes(node, 0)
		if foundNode != nil {
			break
		}
	}

	if foundNode != nil {
		return foundNode, true
	}

	var zero T
	return &zero, false
}

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
		TextDocumentHover:      textDocumentHover,
		TextDocumentReferences: textDocumentReferences,
		TextDocumentCompletion: textDocumentCompletion,
	}
	return server.NewServer(&handler, serverName, false)
}

// initialize handles the LSP initialize request.
func initialize(context *glsp.Context, params *protocol.InitializeParams) (any, error) {
	// Configure logging level from environment
	logLevel := os.Getenv("ORG_LSP_LOG_LEVEL")
	level := slog.LevelInfo // default
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

	// Find link at cursor position using generic helper
	linkNode, foundLink := findNodeAtPosition[org.RegularLink](doc, params.Position)
	if !foundLink {
		return nil, nil
	}

	var filePath string
	var pos org.Position
	var err error

	switch linkNode.Protocol {
	case "file":
		filePath, pos, err = resolveFileLink(uri, linkNode.URL)
	case "id":
		filePath, pos, err = resolveIDLink(uri, linkNode.URL)
	default:
		return nil, nil
	}

	if err != nil {
		return nil, nil
	}

	return toProtocolLocation(filePath, pos)
}

// toProtocolLocation converts an org.Position to a protocol.Location
func toProtocolLocation(filePath string, pos org.Position) (protocol.Location, error) {
	// Convert to absolute path and file URI
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return protocol.Location{}, err
	}
	fileURI := "file://" + absPath

	return protocol.Location{
		URI:   protocol.DocumentUri(fileURI),
		Range: toProtocolRange(pos),
	}, nil
}

// toProtocolRange converts an org.Position to a protocol.Range
func toProtocolRange(pos org.Position) protocol.Range {
	// Convert from 1-based (org) to 0-based (LSP) coordinates
	startLine := uint32(pos.StartLine - 1)
	startCol := uint32(pos.StartColumn - 1)
	endLine := uint32(pos.EndLine - 1)
	endCol := uint32(pos.EndColumn - 1)

	return protocol.Range{
		Start: protocol.Position{Line: startLine, Character: startCol},
		End:   protocol.Position{Line: endLine, Character: endCol},
	}
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

// resolveFileLink resolves a file: link to an absolute path and returns the target position
func resolveFileLink(currentURI protocol.DocumentUri, linkURL string) (string, org.Position, error) {
	slog.Debug("Resolving file link", "currentURI", currentURI, "linkURL", linkURL)

	// Convert URI to path and remove file:// prefix
	currentPath := string(currentURI)[7:]

	// Remove the org-mode file: prefix
	linkURL = strings.TrimPrefix(linkURL, "file:")

	// Handle tilde expansion (~ -> home directory)
	if strings.HasPrefix(linkURL, "~/") {
		if homeDir, err := os.UserHomeDir(); err == nil {
			linkURL = filepath.Join(homeDir, linkURL[2:])
		} else {
			return "", org.Position{}, fmt.Errorf("failed to expand home directory: %w", err)
		}
	}

	// Resolve environment variables (e.g., $HOME, $ORG_DIR)
	linkURL = os.ExpandEnv(linkURL)

	// If path is not absolute, resolve relative to current document directory
	if !filepath.IsAbs(linkURL) {
		currentDir := filepath.Dir(currentPath)
		linkURL = filepath.Join(currentDir, linkURL)
	}

	// Clean the path (resolve . and ..)
	linkURL = filepath.Clean(linkURL)

	slog.Debug("Resolved file link path", "currentPath", currentPath, "resolvedPath", linkURL)

	// For file links, return position at start of file
	pos := org.Position{
		StartLine:   1,
		StartColumn: 1,
		EndLine:     1,
		EndColumn:   1,
	}

	return linkURL, pos, nil
}

// resolveIDLink resolves an id: link via UUID index and returns the target position
func resolveIDLink(currentURI protocol.DocumentUri, uuid string) (string, org.Position, error) {
	if serverState.ProcessedFiles == nil {
		return "", org.Position{}, fmt.Errorf("no processed files")
	}

	uuid = uuid[3:] // remove "id:"

	// Look up UUID in index
	locInterface, found := serverState.ProcessedFiles.UuidIndex.Load(orgscanner.UUID(uuid))
	if !found {
		return "", org.Position{}, fmt.Errorf("UUID not found")
	}

	location, ok := locInterface.(orgscanner.HeaderLocation)
	if !ok {
		return "", org.Position{}, fmt.Errorf("UUID not found")
	}

	// Resolve relative path to absolute using workspace root
	if serverState.OrgScanRoot == "" {
		return "", org.Position{}, fmt.Errorf("no workspace root configured")
	}

	// The FilePath stored in HeaderLocation is relative to OrgScanRoot
	absPath := filepath.Join(serverState.OrgScanRoot, location.FilePath)

	// Clean the path (resolve . and ..)
	absPath = filepath.Clean(absPath)

	slog.Debug("Resolved ID link path", "relativePath", location.FilePath, "absPath", absPath, "orgScanRoot", serverState.OrgScanRoot)

	return absPath, location.Position, nil
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

// textDocumentHover handles the LSP textDocument/hover request
func textDocumentHover(context *glsp.Context, params *protocol.HoverParams) (*protocol.Hover, error) {
	if serverState == nil {
		return nil, nil
	}

	uri := params.TextDocument.URI
	doc, found := serverState.OpenDocs[uri]
	if !found {
		return nil, nil
	}

	// Find link at cursor position
	linkNode, foundLink := findNodeAtPosition[org.RegularLink](doc, params.Position)
	if !foundLink {
		return nil, nil
	}

	// Resolve link to get target position
	var filePath string
	var targetPos org.Position
	var err error

	switch linkNode.Protocol {
	case "file":
		filePath, targetPos, err = resolveFileLink(uri, linkNode.URL)
	case "id":
		filePath, targetPos, err = resolveIDLink(uri, linkNode.URL)
	default:
		return nil, nil
	}

	if err != nil {
		return nil, nil
	}

	slog.Info("Resolved link absolute path and position", "path", filePath, "pos", targetPos)

	// Build hover content
	content := fmt.Sprintf("**%s Link**\n\nTarget: `%s`", strings.ToUpper(linkNode.Protocol), filepath.Base(filePath))

	// Extract context lines from target document
	contextLines := extractContextLines(filePath, targetPos)
	slog.Info("Context extraction result", "hasContent", contextLines != "", "length", len(contextLines))
	if contextLines != "" {
		content += fmt.Sprintf("\n\n```org\n%s\n```", contextLines)
	}

	// Calculate hover range from link node
	hoverRange := toProtocolRange(linkNode.Pos)
	hoverRangePtr := &hoverRange

	return &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.MarkupKindMarkdown,
			Value: content,
		},
		Range: hoverRangePtr,
	}, nil
}

// extractContextLines extracts ±3 lines of context around the target position
func extractContextLines(filePath string, targetPos org.Position) string {
	// Debug logging
	slog.Debug("Extracting context lines", "filePath", filePath, "targetPos", targetPos)

	// Read file content
	content, err := os.ReadFile(filePath)
	if err != nil {
		slog.Debug("Failed to read file for context extraction", "filePath", filePath, "error", err)
		return ""
	}

	lines := strings.Split(string(content), "\n")
	if len(lines) == 0 {
		slog.Debug("File has no lines", "filePath", filePath)
		return ""
	}

	// Debug: log file size and line count
	slog.Debug("File read successfully", "filePath", filePath, "lineCount", len(lines))

	// Calculate line range (±3 lines, 1-based to 0-based)
	startLine := max(0, targetPos.StartLine-4)        // -3 lines  convert to 0-based
	endLine := min(len(lines), targetPos.StartLine+2) // 3 lines, inclusive

	// Extract and join context lines
	var context strings.Builder
	for i := startLine; i < endLine; i++ {
		if i > startLine {
			context.WriteString("\n")
		}
		context.WriteString(lines[i])
	}

	result := context.String()
	slog.Debug("Context extraction complete", "filePath", filePath, "contextLength", len(result), "lines", endLine-startLine)
	return result
}

func textDocumentReferences(context *glsp.Context, params *protocol.ReferenceParams) ([]protocol.Location, error) {
	if serverState == nil {
		return nil, nil
	}

	uri := params.TextDocument.URI
	doc, found := serverState.OpenDocs[uri]
	if !found {
		slog.Debug("Document not in OpenDocs", "uri", uri)
		return nil, nil
	}

	// Find headline at cursor position
	headline, foundHeadline := findNodeAtPosition[org.Headline](doc, params.Position)
	if !foundHeadline {
		return nil, nil
	}

	// Extract UUID from headline properties
	for _, prop := range headline.Properties.Properties {
		if prop[0] == "ID" && prop[1] != "" {
			uuid := prop[1]
			return findIDReferences(uuid)
		}
	}

	return nil, nil
}

func findIDReferences(targetUUID string) ([]protocol.Location, error) {
	if serverState.ProcessedFiles == nil {
		return nil, nil
	}

	var locations []protocol.Location

	// Walk through all processed files
	for _, fileInfo := range serverState.ProcessedFiles.Files {
		if fileInfo.ParsedOrg == nil {
			continue
		}

		// Search for links in this file
		var walkNodes func(node org.Node)
		walkNodes = func(node org.Node) {
			if link, ok := node.(org.RegularLink); ok {
				// Check if this is an id: link
				if linkUUID, ok0 := strings.CutPrefix(link.URL, "id:"); ok0 {
					if linkUUID == targetUUID {
						// Convert link position to absolute file path
						absPath := filepath.Join(serverState.OrgScanRoot, fileInfo.Path)
						absPath = filepath.Clean(absPath)

						loc, err := toProtocolLocation(absPath, link.Pos)
						if err != nil {
							slog.Debug("Failed to convert link to protocol location", "error", err)
							return
						}
						locations = append(locations, loc)
					}
				}
			}

			// Walk children
			if children := getChildren(node); children != nil {
				for _, child := range children {
					walkNodes(child)
				}
			}
		}

		for _, node := range fileInfo.ParsedOrg.Nodes {
			walkNodes(node)
		}
	}

	return locations, nil
}

func textDocumentCompletion(glspCtx *glsp.Context, params *protocol.CompletionParams) (any, error) {
	if serverState == nil {
		return nil, nil
	}

	uri := params.TextDocument.URI
	doc, found := serverState.OpenDocs[uri]
	if !found {
		slog.Debug("Document not in OpenDocs", "uri", uri)
		return nil, nil
	}

	// Check completion context - are we in "id:" or ":tag:" completion?
	compContext := detectCompletionContext(doc, params.Position)

	var items []protocol.CompletionItem

	switch compContext {
	case "id":
		items = completeIDs()
	case "tag":
		items = completeTags(doc, params.Position)
	default:
		return nil, nil
	}

	return &protocol.CompletionList{
		IsIncomplete: false,
		Items:        items,
	}, nil
}

func detectCompletionContext(doc *org.Document, pos protocol.Position) string {
	// Check for tag context: must be on the headline line itself (where tags are)
	headline, found := findNodeAtPosition[org.Headline](doc, pos)
	if found {
		// Cursor must be on the headline's first line (where the * is)
		// Headlines span from their title line to the end of their content,
		// so we need to check specifically if we're on line 1 of the headline
		if headline.Pos.StartLine == int(pos.Line)+1 { // +1 because org is 1-based, LSP is 0-based
			return "tag"
		}
	}

	// Otherwise assume ID completion context
	// (The trigger character ":" was typed, and we're not on a headline line)
	return "id"
}

func completeIDs() []protocol.CompletionItem {
	if serverState.ProcessedFiles == nil {
		return nil
	}

	var items []protocol.CompletionItem

	// Walk through all UUIDs in the index
	serverState.ProcessedFiles.UuidIndex.Range(func(key, value any) bool {
		uuid := string(key.(orgscanner.UUID))
		location := value.(orgscanner.HeaderLocation)

		// Find the FileInfo for this location to get the title
		var title string
		for _, fileInfo := range serverState.ProcessedFiles.Files {
			if fileInfo.Path == location.FilePath {
				title = fileInfo.Title
				break
			}
		}

		// Create completion item
		kind := protocol.CompletionItemKindReference
		item := protocol.CompletionItem{
			Label:      uuid[:8] + "...", // Show first 8 chars for brevity
			Kind:       &kind,
			Detail:     &title,
			InsertText: &uuid,
		}

		items = append(items, item)
		return true // continue iteration
	})

	return items
}

func completeTags(doc *org.Document, pos protocol.Position) []protocol.CompletionItem {
	if serverState.ProcessedFiles == nil {
		return nil
	}

	var items []protocol.CompletionItem
	seenTags := make(map[string]bool)

	// Collect all unique tags from TagMap
	serverState.ProcessedFiles.TagMap.Range(func(key, value any) bool {
		tag := key.(string)

		if !seenTags[tag] {
			seenTags[tag] = true

			kind := protocol.CompletionItemKindProperty
			item := protocol.CompletionItem{
				Label:      tag,
				Kind:       &kind,
				Detail:     strPtr("Tag"),
				InsertText: strPtr(tag + ":"),
			}

			items = append(items, item)
		}
		return true
	})

	return items
}

// Helper to get string pointer
func strPtr(s string) *string {
	return &s
}
