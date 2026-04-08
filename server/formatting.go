// Package server provides the LSP server implementation for org-mode files.
package server

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"reflect"
	"regexp"
	"strings"

	"github.com/alexispurslane/go-org/org"
	protocol "go.lsp.dev/protocol"
)

// Formatting handles textDocument/formatting requests.
// It ensures all headings have UUIDs, normalizes spacing, aligns tags,
// and applies other org-mode formatting conventions.
func (s *ServerImpl) Formatting(ctx context.Context, params *protocol.DocumentFormattingParams) (result []protocol.TextEdit, err error) {
	if s.state == nil {
		return nil, fmt.Errorf("server not initialized")
	}

	s.state.Mu.RLock()
	defer s.state.Mu.RUnlock()

	uri := params.TextDocument.URI
	slog.Debug("Formatting document", "uri", uri)

	// Get the raw content
	content, ok := s.state.RawContent[uri]
	if !ok {
		return nil, fmt.Errorf("document not open: %s", uri)
	}

	// Parse the document
	doc := org.New().Parse(strings.NewReader(content), string(uri))

	// Format the AST recursively
	formattedNodes := formatNodes(doc.Nodes)

	// Serialize the formatted AST back to string
	output := org.String(formattedNodes...)

	// Post-process to fix planning directive indentation
	// The go-org serializer applies default indentation, so we need to override it
	output = fixPlanningDirectiveIndentation(output)

	// Return a single text edit that replaces the entire document
	edit := protocol.TextEdit{
		Range: protocol.Range{
			Start: protocol.Position{Line: 0, Character: 0},
			End:   getEndPosition(content),
		},
		NewText: output,
	}

	slog.Info("Document formatted", "uri", uri)
	return []protocol.TextEdit{edit}, nil
}

// WillSaveWaitUntil handles textDocument/willSaveWaitUntil requests for format-on-save
func (s *ServerImpl) WillSaveWaitUntil(ctx context.Context, params *protocol.WillSaveTextDocumentParams) (result []protocol.TextEdit, err error) {
	formatParams := protocol.DocumentFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: params.TextDocument.URI},
	}
	return s.Formatting(ctx, &formatParams)
}

func (s *ServerImpl) RangeFormatting(ctx context.Context, params *protocol.DocumentRangeFormattingParams) (result []protocol.TextEdit, err error) {
	if s.state == nil {
		return nil, fmt.Errorf("server not initialized")
	}

	s.state.Mu.RLock()
	defer s.state.Mu.RUnlock()

	uri := params.TextDocument.URI
	slog.Debug("Range formatting document", "uri", uri, "range", params.Range)

	// Get the raw content
	content, ok := s.state.RawContent[uri]
	if !ok {
		return nil, fmt.Errorf("document not open: %s", uri)
	}

	// Parse and format the entire document to get proper context
	doc := org.New().Parse(strings.NewReader(content), string(uri))
	formattedNodes := formatNodes(doc.Nodes)
	fullFormatted := org.String(formattedNodes...)

	// Split original and formatted into lines
	originalLines := strings.Split(content, "\n")
	formattedLines := strings.Split(fullFormatted, "\n")

	// Calculate line range bounds
	startLine := int(params.Range.Start.Line)
	endLine := int(params.Range.End.Line)

	// Clamp to valid line range
	if startLine < 0 {
		startLine = 0
	}
	if endLine >= len(originalLines) {
		endLine = len(originalLines) - 1
	}
	if startLine > endLine {
		return nil, nil
	}

	// Ensure we have enough formatted lines
	if endLine >= len(formattedLines) {
		endLine = len(formattedLines) - 1
	}
	if startLine > endLine {
		return nil, nil
	}

	// Extract the formatted content for the specified range
	rangeFormattedLines := formattedLines[startLine : endLine+1]
	rangeFormatted := strings.Join(rangeFormattedLines, "\n")

	// Ensure trailing newline matches original if present
	if strings.HasSuffix(content, "\n") && !strings.HasSuffix(rangeFormatted, "\n") && len(rangeFormatted) > 0 {
		rangeFormatted += "\n"
	}

	// Calculate character positions
	startChar := int(params.Range.Start.Character)
	endChar := int(params.Range.End.Character)

	// Clamp end character for original content
	if endLine < len(originalLines) && endChar > len(originalLines[endLine]) {
		endChar = len(originalLines[endLine])
	}

	// Create the edit for the range
	edit := protocol.TextEdit{
		Range: protocol.Range{
			Start: protocol.Position{
				Line:      uint32(startLine),
				Character: uint32(startChar),
			},
			End: protocol.Position{
				Line:      uint32(endLine),
				Character: uint32(endChar),
			},
		},
		NewText: rangeFormatted,
	}

	slog.Info("Range formatted", "uri", uri, "lines", fmt.Sprintf("%d-%d", startLine, endLine))
	return []protocol.TextEdit{edit}, nil
}

// needsSpaceBefore checks if a node is an inline element that typically needs
// a space before it in normal prose (excluding cases where it's at the start of a line).
// This includes links, timestamps, footnote references, and statistic tokens.
func needsSpaceBefore(n org.Node) bool {
	switch n.(type) {
	case org.RegularLink, org.FootnoteLink, org.Timestamp, org.StatisticToken,
		org.Emphasis, org.InlineBlock:
		return true
	default:
		return false
	}
}

// formatNodes processes a list of nodes, handling inter-node concerns:
// - Filtering empty paragraphs
// - Consolidating keywords at document level
// - Inserting blank lines before headings
// - Preserving trailing spaces before inline elements
func formatNodes(nodes []org.Node) []org.Node {
	if len(nodes) == 0 {
		return nodes
	}

	result := make([]org.Node, 0, len(nodes))
	var keywords []org.Node

	// First pass: collect keywords at document level
	nonKeywords := make([]org.Node, 0, len(nodes))
	for _, n := range nodes {
		if isKeyword(n) {
			keywords = append(keywords, formatNode(n))
		} else {
			nonKeywords = append(nonKeywords, n)
		}
	}
	// Keywords go first, then everything else
	nodes = append(keywords, nonKeywords...)

	for i, n := range nodes {
		// Skip nil nodes
		if n == nil {
			continue
		}

		// Skip empty paragraphs - the serializer will add proper spacing
		if p, ok := n.(org.Paragraph); ok && len(p.Children) == 0 {
			continue
		}

		// Ensure blank line before headings (except at document start)
		if isHeadline(n) && i > 0 {
			result = append(result, org.Text{Content: "\n"})
		}

		// Format the individual node (which recursively formats its children)
		formatted := formatNode(n)

		result = append(result, formatted)
	}

	return result
}

// formatNode processes a single node and recursively formats its children.
// Uses reflection to find and format Children fields on any node type.
func formatNode(n org.Node) org.Node {
	if n == nil {
		return nil
	}

	// First, format the node itself based on its type
	var formatted org.Node
	switch node := n.(type) {
	case org.Headline:
		formatted = formatHeadline(node)
	case org.Paragraph:
		formatted = formatParagraph(node)
	case org.Table:
		formatted = formatTable(node)
	case org.List:
		formatted = formatList(node)
	case org.Block:
		formatted = formatBlock(node)
	case org.Keyword:
		formatted = formatKeyword(node)
	case org.PropertyDrawer:
		formatted = formatPropertyDrawer(node)
	default:
		formatted = n
	}

	// Then, use reflection to recursively format any Children fields
	return formatChildren(formatted)
}

// formatChildren uses reflection to find []org.Node Children fields
// and recursively format them. Returns the node with formatted children.
func formatChildren(n org.Node) org.Node {
	if n == nil {
		return nil
	}

	v := reflect.ValueOf(n)

	// Must be a struct to have a Children field
	if v.Kind() != reflect.Struct {
		return n
	}

	// Look for a Children field of type []org.Node
	childrenField := v.FieldByName("Children")
	if !childrenField.IsValid() {
		return n
	}
	if childrenField.Kind() != reflect.Slice {
		return n
	}
	if childrenField.Type().Elem() != reflect.TypeFor[org.Node]() {
		return n
	}

	// Convert to []org.Node and format
	children := childrenField.Interface().([]org.Node)
	if len(children) == 0 {
		return n
	}

	formattedChildren := formatNodes(children)

	// Create a new node with the formatted children
	newNode := reflect.New(v.Type()).Elem()
	newNode.Set(v)
	newNode.FieldByName("Children").Set(reflect.ValueOf(formattedChildren))

	return newNode.Interface().(org.Node)
}

// formatHeadline ensures UUID, normalizes TODO spacing, aligns tags, formats property drawer
func formatHeadline(h org.Headline) org.Node {
	// Ensure UUID property exists
	h = ensureHeadlineUUID(h)

	// Normalize TODO keyword spacing: "* TODO Heading" not "*  TODO   Heading"
	h.Status = normalizeSpaces(h.Status)

	// Remove trailing whitespace from title
	for i := range h.Title {
		if text, ok := h.Title[i].(org.Text); ok {
			text.Content = strings.TrimRight(text.Content, " \t")
			h.Title[i] = text
		}
	}

	// Align tags to consistent column (default: column 77, or max line length + 1)
	h.Tags = normalizeTags(h.Tags)

	// Format property drawer if present and ensure blank line after
	hasPropertyDrawer := h.Properties != nil
	if hasPropertyDrawer {
		formatted := formatPropertyDrawer(*h.Properties)
		if pd, ok := formatted.(org.PropertyDrawer); ok {
			h.Properties = &pd
		}
	}

	// Add blank line after property drawer if present and there are children
	if hasPropertyDrawer && len(h.Children) > 0 {
		// Prepend a blank line as the first child
		h.Children = append([]org.Node{org.Text{Content: "\n"}}, h.Children...)
	}

	return h
}

// ensureHeadlineUUID adds an :ID: property if missing
func ensureHeadlineUUID(h org.Headline) org.Headline {
	if hasIDProperty(h) {
		return h
	}

	newID := generateUUID()

	if h.Properties == nil {
		h.Properties = &org.PropertyDrawer{
			Properties: [][]string{},
		}
	}

	h.Properties.Properties = append(h.Properties.Properties, []string{"ID", newID})
	return h
}

// hasIDProperty checks if a heading already has an :ID: property
func hasIDProperty(h org.Headline) bool {
	if h.Properties != nil {
		for _, prop := range h.Properties.Properties {
			if len(prop) >= 1 && prop[0] == "ID" {
				return true
			}
		}
	}
	return false
}

// formatText removes trailing whitespace from each line and collapses
// more than 2 consecutive blank lines to exactly 2.
func formatParagraph(p org.Paragraph) org.Node {
	// Match: [Non-Space][Marker][Non-Space]
	stuckEmphasis := regexp.MustCompile(`([^\s])([*/=_\+])([^\s])`)
	for i, current := range p.Children {
		var next org.Node
		if i < len(p.Children)-1 {
			next = p.Children[i+1]
		}

		fmt.Printf("Current type: %s, next type: %s, string value: %s\n", reflect.TypeOf(current), reflect.TypeOf(next), org.String(current))

		if textNode, isText := current.(org.Text); isText {
			if needsSpaceBefore(next) && !strings.HasSuffix(textNode.Content, " ") {
				textNode.Content += " "
			}
			if strings.HasSuffix(textNode.Content, "\n") || next == nil {
				textNode.Content = strings.TrimRight(textNode.Content, " \t")
			}
			if trimmed := strings.TrimLeft(textNode.Content, " \t"); trimmed[0] == '.' || trimmed[0] == ',' || trimmed[0] == '!' || trimmed[0] == '?' {
				textNode.Content = trimmed
			}
			// Replace with: [Non-Space] [Marker][Non-Space]
			textNode.Content = stuckEmphasis.ReplaceAllString(textNode.Content, "$1 $2$3")
			p.Children[i] = textNode
		}
	}
	return p
}

// formatTable aligns column widths
func formatTable(t org.Table) org.Node {
	if len(t.Rows) == 0 {
		return t
	}

	// Calculate max width for each column
	colWidths := calculateColumnWidths(t.Rows)

	// Reformat each row with aligned columns
	for i := range t.Rows {
		t.Rows[i] = alignRow(t.Rows[i], colWidths)
	}

	return t
}

// calculateColumnWidths finds the maximum string content width for each column
func calculateColumnWidths(rows []org.Row) []int {
	if len(rows) == 0 {
		return nil
	}

	maxCols := 0
	for _, row := range rows {
		if len(row.Columns) > maxCols {
			maxCols = len(row.Columns)
		}
	}

	widths := make([]int, maxCols)
	for _, row := range rows {
		for j, col := range row.Columns {
			if j < len(widths) {
				// Render column content to string to measure
				content := org.String(col.Children...)
				content = strings.TrimSpace(content)
				if len(content) > widths[j] {
					widths[j] = len(content)
				}
			}
		}
	}

	return widths
}

// alignRow pads column content to align with calculated widths
func alignRow(row org.Row, widths []int) org.Row {
	for i := range row.Columns {
		if i < len(widths) {
			// Render current content
			content := org.String(row.Columns[i].Children...)
			content = strings.TrimSpace(content)

			// Pad with spaces to reach width (adding spaces on both sides)
			paddedContent := fmt.Sprintf(" %-*s ", widths[i], content)

			// Create a Text node with the padded content
			row.Columns[i].Children = []org.Node{org.Text{Content: paddedContent}}
		}
	}
	return row
}

// formatList normalizes list item indentation
func formatList(l org.List) org.Node {
	// Children are handled by formatChildren via reflection
	return l
}

// formatBlock handles code/example/quote blocks - preserves content
func formatBlock(b org.Block) org.Node {
	// Preserve block content exactly - don't format children
	return b
}

// formatKeyword normalizes keyword spacing
func formatKeyword(k org.Keyword) org.Node {
	// Normalize: "#+KEY: value" with single space after colon
	k.Key = strings.TrimSpace(k.Key)
	k.Value = strings.TrimLeft(k.Value, " ")
	return k
}

// formatPropertyDrawer normalizes property drawer indentation
func formatPropertyDrawer(p org.PropertyDrawer) org.Node {
	slog.Debug("formatPropertyDrawer called", "numProps", len(p.Properties))
	// Ensure all properties start at column 0 with no leading spaces
	for i := range p.Properties {
		if len(p.Properties[i]) >= 2 {
			oldKey := p.Properties[i][0]
			p.Properties[i][0] = strings.TrimSpace(p.Properties[i][0])
			p.Properties[i][1] = strings.TrimSpace(p.Properties[i][1])
			slog.Debug("Formatted property", "oldKey", oldKey, "newKey", p.Properties[i][0])
		}
	}
	return p
}

// Helper functions

// isHeadline checks if a node is a Headline
func isHeadline(n org.Node) bool {
	_, ok := n.(org.Headline)
	return ok
}

// isKeyword checks if a node is a Keyword
func isKeyword(n org.Node) bool {
	_, ok := n.(org.Keyword)
	return ok
}

// normalizeSpaces trims extra spaces and ensures single spacing
func normalizeSpaces(s string) string {
	s = strings.TrimSpace(s)
	// Collapse multiple spaces to single
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return s
}

// normalizeTags aligns tags to a consistent column
func normalizeTags(tags []string) []string {
	// The go-org serializer adds colons automatically, so we just ensure clean tag names
	result := make([]string, len(tags))
	for i, tag := range tags {
		tag = strings.TrimSpace(tag)
		// Strip any existing colons from both ends (serializer will add them)
		tag = strings.Trim(tag, ":")
		result[i] = tag
	}
	return result
}

// generateUUID creates a new UUID v4 string
func generateUUID() string {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		panic(err)
	}
	// Set version (4) and variant bits according to RFC 4122
	b[6] = (b[6] & 0x0f) | 0x40 // Version 4
	b[8] = (b[8] & 0x3f) | 0x80 // Variant is 10

	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// fixPlanningDirectiveIndentation post-processes the serialized content to ensure
// planning directives (DEADLINE, SCHEDULED, CLOCK, CLOSED) are indented by heading-level+1 spaces
func fixPlanningDirectiveIndentation(content string) string {
	lines := strings.Split(content, "\n")
	currentLevel := 0
	planningKeywords := []string{"DEADLINE", "SCHEDULED", "CLOCK", "CLOSED"}

	for i, line := range lines {
		if level := getHeadingLevel(line); level > 0 {
			currentLevel = level
			continue
		}

		stripped := strings.TrimSpace(line)
		if stripped == "" {
			continue
		}

		for _, keyword := range planningKeywords {
			if strings.HasPrefix(stripped, keyword+":") {
				if currentLevel > 0 {
					indentation := strings.Repeat(" ", currentLevel+1)
					lines[i] = indentation + stripped
				}
				break
			}
		}
	}

	return strings.Join(lines, "\n")
}

// getHeadingLevel extracts the heading level from a line
// Returns 0 if the line is not a heading
func getHeadingLevel(line string) int {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "*") {
		return 0
	}
	level := 0
	for _, ch := range line {
		if ch == '*' {
			level++
		} else if ch != ' ' {
			break
		}
	}
	return level
}

// formatPlanningDirectives ensures planning directives (DEADLINE, SCHEDULED, CLOCK, CLOSED)
// are indented by heading-level+1 spaces

// getEndPosition calculates the end position of the document content
func getEndPosition(content string) protocol.Position {
	lines := strings.Split(content, "\n")
	lastLine := len(lines) - 1
	if lastLine < 0 {
		lastLine = 0
	}

	lastLineLength := 0
	if lastLine < len(lines) {
		lastLineLength = len(lines[lastLine])
	}

	return protocol.Position{
		Line:      uint32(lastLine),
		Character: uint32(lastLineLength),
	}
}
