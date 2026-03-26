package server

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/alexispurslane/go-org/org"
	"github.com/alexispurslane/org-lsp/orgscanner"
	protocol "go.lsp.dev/protocol"
)

// CodeLens returns code lens items for each heading in the document.
// Each lens shows the count of backlinks pointing to that heading.
func (s *ServerImpl) CodeLens(ctx context.Context, params *protocol.CodeLensParams) (result []protocol.CodeLens, err error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("PANIC in CodeLens", "recover", r)
		}
	}()

	slog.Debug("CodeLens called", "uri", params.TextDocument.URI)
	if s.state == nil {
		slog.Error("Server state is nil in CodeLens")
		return nil, nil
	}
	s.state.Mu.RLock()
	defer s.state.Mu.RUnlock()

	uri := params.TextDocument.URI
	doc, found := s.state.OpenDocs[uri]
	if !found {
		slog.Debug("Document not in OpenDocs", "uri", uri)
		return nil, nil
	}

	// Get the file path for this document
	docPath := uriToPath(string(uri))
	relPath, err := filepath.Rel(s.state.OrgScanRoot, docPath)
	if err != nil {
		slog.Debug("Failed to get relative path", "error", err)
		relPath = docPath
	}

	slog.Debug("CodeLens processing document", "uri", uri, "relPath", relPath)

	// Collect all headings in the document
	headings := collectHeadings(doc)

	// For each heading, count backlinks
	var lenses []protocol.CodeLens
	for _, heading := range headings {
		backlinks := countBacklinks(s.state, relPath, heading.UUID)

		slog.Debug("Heading backlink count", "title", heading.Title, "uuid", heading.UUID, "count", backlinks)

		// Create code lens for this heading
		title := formatBacklinkCount(backlinks)
		command := protocol.Command{
			Title: title,
		}

		lens := protocol.CodeLens{
			Range:   heading.Range,
			Command: &command,
		}
		lenses = append(lenses, lens)
	}

	return lenses, nil
}

// headingInfo holds information about a heading in the document
type headingInfo struct {
	Title string
	UUID  string
	Range protocol.Range
}

// collectHeadings walks the document and collects all headings with their info
func collectHeadings(doc *org.Document) []headingInfo {
	var headings []headingInfo

	var walkNodes func(node org.Node)
	walkNodes = func(node org.Node) {
		if headline, ok := node.(org.Headline); ok {
			title := strings.TrimSpace(org.String(headline.Title...))
			uuid := extractUUIDFromHeadline(&headline)

			// CodeLens should be positioned at just the heading line, not the entire section
			headingRange := protocol.Range{
				Start: protocol.Position{
					Line:      uint32(headline.Pos.StartLine),
					Character: uint32(headline.Pos.StartColumn),
				},
				End: protocol.Position{
					Line:      uint32(headline.Pos.StartLine), // Same line as start (just the heading)
					Character: uint32(headline.Pos.EndColumn),
				},
			}
			heading := headingInfo{
				Title: title,
				UUID:  uuid,
				Range: headingRange,
			}
			headings = append(headings, heading)
		}

		// Walk children
		node.Range(func(n org.Node) bool {
			walkNodes(n)
			return true
		})
	}

	for _, node := range doc.Nodes {
		walkNodes(node)
	}

	return headings
}

// extractUUIDFromHeadline extracts the ID property from a headline's property drawer
func extractUUIDFromHeadline(headline *org.Headline) string {
	if headline.Properties == nil {
		return ""
	}

	for _, prop := range headline.Properties.Properties {
		if prop[0] == "ID" && len(prop) > 1 && prop[1] != "" {
			return prop[1]
		}
	}
	return ""
}

// countBacklinks counts how many links point to a heading:
// - ID links: count id:UUID links in other files pointing to this heading's UUID
// - File links: count file: links in other files pointing to this file
func countBacklinks(state *State, targetFilePath, targetUUID string) int {
	if state.Scanner == nil || state.Scanner.ProcessedFiles == nil {
		return 0
	}

	count := 0

	// Walk through all processed files
	state.Scanner.ProcessedFiles.Files.Range(func(key, value any) bool {
		fileInfo, ok := value.(*orgscanner.FileInfo)
		if !ok || fileInfo.ParsedOrg == nil {
			return true // continue iteration
		}

		// Skip the target file itself
		if fileInfo.Path == targetFilePath {
			return true // continue iteration
		}

		// Search for links in this file
		var walkNodes func(node org.Node)
		walkNodes = func(node org.Node) {
			if link, ok := node.(org.RegularLink); ok {
				// Check for id: link
				if targetUUID != "" {
					if linkUUID, ok0 := strings.CutPrefix(link.URL, "id:"); ok0 && linkUUID == targetUUID {
						count++
						return
					}
				}

				// Check for file: link pointing to target file
				if after, ok0 := strings.CutPrefix(link.URL, "file:"); ok0 {
					linkPath := after

					// Resolve the link path to compare with target
					// Get the source file's directory
					sourceRelPath := fileInfo.Path
					sourceAbsPath := filepath.Join(state.OrgScanRoot, sourceRelPath)
					sourceDir := filepath.Dir(sourceAbsPath)

					// Resolve the link relative to source file's directory
					resolvedPath := filepath.Join(sourceDir, linkPath)
					resolvedPath = filepath.Clean(resolvedPath)

					// Compare with target file's absolute path
					targetAbsPath := filepath.Join(state.OrgScanRoot, targetFilePath)
					targetAbsPath = filepath.Clean(targetAbsPath)

					if resolvedPath == targetAbsPath {
						count++
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

	return count
}

// formatBacklinkCount returns a human-readable string for the backlink count
func formatBacklinkCount(count int) string {
	if count == 0 {
		return "0 backlinks"
	}
	if count == 1 {
		return "1 backlink"
	}
	return fmt.Sprintf("%d backlinks", count)
}
