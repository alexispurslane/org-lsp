package server

import (
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/alexispurslane/go-org/org"
	"github.com/alexispurslane/org-lsp/orgscanner"
	glsp "github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func textDocumentCompletion(glspCtx *glsp.Context, params *protocol.CompletionParams) (any, error) {
	slog.Debug("textDocument/completion handler called", "uri", params.TextDocument.URI, "line", params.Position.Line, "char", params.Position.Character)
	if serverState == nil {
		slog.Error("Server state is nil in completion")
		return nil, nil
	}

	uri := params.TextDocument.URI
	doc, found := serverState.OpenDocs[uri]
	if !found {
		slog.Debug("Document not in OpenDocs", "uri", uri)
		return nil, nil
	}

	// Check completion context - are we in "id:" or ":tag:" completion?
	ctx := detectCompletionContext(doc, uri, params.Position)

	if ctx.Type == "" {
		return &protocol.CompletionList{
			IsIncomplete: false,
			Items:        []protocol.CompletionItem{},
		}, nil
	}

	items := []protocol.CompletionItem{}

	switch ctx.Type {
	case ContextTypeID:
		items = completeIDs(ctx)
	case ContextTypeTag:
		items = completeTags(doc, params.Position, ctx)
	case ContextTypeFile:
		items = completeFiles(ctx)
	case ContextTypeBlock:
		items = completeBlockTypes(ctx, params.Position)
	case ContextTypeExport:
		items = completeExportTypes(ctx, params.Position)
	default:
		return nil, nil
	}

	return &protocol.CompletionList{
		IsIncomplete: false,
		Items:        items,
	}, nil
}

func detectCompletionContext(doc *org.Document, uri protocol.DocumentUri, pos protocol.Position) CompletionContext {
	// First check if we're in a tag context (on headline line)
	headline, found := findNodeAtPosition[org.Headline](doc, pos)
	if found {
		// Cursor must be on the headline's first line (where the * is)
		if headline.Pos.StartLine == int(pos.Line) {
			// Now check if we're AFTER the headline title text (not at beginning)
			return detectTagContext(doc, pos, headline)
		}
	}

	// Check if we're in an export block completion context (must be before block context)
	exportCtx := detectExportBlockContext(doc, uri, pos)
	if exportCtx.Type != ContextTypeNone {
		return exportCtx
	}

	// Check if we're in a block type completion context
	blockCtx := detectBlockContext(doc, uri, pos)
	if blockCtx.Type != ContextTypeNone {
		return blockCtx
	}

	// Check if we're in a file link completion context
	fileCtx := detectFileContext(doc, uri, pos)
	if fileCtx.Type != ContextTypeNone {
		return fileCtx
	}

	// Check if we're in an ID link completion context by examining text before cursor
	return detectIDContext(doc, uri, pos)
}

// detectPrefixContext is a generic helper that checks if cursor is after a specific prefix
func detectPrefixContext(doc *org.Document, uri protocol.DocumentUri, pos protocol.Position, prefix string, ctxType CompletionContextType, checkClosingBrackets bool) CompletionContext {
	ctx := CompletionContext{Type: ContextTypeNone}

	// Get raw content to check text before cursor
	content, found := serverState.RawContent[uri]
	if !found {
		return ctx
	}

	lines := strings.Split(content, "\n")
	if int(pos.Line) >= len(lines) {
		return ctx
	}

	line := lines[pos.Line]
	if int(pos.Character) > len(line) {
		return ctx
	}

	textBeforeCursor := line[:pos.Character]

	// Find prefix
	idx := strings.LastIndex(textBeforeCursor, prefix)
	if idx == -1 {
		return ctx
	}

	ctx.Type = ctxType
	// Extract filter text after prefix, with bounds check
	filterStart := idx + len(prefix)
	if filterStart <= len(textBeforeCursor) {
		ctx.FilterPrefix = strings.TrimSpace(textBeforeCursor[filterStart:])
	}

	// Check if closing brackets already exist after cursor (for links)
	if checkClosingBrackets {
		if int(pos.Character) < len(line) {
			textAfterCursor := line[pos.Character:]
			ctx.NeedsClosingBracket = !strings.HasPrefix(textAfterCursor, "]]")
		} else {
			ctx.NeedsClosingBracket = true
		}
	}

	return ctx
}

// detectBlockContext checks if cursor is in a block type completion context (after "#+begin_")
func detectBlockContext(doc *org.Document, uri protocol.DocumentUri, pos protocol.Position) CompletionContext {
	return detectPrefixContext(doc, uri, pos, "#+begin_", ContextTypeBlock, false)
}

// detectExportBlockContext checks if cursor is in an export block completion context (after "#+begin_export_")
func detectExportBlockContext(doc *org.Document, uri protocol.DocumentUri, pos protocol.Position) CompletionContext {
	return detectPrefixContext(doc, uri, pos, "#+begin_export_", ContextTypeExport, false)
}

// detectFileContext checks if cursor is in a file link completion context (after "[[file:")
func detectFileContext(doc *org.Document, uri protocol.DocumentUri, pos protocol.Position) CompletionContext {
	return detectPrefixContext(doc, uri, pos, "[[file:", ContextTypeFile, true)
}

// detectIDContext checks if cursor is in an ID completion context (after "[[id:")
func detectIDContext(doc *org.Document, uri protocol.DocumentUri, pos protocol.Position) CompletionContext {
	ctx := detectPrefixContext(doc, uri, pos, "[[id:", ContextTypeID, true)
	// ID context uses lowercase filter for case-insensitive matching
	ctx.FilterPrefix = strings.ToLower(ctx.FilterPrefix)
	return ctx
}

// detectTagContext checks if cursor is in a valid tag position (after headline text)
func detectTagContext(doc *org.Document, pos protocol.Position, headline *org.Headline) CompletionContext {
	// Tags appear at the end of the headline line, after the title
	// Check if position is after the headline title ends
	// In org, Headline.Pos.EndLine is calculated based on content
	// For tag completion, we need cursor to be on the headline line itself (checked above)
	// AND after some text (not right after asterisk)

	cursorCol := int(pos.Character)

	// If cursor is at or before the asterisk + space, not in tag context
	// Headline lines look like: "* Title          :tag:"
	if cursorCol < 2 { // Too early on line
		return CompletionContext{Type: ContextTypeNone}
	}

	return CompletionContext{
		Type:                ContextTypeTag,
		FilterPrefix:        "",
		NeedsClosingBracket: false,
	}
}

func completeIDs(ctx CompletionContext) []protocol.CompletionItem {
	if serverState.Scanner == nil || serverState.Scanner.ProcessedFiles == nil {
		return nil
	}

	var items []protocol.CompletionItem

	// Walk through all UUIDs in the index
	serverState.Scanner.ProcessedFiles.UuidIndex.Range(func(key, value any) bool {
		uuid := string(key.(orgscanner.UUID))
		location := value.(orgscanner.HeaderLocation)

		// Use the header title from the location (now available in UUID index)
		title := location.Title
		if title == "" {
			title = "Untitled"
		}

		// Filter by title if user has typed something after the prefix
		if ctx.FilterPrefix != "" {
			titleLower := strings.ToLower(title)
			if !strings.Contains(titleLower, ctx.FilterPrefix) {
				return true // Skip this item, continue iteration
			}
		}

		// Generate hover preview for this header as documentation
		preview := extractContextLinesForCompletion(location)

		// Build insert text: UUID + closing brackets if needed
		insertText := uuid
		if ctx.NeedsClosingBracket {
			insertText = uuid + "]]"
		}

		// Create completion item with title as label, UUID as insert text
		item := protocol.CompletionItem{
			Label:      title, // User sees heading title
			Kind:       ptrTo(protocol.CompletionItemKindReference),
			Detail:     strPtr("ID Link"), // Type indicator
			InsertText: &insertText,       // Full UUID inserted (+ closing brackets)
			Documentation: protocol.MarkupContent{
				Kind:  protocol.MarkupKindMarkdown,
				Value: preview,
			},
		}

		items = append(items, item)
		return true // continue iteration
	})

	return items
}

// extractContextLinesForCompletion generates hover preview for completion items
// Excludes header and properties list, since the former is already included in
// the completion item's name, and the latter is useless, so starts 4 lines
// *after*
func extractContextLinesForCompletion(loc orgscanner.HeaderLocation) string {
	absPath := filepath.Join(serverState.OrgScanRoot, loc.FilePath)
	absPath = filepath.Clean(absPath)

	lines, err := readFileLines(absPath)
	if err != nil {
		return ""
	}

	var context strings.Builder
	context.WriteString("**")
	context.WriteString(loc.Title)
	context.WriteString("**\n\n```org\n")

	// Show header line and content below it
	startLine := loc.Position.StartLine + 1 // Exclude title
	numLines := 4
	readLines := 0
	inProperties := false

	for _, line := range lines[startLine:] {
		if readLines >= numLines {
			break
		}

		if strings.Contains(line, ":PROPERTIES:") {
			inProperties = true
		} else if strings.Contains(line, ":END:") {
			inProperties = false
			continue
		}

		if inProperties {
			continue
		}

		context.WriteString(line)
		readLines += 1
	}

	context.WriteString("\n```")
	return context.String()
}

func completeTags(doc *org.Document, pos protocol.Position, ctx CompletionContext) []protocol.CompletionItem {
	if serverState.Scanner == nil || serverState.Scanner.ProcessedFiles == nil {
		return nil
	}

	var items []protocol.CompletionItem
	seenTags := make(map[string]bool)

	// Collect all unique tags from TagMap
	serverState.Scanner.ProcessedFiles.TagMap.Range(func(key, value any) bool {
		tag := key.(string)

		if !seenTags[tag] {
			seenTags[tag] = true

			item := protocol.CompletionItem{
				Label:      tag,
				Kind:       ptrTo(protocol.CompletionItemKindProperty),
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
func completeFiles(ctx CompletionContext) []protocol.CompletionItem {
	if serverState.Scanner == nil || serverState.Scanner.ProcessedFiles == nil {
		return nil
	}

	var items []protocol.CompletionItem
	filterLower := strings.ToLower(ctx.FilterPrefix)

	// Walk through all processed files
	for _, fileInfo := range serverState.Scanner.ProcessedFiles.Files {
		// Filter by partial path prefix (case-insensitive)
		filePathLower := strings.ToLower(fileInfo.Path)
		if filterLower != "" && !strings.Contains(filePathLower, filterLower) {
			continue
		}

		// Create completion item
		item := protocol.CompletionItem{
			Label:  fileInfo.Path,
			Kind:   ptrTo(protocol.CompletionItemKindFile),
			Detail: strPtr("File"),
		}

		// Insert text is just the path, then add closing bracket if needed
		insertText := fileInfo.Path
		if ctx.NeedsClosingBracket {
			insertText = insertText + "]]"
		}
		item.InsertText = strPtr(insertText)

		items = append(items, item)
	}

	slog.Debug("File completion generated", "itemCount", len(items), "filter", ctx.FilterPrefix)
	return items
}

// completeBlockTypes returns completion items for block types (#+begin_)
func completeBlockTypes(ctx CompletionContext, pos protocol.Position) []protocol.CompletionItem {
	blockTypes := []string{"quote", "src", "verse"}

	var items []protocol.CompletionItem
	filterLower := strings.ToLower(ctx.FilterPrefix)

	// Calculate the start of "#+begin_" prefix for TextEdit range
	// "#+begin_" is 8 characters, plus whatever filter prefix was typed
	prefixLen := 8 + len(ctx.FilterPrefix)
	startChar := max(int(pos.Character)-prefixLen, 0)

	for _, blockType := range blockTypes {
		// Filter by partial match (case-insensitive)
		if filterLower != "" && !strings.Contains(strings.ToLower(blockType), filterLower) {
			continue
		}

		fullLabel := "#+begin_" + blockType
		item := protocol.CompletionItem{
			Label:  fullLabel,
			Kind:   ptrTo(protocol.CompletionItemKindKeyword),
			Detail: strPtr("Block type"),
		}

		// Use TextEdit to replace the entire "#+begin_XXX" prefix
		insertText := fullLabel + "\n\n#+end_" + blockType
		item.TextEdit = &protocol.TextEdit{
			Range: protocol.Range{
				Start: protocol.Position{
					Line:      pos.Line,
					Character: protocol.UInteger(startChar),
				},
				End: protocol.Position{
					Line:      pos.Line,
					Character: pos.Character,
				},
			},
			NewText: insertText,
		}

		items = append(items, item)
	}

	slog.Debug("Block type completion generated", "itemCount", len(items), "filter", ctx.FilterPrefix)
	return items
}

// completeExportTypes returns completion items for export block types (#+begin_export_)
func completeExportTypes(ctx CompletionContext, pos protocol.Position) []protocol.CompletionItem {
	exportTypes := []string{"html", "latex"}

	var items []protocol.CompletionItem
	filterLower := strings.ToLower(ctx.FilterPrefix)

	// Calculate the start of "#+begin_export_" prefix for TextEdit range
	// "#+begin_export_" is 15 characters, plus whatever filter prefix was typed
	prefixLen := 15 + len(ctx.FilterPrefix)
	startChar := max(int(pos.Character)-prefixLen, 0)

	for _, exportType := range exportTypes {
		// Filter by partial match (case-insensitive)
		if filterLower != "" && !strings.Contains(strings.ToLower(exportType), filterLower) {
			continue
		}

		fullLabel := "#+begin_export_" + exportType
		item := protocol.CompletionItem{
			Label:  fullLabel,
			Kind:   ptrTo(protocol.CompletionItemKindKeyword),
			Detail: strPtr("Export format"),
		}

		// Use TextEdit to replace the entire "#+begin_export_XXX" prefix
		insertText := fullLabel + "\n\n#+end_export"
		item.TextEdit = &protocol.TextEdit{
			Range: protocol.Range{
				Start: protocol.Position{
					Line:      pos.Line,
					Character: protocol.UInteger(startChar),
				},
				End: protocol.Position{
					Line:      pos.Line,
					Character: pos.Character,
				},
			},
			NewText: insertText,
		}

		items = append(items, item)
	}

	slog.Debug("Export type completion generated", "itemCount", len(items), "filter", ctx.FilterPrefix)
	return items
}
