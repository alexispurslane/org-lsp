// Package orgscanner provides core functionality for scanning, parsing,
// and extracting structured data from org-mode files.
package orgscanner

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/alexispurslane/go-org/org"
)

// ParseFile reads and parses an org-mode file relative to root, extracting metadata.
func ParseFile(filePath, root string) (*FileInfo, error) {
	absPath := filepath.Join(root, filePath)
	slog.Debug("Parsing org file", "path", filePath)

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	if len(data) == 0 {
		slog.Debug("Skipping empty file", "path", filePath)
		return nil, nil
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	conf := org.New()
	doc := conf.Parse(bytes.NewReader(data), absPath)

	result := &FileInfo{
		Path:      filePath,
		ModTime:   info.ModTime(),
		Preview:   extractPreview(doc, 500),
		Title:     extractTitle(doc),
		Tags:      extractTags(doc),
		UUIDs:     extractUUIDs(doc),
		ParsedOrg: doc,
	}

	slog.Debug("Extracted file metadata",
		"path", filePath,
		"title", result.Title,
		"tags", result.Tags,
		"uuid_count", len(result.UUIDs))

	return result, nil
}

// extractTitle gets the title from #+TITLE directive or first headline.
func extractTitle(doc *org.Document) string {
	if title := doc.Get("TITLE"); title != "" {
		slog.Debug("Found title in #+TITLE: directive", "title", title)
		return title
	}
	if title := doc.Get("title"); title != "" {
		slog.Debug("Found title in #+title: directive", "title", title)
		return title
	}

	for _, node := range doc.Nodes {
		if headline, ok := node.(org.Headline); ok {
			title := org.String(headline.Title...)
			if title != "" {
				slog.Debug("Found title in headline", "title", title)
				return title
			}
		}
	}

	slog.Warn("No title found in org file")
	return ""
}

// extractTags gets tags from the first headline.
func extractTags(doc *org.Document) []string {
	for _, node := range doc.Nodes {
		if headline, ok := node.(org.Headline); ok {
			if len(headline.Tags) > 0 {
				slog.Debug("Extracted tags from headline", "tags", headline.Tags)
				return headline.Tags
			}
		}
	}
	return nil
}

// normalizePosition ensures that end position is at least as valid as start position.
// If end line/column are zero or less than start, they are set to equal start.
func normalizePosition(pos org.Position) org.Position {
	if pos.EndLine == 0 || pos.EndLine < pos.StartLine {
		pos.EndLine = pos.StartLine
	}
	if pos.EndColumn == 0 || pos.EndColumn < pos.StartColumn {
		pos.EndColumn = pos.StartColumn
	}
	return pos
}

// extractUUIDs walks the AST to find all UUIDs in property drawers.
func extractUUIDs(doc *org.Document) FileUUIDPositions {
	uuidToPosition := make(FileUUIDPositions)

	var walkNodes func(node org.Node)
	walkNodes = func(node org.Node) {
		if headline, ok := node.(org.Headline); ok {
			if headline.Properties != nil {
				for _, prop := range headline.Properties.Properties {
					if prop[0] == "ID" && prop[1] != "" {
						id := UUID(prop[1])
						if isValidUUID(string(id)) {
							uuidToPosition[id] = UUIDInfo{
								Position: normalizePosition(headline.Pos),
								Title:    strings.TrimSpace(org.String(headline.Title...)),
							}
						}
					}
				}
			}

			for _, child := range headline.Children {
				walkNodes(child)
			}
		}
	}

	for _, node := range doc.Nodes {
		walkNodes(node)
	}

	if len(uuidToPosition) > 0 {
		slog.Debug("Extracted UUIDs from property drawers", "uuid_count", len(uuidToPosition))
	}
	return uuidToPosition
}

// extractPreview extracts a text preview from the document.
func extractPreview(doc *org.Document, maxLen int) string {
	var builder strings.Builder

	var collectText func(org.Node) bool
	collectText = func(node org.Node) bool {
		if builder.Len() >= maxLen {
			return false
		}
		switch n := node.(type) {
		case org.Text:
			builder.WriteString(n.Content)
		case org.RegularLink:
			if len(n.Description) > 0 {
				builder.WriteString(strings.TrimSpace(org.String(n.Description...)))
			} else {
				builder.WriteString(n.URL)
			}
		case org.Emphasis:
			builder.WriteString(strings.TrimSpace(org.String(n.Content...)))
		case org.Block:
			for _, child := range n.Children {
				if !collectText(child) {
					return false
				}
			}
			builder.WriteString(" ")
		default:
			if children := getChildren(node); children != nil {
				for _, child := range children {
					if !collectText(child) {
						return false
					}
				}
			}
		}
		return builder.Len() < maxLen
	}

	for _, node := range doc.Nodes {
		if !collectText(node) {
			break
		}
	}

	text := builder.String()

	re := regexp.MustCompile(`([.!?])([A-Za-z])`)
	text = re.ReplaceAllString(text, "$1 $2")

	text = strings.Join(strings.Fields(text), " ")

	if len(text) > maxLen {
		cutAt := maxLen
		for cutAt > 0 && text[cutAt-1] > 127 {
			cutAt--
		}
		return text[:cutAt] + "..."
	}

	return text
}

// getChildren extracts child nodes from different org node types.
func getChildren(node org.Node) []org.Node {
	switch n := node.(type) {
	case org.Paragraph:
		return n.Children
	case org.Headline:
		return n.Children
	case org.Block:
		return n.Children
	default:
		return nil
	}
}

// isValidUUID checks if a string is a valid UUID format.
func isValidUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	if s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return false
	}
	for _, c := range s {
		if c != '-' && !isHexChar(byte(c)) {
			return false
		}
	}
	return true
}

// isHexChar checks if a byte is a valid hex character.
func isHexChar(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}
