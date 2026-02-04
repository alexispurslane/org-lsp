package server

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/alexispurslane/go-org/org"
	"github.com/alexispurslane/org-lsp/orgscanner"
	protocol "go.lsp.dev/protocol"
)

func (s *ServerImpl) DocumentSymbol(ctx context.Context, params *protocol.DocumentSymbolParams) (result []interface{}, err error) {
	slog.Debug("DocumentSymbol handler called", "uri", params.TextDocument.URI)
	if serverState == nil {
		slog.Error("Server state is nil in documentSymbol")
		return nil, nil
	}
	serverState.Mu.RLock()
	defer serverState.Mu.RUnlock()

	uri := params.TextDocument.URI
	doc, found := serverState.OpenDocs[uri]
	if !found {
		slog.Debug("Document not in OpenDocs", "uri", uri)
		return nil, nil
	}

	// Convert outline sections to document symbols
	symbols := sectionsToSymbols(doc.Outline.Children)

	// Convert []DocumentSymbol to []interface{}
	result = make([]interface{}, len(symbols))
	for i, symbol := range symbols {
		result[i] = symbol
	}

	slog.Debug("Document symbols generated", "uri", uri, "count", len(result))
	return result, nil
}

func (s *ServerImpl) Symbols(ctx context.Context, params *protocol.WorkspaceSymbolParams) (result []protocol.SymbolInformation, err error) {
	slog.Info("🔍 WORKSPACE/SYMBOL HANDLER CALLED", "query", params.Query, "queryEmpty", params.Query == "")

	if serverState == nil {
		slog.Error("❌ serverState is NIL")
		return nil, nil
	}
	slog.Info("✅ serverState exists", "orgScanRoot", serverState.OrgScanRoot)

	if serverState.Scanner == nil || serverState.Scanner.ProcessedFiles == nil {
		slog.Error("❌ serverState.Scanner or ProcessedFiles is NIL")
		return nil, nil
	}

	query := strings.ToLower(params.Query)
	var symbols []protocol.SymbolInformation
	matchCount := 0
	skipCount := 0

	// Iterate through all UUID-indexed headers
	serverState.Scanner.ProcessedFiles.UuidIndex.Range(func(key, value any) bool {
		uuidKey, ok := key.(orgscanner.UUID)
		if !ok {
			slog.Warn("⚠️ UUID key is not an orgscanner.UUID", "key", key, "keyType", fmt.Sprintf("%T", key))
			return true
		}
		uuid := string(uuidKey)

		location, ok := value.(orgscanner.HeaderLocation)
		if !ok {
			slog.Warn("⚠️ Value is not HeaderLocation", "uuid", uuid, "valueType", fmt.Sprintf("%T", value))
			skipCount++
			return true // Skip invalid entries
		}

		slog.Debug("Processing entry", "uuid", uuid, "title", location.Title, "filePath", location.FilePath)

		// Substring match on title
		titleLower := strings.ToLower(location.Title)
		matches := query == "" || strings.Contains(titleLower, query)

		if !matches {
			slog.Debug("❌ No match", "title", location.Title, "query", query)
			skipCount++
		} else {
			slog.Info("✅ MATCH FOUND", "title", location.Title, "query", query, "uuid", uuid)
			uri := pathToURI(location.FilePath)
			slog.Debug("Converted path to URI", "path", location.FilePath, "uri", uri)

			symbol := protocol.SymbolInformation{
				Name: location.Title,
				Kind: levelToSymbolKind(location.Level),
				Location: protocol.Location{
					URI: protocol.DocumentURI(uri),
					Range: protocol.Range{
						Start: protocol.Position{
							Line:      uint32(location.Position.StartLine),
							Character: uint32(location.Position.StartColumn),
						},
						End: protocol.Position{
							Line:      uint32(location.Position.EndLine),
							Character: uint32(location.Position.EndColumn),
						},
					},
				},
			}
			symbols = append(symbols, symbol)
			matchCount++
		}
		return true // Continue iteration
	})

	slog.Info("🏁 WORKSPACE/SYMBOL COMPLETE",
		"query", query,
		"symbolsReturned", len(symbols),
		"matches", matchCount,
		"skipped", skipCount)
	return symbols, nil
}

// sectionsToSymbols converts a slice of org.Section to DocumentSymbol slice
func sectionsToSymbols(sections []*org.Section) []protocol.DocumentSymbol {
	if len(sections) == 0 {
		return nil
	}

	symbols := make([]protocol.DocumentSymbol, 0, len(sections))
	for _, section := range sections {
		if section.Headline == nil {
			continue
		}

		symbol := sectionToSymbol(section)
		symbols = append(symbols, symbol)
	}

	return symbols
}

// sectionToSymbol converts a single org.Section to DocumentSymbol
func sectionToSymbol(section *org.Section) protocol.DocumentSymbol {
	headline := section.Headline

	// Render title nodes to string
	name := renderNodesToString(headline.Title)

	// Map heading level to SymbolKind
	kind := levelToSymbolKind(headline.Lvl)

	// Create range from headline position
	selectionRange := protocol.Range{
		Start: protocol.Position{
			Line:      uint32(headline.Pos.StartLine),
			Character: uint32(headline.Pos.StartColumn),
		},
		End: protocol.Position{
			Line:      uint32(headline.Pos.EndLine),
			Character: uint32(headline.Pos.EndColumn),
		},
	}

	// Full range includes the section content, not just the headline
	// For now, use same as selection range (can be refined later)
	fullRange := selectionRange

	// Build detail string from tags
	detail := ""
	if len(headline.Tags) > 0 {
		detail = strings.Join(headline.Tags, " ")
	}

	symbol := protocol.DocumentSymbol{
		Name:           name,
		Detail:         detail, // Tags as detail if any
		Kind:           kind,
		Range:          fullRange,
		SelectionRange: selectionRange,
		Children:       sectionsToSymbols(section.Children),
	}

	return symbol
}

// levelToSymbolKind maps org heading levels to LSP SymbolKind
func levelToSymbolKind(lvl int) protocol.SymbolKind {
	switch lvl {
	case 1:
		return protocol.SymbolKindNamespace
	case 2:
		return protocol.SymbolKindClass
	case 3:
		return protocol.SymbolKindMethod
	case 4:
		return protocol.SymbolKindProperty
	default:
		return protocol.SymbolKindField // Level 5+
	}
}

// renderNodesToString renders a slice of org nodes to a plain string
// This is a simple renderer that extracts text from text nodes
func renderNodesToString(nodes []org.Node) string {
	if len(nodes) == 0 {
		return ""
	}

	var builder strings.Builder
	for _, node := range nodes {
		renderNode(&builder, node)
	}

	return strings.TrimSpace(builder.String())
}

// renderNode recursively renders a single node
func renderNode(builder *strings.Builder, node org.Node) {
	if node == nil {
		return
	}

	switch n := node.(type) {
	case org.Text:
		builder.WriteString(n.Content)
	case org.Emphasis:
		// Emphasis handles bold (*), italic (/), underline (_), strike-through (+)
		// code (~), verbatim (=), subscript (_{}), and superscript (^{})
		for _, child := range n.Content {
			renderNode(builder, child)
		}
	case org.InlineBlock:
		// Inline blocks (~code~ or =verbatim=)
		for _, child := range n.Children {
			renderNode(builder, child)
		}
	case org.RegularLink:
		// For links, show the description if available, otherwise the URL
		if len(n.Description) > 0 {
			for _, child := range n.Description {
				renderNode(builder, child)
			}
		} else {
			builder.WriteString(n.URL)
		}
	case org.Timestamp:
		// Format timestamp using Go's time formatting
		builder.WriteString(n.Time.Format("2006-01-02"))
	case org.Macro:
		builder.WriteString("{{{")
		builder.WriteString(n.Name)
		for _, param := range n.Parameters {
			builder.WriteString("(")
			builder.WriteString(param)
			builder.WriteString(")")
		}
		builder.WriteString("}}}")
	case org.LineBreak:
		builder.WriteString(" ")
	case org.StatisticToken:
		builder.WriteString(n.Content)
	case org.LatexFragment:
		for _, child := range n.Content {
			renderNode(builder, child)
		}
	case org.FootnoteLink:
		builder.WriteString("[fn:")
		builder.WriteString(n.Name)
		builder.WriteString("]")
	default:
		// For unknown nodes that might have Children, try to extract text
		// This handles Paragraph, Table, List, etc. recursively
		if nodeWithChildren, ok := node.(interface{ GetChildren() []org.Node }); ok {
			for _, child := range nodeWithChildren.GetChildren() {
				renderNode(builder, child)
			}
		}
	}
}
