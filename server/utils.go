package server

import (
	"net/url"
	"path/filepath"
	"reflect"

	"github.com/alexispurslane/go-org/org"
	"github.com/alexispurslane/org-lsp/orgscanner"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// countUUIDs returns the total number of UUIDs in the ProcessedFiles.
func countUUIDs(procFiles *orgscanner.ProcessedFiles) int {
	count := 0
	procFiles.UuidIndex.Range(func(key, value any) bool {
		count++
		return true
	})
	return count
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

// findNodeAtPosition searches for a node of type T at the given cursor position
func findNodeAtPosition[T org.Node](doc *org.Document, pos protocol.Position) (*T, bool) {
	if doc == nil {
		var zero T
		return &zero, false
	}

	targetLine := int(pos.Line)
	targetCol := int(pos.Character)

	var foundNode *T
	var foundDepth = -1

	var walkNodes func(node org.Node, currentDepth int)
	walkNodes = func(node org.Node, currentDepth int) {

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

		// Determine if this is an inline node (requires precise column match) or block node (line-only match)
		var isInline bool
		switch node.(type) {
		case org.Text, org.LineBreak, org.ExplicitLineBreak, org.StatisticToken,
			org.Timestamp, org.Emphasis, org.InlineBlock, org.LatexFragment,
			org.FootnoteLink, org.RegularLink, org.Macro:
			isInline = true
		}

		cursorInNode := targetLine >= nodePos.StartLine && targetLine <= nodePos.EndLine

		if isInline {
			cursorInNode = cursorInNode &&
				targetCol >= nodePos.StartColumn && targetCol <= nodePos.EndColumn
		}

		if cursorInNode {
			if typedNode, ok := node.(T); ok {
				// Only take this node if it's deeper than our current best match
				if currentDepth > foundDepth {
					foundNode = &typedNode
					foundDepth = currentDepth
				}
			}
		}

		// Always walk children to find more specific matches
		if children := getChildren(node); children != nil {
			for _, child := range children {
				walkNodes(child, currentDepth+1)
			}
		}
	}

	// Walk all document nodes
	for _, node := range doc.Nodes {
		walkNodes(node, 0)
	}

	if foundNode != nil {
		return foundNode, true
	}

	var zero T
	return &zero, false
}

// getChildren returns the children of a node by finding Children or Content fields via reflection
func getChildren(node org.Node) []org.Node {
	if node == nil {
		return nil
	}

	nodeVal := reflect.ValueOf(node)

	// Try to find a Children field of type []org.Node
	if childrenField := nodeVal.FieldByName("Children"); childrenField.IsValid() {
		if children, ok := childrenField.Interface().([]org.Node); ok {
			return children
		}
	}

	// Try to find a Content field of type []org.Node (used by Emphasis, etc.)
	if contentField := nodeVal.FieldByName("Content"); contentField.IsValid() {
		if content, ok := contentField.Interface().([]org.Node); ok {
			return content
		}
	}

	return nil
}

// ptrTo returns a pointer to the given value
func ptrTo[T any](v T) *T {
	return &v
}

// strPtr returns a pointer to the given string
func strPtr(s string) *string {
	return &s
}

// toProtocolLocation converts an absolute path and org Position to LSP Location
func toProtocolLocation(absPath string, pos org.Position) (protocol.Location, error) {
	// Convert absolute path to URI
	uri := "file://" + filepath.ToSlash(absPath)

	// Create location with the link's range
	return protocol.Location{
		URI: protocol.DocumentUri(uri),
		Range: protocol.Range{
			Start: protocol.Position{
				Line:      uint32(pos.StartLine - 1), // Convert 1-based to 0-based
				Character: uint32(pos.StartColumn),
			},
			End: protocol.Position{
				Line:      uint32(pos.EndLine - 1), // Convert 1-based to 0-based
				Character: uint32(pos.EndColumn),
			},
		},
	}, nil
}

// toProtocolRange converts an org Position to LSP Range
func toProtocolRange(pos org.Position) protocol.Range {
	return protocol.Range{
		Start: protocol.Position{
			Line:      uint32(pos.StartLine - 1),
			Character: uint32(pos.StartColumn),
		},
		End: protocol.Position{
			Line:      uint32(pos.EndLine - 1),
			Character: uint32(pos.EndColumn),
		},
	}
}
