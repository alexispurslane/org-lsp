package server

import (
	"context"
	"log/slog"
	"slices"
	"strings"

	"github.com/alexispurslane/go-org/org"
	protocol "go.lsp.dev/protocol"
)

// buildLinkTarget constructs the full target URL from a RegularLink
func buildLinkTarget(link org.RegularLink) string {
	if link.Protocol == "" {
		return link.URL
	}
	// Check if URL already has the protocol prefix
	prefix := link.Protocol + ":"
	if strings.HasPrefix(link.URL, prefix) {
		return link.URL
	}
	return prefix + link.URL
}

// DocumentHighlight implements textDocument/documentHighlight.
// Highlights all occurrences of the same tag when cursor is on a tag,
// or all links to the same target when cursor is on a link.
func (s *ServerImpl) DocumentHighlight(ctx context.Context, params *protocol.DocumentHighlightParams) (result []protocol.DocumentHighlight, err error) {
	if s.state == nil {
		return nil, nil
	}
	s.state.Mu.RLock()
	defer s.state.Mu.RUnlock()

	uri := params.TextDocument.URI
	doc, found := s.state.OpenDocs[uri]
	if !found {
		return nil, nil
	}

	pos := params.Position

	// First, check if we're on a tag in a headline
	if headline, found := findNodeAtPosition[org.Headline](doc, pos); found {
		// Check if position is within any tag
		if tag := findTagAtPosition(headline, pos); tag != "" {
			// Highlight all occurrences of this tag
			return highlightAllTags(doc, tag), nil
		}
	}

	// Check if we're on a link
	if link, found := findNodeAtPosition[org.RegularLink](doc, pos); found {
		// Highlight all links with the same target
		return highlightAllLinksToTarget(doc, *link), nil
	}

	return nil, nil
}

// findTagAtPosition checks if the position is within any tag of the headline
func findTagAtPosition(headline *org.Headline, pos protocol.Position) string {
	// Only check if we're on the headline line
	if int(pos.Line) != headline.Pos.StartLine {
		return ""
	}

	// Calculate where tags start:
	// level (stars) + 1 (space) + title length
	title := org.String(headline.Title...)
	tagsStartCol := headline.Lvl + 1 + len(title)

	// Iterate through tags to find which one contains the cursor
	currentCol := tagsStartCol
	for _, tag := range headline.Tags {
		// Each tag includes the colon prefix in its position
		// Tag format is ":tagname:" so length is len(tag) + 1
		tagStart := currentCol
		tagEnd := tagStart + len(tag) + 1 // +1 for the colon

		// Check if cursor is within this tag
		if int(pos.Character) >= tagStart && int(pos.Character) <= tagEnd {
			return tag
		}

		// Move to next tag position (add tag length + 1 for colon)
		currentCol = tagEnd
	}

	return ""
}

// highlightAllTags returns highlights for all occurrences of a tag in the document
func highlightAllTags(doc *org.Document, targetTag string) []protocol.DocumentHighlight {
	var highlights []protocol.DocumentHighlight

	var walkNodes func(node org.Node)
	walkNodes = func(node org.Node) {
		if headline, ok := node.(org.Headline); ok {
			if slices.Contains(headline.Tags, targetTag) {
				// Highlight the entire headline or just the tag area
				// For now, highlight the headline
				highlights = append(highlights, protocol.DocumentHighlight{
					Range: toProtocolRange(headline.Pos),
					Kind:  protocol.DocumentHighlightKindRead,
				})
			}
		}

		node.Range(func(n org.Node) bool {
			walkNodes(n)
			return true
		})
	}

	for _, node := range doc.Nodes {
		walkNodes(node)
	}

	slog.Debug("DocumentHighlight found tags", "tag", targetTag, "count", len(highlights))
	return highlights
}

// highlightAllLinksToTarget returns highlights for all links pointing to the same target
func highlightAllLinksToTarget(doc *org.Document, targetLink org.RegularLink) []protocol.DocumentHighlight {
	var highlights []protocol.DocumentHighlight

	target := buildLinkTarget(targetLink)

	var walkNodes func(node org.Node)
	walkNodes = func(node org.Node) {
		if link, ok := node.(org.RegularLink); ok {
			if buildLinkTarget(link) == target {
				highlights = append(highlights, protocol.DocumentHighlight{
					Range: toProtocolRange(link.Pos),
					Kind:  protocol.DocumentHighlightKindRead,
				})
			}
		}

		node.Range(func(n org.Node) bool {
			walkNodes(n)
			return true
		})
	}

	for _, node := range doc.Nodes {
		walkNodes(node)
	}

	slog.Debug("DocumentHighlight found links", "target", target, "count", len(highlights))
	return highlights
}
