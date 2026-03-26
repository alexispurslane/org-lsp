package server

import (
	"context"
	"log/slog"

	"github.com/alexispurslane/go-org/org"
	protocol "go.lsp.dev/protocol"
)

// DocumentLink handles textDocument/documentLink requests.
// It returns all links in the document as clickable ranges with resolved targets.
func (s *ServerImpl) DocumentLink(ctx context.Context, params *protocol.DocumentLinkParams) (result []protocol.DocumentLink, err error) {
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

	var links []protocol.DocumentLink

	// Walk all nodes in the document to find links
	var walkNodes func(node org.Node)
	walkNodes = func(node org.Node) {
		if link, ok := node.(org.RegularLink); ok {
			// Resolve the link to get the target file URI
			target := resolveLinkTarget(s.state, uri, link)

			links = append(links, protocol.DocumentLink{
				Range:   toProtocolRange(link.Pos),
				Target:  target,
				Tooltip: buildLinkTooltip(link),
			})
		}

		// Walk children recursively
		node.Range(func(n org.Node) bool {
			walkNodes(n)
			return true
		})
	}

	// Walk all document nodes
	for _, node := range doc.Nodes {
		walkNodes(node)
	}

	slog.Debug("DocumentLink found links", "uri", uri, "count", len(links))
	return links, nil
}

// DocumentLinkResolve handles textDocument/documentLinkResolve requests.
// Since we provide full link info upfront, this just returns nil.
func (s *ServerImpl) DocumentLinkResolve(ctx context.Context, params *protocol.DocumentLink) (result *protocol.DocumentLink, err error) {
	return nil, nil
}

// resolveLinkTarget resolves a RegularLink to a file:// URI for LSP clients.
func resolveLinkTarget(state *State, currentURI protocol.DocumentURI, link org.RegularLink) protocol.DocumentURI {
	switch link.Protocol {
	case "file":
		// Use existing resolveFileLink from definitions.go
		filePath, _, err := resolveFileLink(currentURI, link.URL)
		if err != nil {
			// Fall back to just returning the URL as-is if resolution fails
			return protocol.DocumentURI(link.URL)
		}
		// Convert absolute path to file:// URI
		return protocol.DocumentURI(pathToURI(filePath))

	case "id":
		// Use existing resolveIDLink from definitions.go
		filePath, _, err := resolveIDLink(state, currentURI, link.URL)
		if err != nil {
			// Fall back to id: scheme URL
			return protocol.DocumentURI("id:" + link.URL)
		}
		// Convert absolute path to file:// URI
		return protocol.DocumentURI(pathToURI(filePath))

	case "http", "https":
		// Return web URLs as-is
		return protocol.DocumentURI(link.URL)

	default:
		// For other protocols or no protocol, return as-is
		if link.Protocol != "" {
			return protocol.DocumentURI(link.Protocol + ":" + link.URL)
		}
		return protocol.DocumentURI(link.URL)
	}
}

// buildLinkTooltip creates the tooltip text from link description
func buildLinkTooltip(link org.RegularLink) string {
	if len(link.Description) > 0 {
		desc := org.String(link.Description...)
		if desc != "" {
			return desc
		}
	}
	// Fall back to showing the URL
	if link.Protocol != "" {
		return link.Protocol + ":" + link.URL
	}
	return link.URL
}
