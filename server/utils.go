package server

import (
	"net/url"
	"path/filepath"

	"github.com/alexispurslane/go-org/org"
	"github.com/alexispurslane/org-lsp/orgscanner"
	protocol "go.lsp.dev/protocol"
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

// pathToURI converts a filesystem path to a file:// URI
func pathToURI(path string) string {
	// Ensure path is absolute for valid file URI (RFC 8089)
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}
	// Convert to forward slashes and prepend file://
	// Note: filepath.Abs on Unix returns /absolute/path, so we get file:///absolute/path
	// On Windows, it returns C:\path, so we get file://C:/path (which is correct)
	return "file://" + filepath.ToSlash(absPath)
}

func collectChildren(node org.Node) []org.Node {
	var nodes []org.Node
	node.Range(func(n org.Node) bool {
		nodes = append(nodes, n)
		return true
	})
	return nodes
}

func findNodesInRange(nodes []org.Node, startLine, endLine int) []org.Node {
	var results []org.Node

	var walk func(node org.Node) bool
	walk = func(node org.Node) bool {
		pos := node.Position()

		// Check if this node overlaps with our selection range
		if pos.StartLine <= endLine && pos.EndLine >= startLine {
			// If it's completely inside, or the top is inside, then we should add it to our list
			fullyContained := pos.StartLine >= startLine && pos.EndLine <= endLine
			topOverlaps := pos.StartLine >= startLine && pos.EndLine >= endLine
			if fullyContained || topOverlaps {
				results = append(results, node)
			} else {
				// If it is only overlapping, then we should investigate it for children that satisfy our criteria
				node.Range(func(n org.Node) bool {
					return walk(n)
				})
			}
		}

		return true
	}

	for _, node := range nodes {
		walk(node)
	}
	return results
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
		nodePos := node.Position()

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

		node.Range(func(n org.Node) bool {
			walkNodes(n, currentDepth+1)
			return true
		})
	}

	// Walk all document node
	for _, node := range doc.Nodes {
		walkNodes(node, 0)
	}

	if foundNode != nil {
		return foundNode, true
	}

	var zero T
	return &zero, false
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
		URI: protocol.DocumentURI(uri),
		Range: protocol.Range{
			Start: protocol.Position{
				Line:      uint32(pos.StartLine), // Already 0-indexed
				Character: uint32(pos.StartColumn),
			},
			End: protocol.Position{
				Line:      uint32(pos.EndLine), // Already 0-indexed
				Character: uint32(pos.EndColumn),
			},
		},
	}, nil
}

// toProtocolRange converts an org Position to LSP Range
func toProtocolRange(pos org.Position) protocol.Range {
	return protocol.Range{
		Start: protocol.Position{
			Line:      uint32(pos.StartLine),
			Character: uint32(pos.StartColumn),
		},
		End: protocol.Position{
			Line:      uint32(pos.EndLine),
			Character: uint32(pos.EndColumn),
		},
	}
}
