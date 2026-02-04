package server

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/alexispurslane/go-org/org"
	"github.com/alexispurslane/org-lsp/orgscanner"
	protocol "go.lsp.dev/protocol"
)

func (s *ServerImpl) Definition(ctx context.Context, params *protocol.DefinitionParams) (result []protocol.Location, err error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("PANIC in Definition", "recover", r)
		}
	}()
	slog.Debug("Definition called", "uri", params.TextDocument.URI, "line", params.Position.Line, "char", params.Position.Character)
	if serverState == nil {
		slog.Error("Server state is nil in definition")
		return nil, nil
	}

	uri := params.TextDocument.URI
	doc, found := serverState.OpenDocs[uri]
	if !found {
		slog.Debug("Document not in OpenDocs", "uri", uri, "availableDocs", len(serverState.OpenDocs))
		return nil, nil
	}

	// Find link at cursor position using generic helper
	linkNode, foundLink := findNodeAtPosition[org.RegularLink](doc, params.Position)
	if !foundLink {
		slog.Debug("No link node found at position", "line", params.Position.Line, "char", params.Position.Character)
		return nil, nil
	}

	slog.Debug("Found link node", "protocol", linkNode.Protocol, "url", linkNode.URL)

	var filePath string
	var pos org.Position

	switch linkNode.Protocol {
	case "file":
		slog.Debug("Resolving file link", "url", linkNode.URL)
		filePath, pos, err = resolveFileLink(uri, linkNode.URL)
	case "id":
		slog.Debug("Resolving ID link", "uuid", linkNode.URL)
		filePath, pos, err = resolveIDLink(uri, linkNode.URL)
	default:
		slog.Debug("Unknown link protocol", "protocol", linkNode.Protocol)
		return nil, nil
	}

	if err != nil {
		slog.Debug("Link resolution failed", "error", err)
		return nil, nil
	}

	slog.Debug("Link resolved", "filePath", filePath, "line", pos.StartLine)
	location, err := toProtocolLocation(filePath, pos)
	if err != nil {
		slog.Error("Failed to convert to protocol location", "error", err)
		return nil, err
	}
	return []protocol.Location{location}, nil
}

func (s *ServerImpl) Hover(ctx context.Context, params *protocol.HoverParams) (result *protocol.Hover, err error) {
	slog.Debug("Hover handler called", "uri", params.TextDocument.URI, "line", params.Position.Line, "char", params.Position.Character)
	if serverState == nil {
		slog.Error("Server state is nil in hover")
		return nil, nil
	}

	uri := params.TextDocument.URI
	doc, found := serverState.OpenDocs[uri]
	if !found {
		slog.Debug("Document not found in OpenDocs for hover", "uri", uri, "availableDocs", len(serverState.OpenDocs))
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
	var resolveErr error

	switch linkNode.Protocol {
	case "file":
		filePath, targetPos, resolveErr = resolveFileLink(uri, linkNode.URL)
	case "id":
		filePath, targetPos, resolveErr = resolveIDLink(uri, linkNode.URL)
	default:
		return nil, nil
	}

	if resolveErr != nil {
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
			Kind:  "markdown",
			Value: content,
		},
		Range: hoverRangePtr,
	}, nil
}

func (s *ServerImpl) References(ctx context.Context, params *protocol.ReferenceParams) (result []protocol.Location, err error) {
	if serverState == nil {
		return nil, nil
	}

	uri := params.TextDocument.URI
	doc, found := serverState.OpenDocs[uri]
	if !found {
		slog.Debug("Document not in OpenDocs", "uri", uri)
		return nil, nil
	}

	// First check if cursor is on an id: link (Enhanced References feature)
	link, foundLink := findNodeAtPosition[org.RegularLink](doc, params.Position)
	if foundLink {
		// Check if this is an id: link
		if linkUUID, ok := strings.CutPrefix(link.URL, "id:"); ok && linkUUID != "" {
			slog.Debug("Found id: link at cursor, finding references", "uuid", linkUUID)
			locations, err := findIDReferences(linkUUID)
			if err != nil {
				return nil, err
			}
			return locations, nil
		}
	}

	// Fall back to headline detection
	headline, foundHeadline := findNodeAtPosition[org.Headline](doc, params.Position)
	if !foundHeadline {
		return nil, nil
	}

	// Extract UUID from headline properties
	for _, prop := range headline.Properties.Properties {
		if prop[0] == "ID" && prop[1] != "" {
			uuid := prop[1]
			locations, err := findIDReferences(uuid)
			if err != nil {
				return nil, err
			}
			return locations, nil
		}
	}

	return nil, nil
}

// resolveFileLink resolves a file: link to an absolute path and returns the target position
func resolveFileLink(currentURI protocol.DocumentURI, linkURL string) (string, org.Position, error) {
	slog.Debug("Resolving file link", "currentURI", currentURI, "linkURL", linkURL)

	// Convert URI to filesystem path
	currentPath := uriToPath(string(currentURI))

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
		StartLine:   0,
		StartColumn: 0,
		EndLine:     0,
		EndColumn:   0,
	}

	return linkURL, pos, nil
}

// resolveIDLink resolves an id: link via UUID index and returns the target position
func resolveIDLink(currentURI protocol.DocumentURI, uuid string) (string, org.Position, error) {
	if serverState.Scanner == nil || serverState.Scanner.ProcessedFiles == nil {
		return "", org.Position{}, fmt.Errorf("no processed files")
	}

	uuid = uuid[3:] // remove "id:"

	// Look up UUID in index
	locInterface, found := serverState.Scanner.ProcessedFiles.UuidIndex.Load(orgscanner.UUID(uuid))
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

// extractContextLines extracts ±3 lines of context around the target position
func extractContextLines(filePath string, targetPos org.Position) string {
	slog.Debug("Extracting context lines", "filePath", filePath, "targetPos", targetPos)

	lines, err := readFileLines(filePath)
	if err != nil {
		slog.Debug("Failed to read file for context extraction", "filePath", filePath, "error", err)
		return ""
	}

	// Calculate line range (±3 lines, 1-based to 0-based)
	startLine := max(0, targetPos.StartLine)          // -3 lines, convert to 0-based
	endLine := min(len(lines), targetPos.StartLine+2) // 3 lines, inclusive

	return joinLines(lines, startLine, endLine)
}

// readFileLines reads a file and returns its lines
func readFileLines(filePath string) ([]string, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(content), "\n")
	if len(lines) == 0 {
		return nil, fmt.Errorf("file has no lines")
	}
	return lines, nil
}

// joinLines joins lines from start to end index
func joinLines(lines []string, start, end int) string {
	var context strings.Builder
	for i := start; i < end && i < len(lines); i++ {
		if i > start {
			context.WriteString("\n")
		}
		context.WriteString(lines[i])
	}
	return context.String()
}

func findIDReferences(targetUUID string) ([]protocol.Location, error) {
	if serverState.Scanner == nil || serverState.Scanner.ProcessedFiles == nil {
		return nil, nil
	}

	var locations []protocol.Location

	// Walk through all processed files using sync.Map.Range
	serverState.Scanner.ProcessedFiles.Files.Range(func(key, value any) bool {
		fileInfo, ok := value.(*orgscanner.FileInfo)
		if !ok || fileInfo.ParsedOrg == nil {
			return true // continue iteration
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
			node.Range(func(n org.Node) bool {
				walkNodes(n)
				return true
			})
		}

		for _, node := range fileInfo.ParsedOrg.Nodes {
			walkNodes(node)
		}
		return true // continue iteration
	})

	return locations, nil
}
