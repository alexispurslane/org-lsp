// Package server provides the LSP server implementation for org-mode files.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"

	"github.com/alexispurslane/go-org/org"
	protocol "go.lsp.dev/protocol"
)

func (s *ServerImpl) CodeAction(ctx context.Context, params *protocol.CodeActionParams) (result []protocol.CodeAction, err error) {
	if serverState == nil {
		return nil, nil
	}
	serverState.Mu.RLock()
	defer serverState.Mu.RUnlock()

	uri := params.TextDocument.URI
	doc, ok := serverState.OpenDocs[uri]
	if !ok {
		return nil, nil
	}

	var actions []protocol.CodeAction

	// Convert selection range to org positions
	startLine := int(params.Range.Start.Line)
	endLine := int(params.Range.End.Line)

	// Check for heading -> list conversion
	nodesInRange := findNodesInRange(
		doc.Nodes,
		startLine,
		endLine,
	)

	for i, node := range nodesInRange {
		slog.Debug("found node in range", "node", node, "i", i)
	}
	// Filter to only headings
	var headingsInRange bool
	for _, node := range nodesInRange {
		if _, ok := node.(org.Headline); ok {
			headingsInRange = true
			break
		}
	}
	if headingsInRange {
		actions = append(actions, getHeadingConversionActions(nodesInRange, uri)...)
	}

	// Check for list -> heading conversion
	cursorPos := protocol.Position{
		Line:      params.Range.Start.Line,
		Character: params.Range.Start.Character,
	}
	if list, found := findNodeAtPosition[org.List](doc, cursorPos); found {
		actions = append(actions, getListConversionAction(*list, doc, uri, params.Range))
	}

	// Check for code block evaluation (single block at cursor only)
	if block, found := findNodeAtPosition[org.Block](doc, cursorPos); found && strings.EqualFold(block.Name, "src") {
		actions = append(actions, getCodeBlockAction(*block, uri))
	}
	return actions, nil
}

// getHeadingConversionActions returns actions to convert headings to lists.
func getHeadingConversionActions(nodes []org.Node, uri protocol.DocumentURI) []protocol.CodeAction {
	kindRefactor := protocol.RefactorRewrite

	// Build text edits for all selected headings
	var orderedListEdits []protocol.TextEdit
	var bulletListEdits []protocol.TextEdit

	// Get the full subtree range for this heading
	startPos := nodes[0].Position()
	start := protocol.Position{
		Line:      uint32(startPos.StartLine),
		Character: uint32(startPos.StartColumn),
	}
	endPos := nodes[len(nodes)-1].Position()
	end := protocol.Position{
		Line:      uint32(endPos.EndLine),
		Character: uint32(endPos.EndColumn),
	}
	subtreeRange := protocol.Range{
		Start: start,
		End:   end,
	}
	// Convert subtree to ordered list (with proper numbering)
	listPos := org.Position{
		StartLine:   startPos.StartLine,
		StartColumn: startPos.StartColumn,
		EndLine:     endPos.EndLine,
		EndColumn:   endPos.EndColumn,
	}
	orderedText := org.String(headingSubtreeToOrderedList(nodes, listPos)...)
	orderedListEdits = append(orderedListEdits, protocol.TextEdit{
		Range:   subtreeRange,
		NewText: orderedText,
	})

	// Convert subtree to bullet list
	bulletText := org.String(headingSubtreeToUnorderedList(nodes, listPos, 0)...)
	bulletListEdits = append(bulletListEdits, protocol.TextEdit{
		Range:   subtreeRange,
		NewText: bulletText,
	})

	return []protocol.CodeAction{
		{
			Title:       "Convert headings to ordered list",
			Kind:        kindRefactor,
			Diagnostics: nil,
			Edit: &protocol.WorkspaceEdit{
				Changes: map[protocol.DocumentURI][]protocol.TextEdit{
					uri: orderedListEdits,
				},
			},
		},
		{
			Title:       "Convert headings to bullet list",
			Kind:        kindRefactor,
			Diagnostics: nil,
			Edit: &protocol.WorkspaceEdit{
				Changes: map[protocol.DocumentURI][]protocol.TextEdit{
					uri: bulletListEdits,
				},
			},
		},
	}
}

// getListConversionAction returns an action to convert a list to headings.
func getListConversionAction(list org.List, doc *org.Document, uri protocol.DocumentURI, selectionRange protocol.Range) protocol.CodeAction {
	kindRefactor := protocol.RefactorRewrite

	// Find the heading that contains this list to determine appropriate level
	listPos := list.Position()
	startLevel := 1 // Default to level 1 if no parent heading found
	if parentHeading, found := findNodeAtPosition[org.Headline](doc, protocol.Position{
		Line:      uint32(listPos.StartLine), // Convert 1-based to 0-based
		Character: 0,
	}); found {
		startLevel = parentHeading.Lvl + 1
	}

	// Convert list to headings at parent level + 1 (or level 1 if no parent)
	headings := listToHeadingSubtree(list, startLevel)
	newText := org.String(headings...)

	// Build text edit for the list
	editRange := protocol.Range{
		Start: protocol.Position{
			Line:      uint32(listPos.StartLine),
			Character: 0,
		},
		End: protocol.Position{
			Line:      uint32(listPos.EndLine),
			Character: 0,
		},
	}
	slog.Debug("getListConversionAction: edit range (clamped to selection)",
		"startLine", editRange.Start.Line,
		"startChar", editRange.Start.Character,
		"endLine", editRange.End.Line,
		"endChar", editRange.End.Character,
		"selectionStart", selectionRange.Start.Line,
		"selectionEnd", selectionRange.End.Line)

	return protocol.CodeAction{
		Title:       "Convert list to headings",
		Kind:        kindRefactor,
		Diagnostics: nil,
		Edit: &protocol.WorkspaceEdit{
			Changes: map[protocol.DocumentURI][]protocol.TextEdit{
				uri: {
					{
						Range:   editRange,
						NewText: newText,
					},
				},
			},
		},
	}
}

// listConfig holds the variations between ordered and unordered list conversion.
type listConfig struct {
	Kind   org.ListKind
	Bullet func(index, depth int) string
	Depth  int
}

// headingSubtreeToList is the shared implementation for converting headings to lists.
func headingSubtreeToList(nodes []org.Node, listPos org.Position, cfg listConfig) []org.Node {
	var newNodes []org.Node

	// List items have to be parented to a common list node, but we don't want
	// to have to have a unifying parent node to convert a list of headings into
	// a list, because we want to be able to convert subsets of headings, so we
	// need to keep a running slice of the list items we've created, so we can
	// finally output them all at once as part of a common list node.
	var currentListNodes []org.Node
	for _, node := range nodes {
		if headline, ok := node.(org.Headline); ok {
			// We want a newline between the former title and its children in
			// the list item, to mirror what it looked like before.
			title := headline.Title
			if len(headline.Children) > 0 {
				title = append(title, org.Text{Content: "\n"})
			}

			var children []org.Node
			if len(headline.Children) == 0 {
				// If there are no children, we just need another newline to make things look nice
				children = []org.Node{org.Text{Content: "\n"}}
			} else {
				// If this heading has children, we want to convert the whole
				// heading tree, and it might have headings in its children, so
				// we have to recur and convert that subtreen
				childCfg := cfg
				childCfg.Depth = cfg.Depth + 1
				children = headingSubtreeToList(headline.Children, org.Position{}, childCfg)
			}

			// Add the heading to the currently ongoing list
			currentListNodes = append(currentListNodes, org.ListItem{
				Bullet: cfg.Bullet(len(currentListNodes), cfg.Depth),
				Children: append(
					title,
					children...,
				),
				Pos: headline.Pos,
			})
		} else {
			// We don't want random whitespace breaking up an otherwise consecutive list-to-be
			text, ok := node.(org.Text)
			isWhitespaceNode := ok && len(strings.TrimSpace(text.Content)) == 0

			// If we've hit a node that interrupts the flow of headings (usually
			// another heading), then we want to output the list we've been
			// working on, because any further headings will be part of a new
			// list
			if len(currentListNodes) > 0 && !isWhitespaceNode {
				newNodes = append(newNodes, org.List{
					Kind:  cfg.Kind,
					Items: currentListNodes,
					Pos:   listPos,
				})
				currentListNodes = []org.Node{}
			}

			// Then of course add that node, so we don't lose it
			newNodes = append(newNodes, node)
		}
	}

	// If we've got a final list waiting to be output we want to make sure it
	// gets output, even if there are no outside-heading nodes afterward
	if len(currentListNodes) > 0 {
		newNodes = append(newNodes, org.List{
			Kind:  cfg.Kind,
			Items: currentListNodes,
			Pos:   listPos,
		})
	}

	return newNodes
}

// headingSubtreeToOrderedList converts a heading and all its children to ordered list.
func headingSubtreeToOrderedList(nodes []org.Node, listPos org.Position) []org.Node {
	return headingSubtreeToList(nodes, listPos, listConfig{
		Kind: org.OrderedList,
		Bullet: func(index, depth int) string {
			return fmt.Sprintf("%d.", index+1)
		},
		Depth: 0,
	})
}

// headingSubtreeToUnorderedList converts a heading and all its children to bullet list.
func headingSubtreeToUnorderedList(nodes []org.Node, listPos org.Position, depth int) []org.Node {
	bullets := []string{"-", "+", "*"}
	return headingSubtreeToList(nodes, listPos, listConfig{
		Kind: org.UnorderedList,
		Bullet: func(index, depth int) string {
			return bullets[min(2, depth)]
		},
		Depth: depth,
	})
}

// listToHeadingSubtree converts a list to headings at the specified level.
// The first line of each list item becomes the headline title, remaining content becomes children.
func listToHeadingSubtree(list org.List, startLevel int) []org.Node {
	slog.Debug("listToHeadingSubtree: starting conversion", "listKind", list.Kind, "itemCount", len(list.Items), "startLevel", startLevel)
	var headings []org.Node

	for i, item := range list.Items {
		slog.Debug("listToHeadingSubtree: processing list item", "index", i)
		// Split item children into title (first line) and body (rest)
		var title []org.Node
		var children []org.Node
		foundFirstText := false

		item.Range(func(child org.Node) bool {
			// If we haven't found title yet, check for Paragraph or direct Text
			if !foundFirstText {
				// Check if child is a Paragraph containing text
				if para, ok := child.(org.Paragraph); ok {
					slog.Debug("listToHeadingSubtree: found paragraph, looking for text inside")
					// Look for Text nodes inside the paragraph
					var paraChildren []org.Node
					para.Range(func(paraChild org.Node) bool {
						if text, ok := paraChild.(org.Text); ok && !foundFirstText {
							foundFirstText = true
							content := strings.TrimSpace(text.Content)
							slog.Debug("listToHeadingSubtree: found first text in paragraph", "content", content)

							// Split on first newline to separate title from body
							if before, after, ok0 := strings.Cut(content, "\n"); ok0 {
								titleText := strings.TrimSpace(before)
								title = []org.Node{org.Text{Content: titleText, Pos: text.Pos}}
								slog.Debug("listToHeadingSubtree: split title at newline", "title", titleText)

								remaining := strings.TrimSpace(after)
								if remaining != "" {
									children = append(children, org.Text{Content: remaining + "\n"})
								}
							} else {
								// No newline - entire text is the title
								title = []org.Node{org.Text{Content: strings.TrimSpace(content), Pos: text.Pos}}
								slog.Debug("listToHeadingSubtree: using entire text as title", "title", content)
							}
						} else {
							// Not the first text, add to paragraph children
							paraChildren = append(paraChildren, paraChild)
						}
						return true
					})
					// If paragraph had other content after title, add it back as a paragraph
					if len(paraChildren) > 0 {
						children = append(children, org.Paragraph{Children: paraChildren, Pos: para.Pos})
					}
					return true
				}

				// Direct Text node (less common but handle it)
				if text, ok := child.(org.Text); ok {
					foundFirstText = true
					content := strings.TrimSpace(text.Content)
					slog.Debug("listToHeadingSubtree: found direct text node", "content", content)

					if before, after, ok0 := strings.Cut(content, "\n"); ok0 {
						title = []org.Node{org.Text{Content: strings.TrimSpace(before), Pos: text.Pos}}
						remaining := strings.TrimSpace(after)
						if remaining != "" {
							children = append(children, org.Text{Content: remaining + "\n"})
						}
						slog.Debug("listToHeadingSubtree: split title at newline", "title", before)
					} else {
						title = []org.Node{org.Text{Content: strings.TrimSpace(content), Pos: text.Pos}}
						slog.Debug("listToHeadingSubtree: using entire text as title", "title", content)
					}
					return true
				}
			}

			// Recursively convert nested lists
			if nestedList, ok := child.(org.List); ok {
				slog.Debug("listToHeadingSubtree: found nested list, recursing", "nestedLevel", startLevel+1)
				nestedHeadings := listToHeadingSubtree(nestedList, startLevel+1)
				children = append(children, nestedHeadings...)
			} else if !foundFirstText {
				// Before finding title, everything goes to children (shouldn't happen often)
				children = append(children, child)
			} else {
				// After finding title, add remaining content
				children = append(children, child)
			}
			return true
		})

		// Create headline from this list item
		listItem := item.(org.ListItem)
		slog.Debug("listToHeadingSubtree: creating headline", "level", startLevel, "titleLen", len(title), "childrenLen", len(children))
		headings = append(headings, org.Headline{
			Lvl:      startLevel,
			Title:    title,
			Children: children,
			Pos:      listItem.Pos,
		})
	}
	slog.Debug("listToHeadingSubtree: completed conversion", "headingsCreated", len(headings))
	return headings
}

// getCodeBlockAction returns action to evaluate a code block.
func getCodeBlockAction(block org.Block, uri protocol.DocumentURI) protocol.CodeAction {
	lang := "unknown"
	if len(block.Parameters) > 0 {
		lang = block.Parameters[0]
	}
	title := fmt.Sprintf("Evaluate %s code block", lang)

	kindQuickFix := protocol.QuickFix

	return protocol.CodeAction{
		Title:       title,
		Kind:        kindQuickFix,
		Diagnostics: nil,
		Command: &protocol.Command{
			Title:     title,
			Command:   "org.executeCodeBlock",
			Arguments: []any{string(uri), block.Pos.StartLine, block.Pos.StartColumn},
		},
	}
}

// ExecuteCodeBlock executes the code in a src block and returns the result.
// This is called via workspace/executeCommand.
func ExecuteCodeBlock(uri protocol.DocumentURI, line, column int) (string, error) {
	slog.Debug("Executing code block", "uri", uri, "line", line, "column", column)

	if serverState == nil {
		slog.Debug("Server state nil", "error", "server state not initialized")
		return "", fmt.Errorf("server state not initialized")
	}
	serverState.Mu.RLock()
	defer serverState.Mu.RUnlock()

	doc, ok := serverState.OpenDocs[uri]
	if !ok {
		slog.Debug("Document not found", "uri", uri)
		return "", fmt.Errorf("document not found")
	}

	// Find the block at the given position
	pos := protocol.Position{Line: uint32(line), Character: uint32(column)}
	block, found := findNodeAtPosition[org.Block](doc, pos)
	if !found || !strings.EqualFold(block.Name, "src") {
		slog.Debug("Block not found or not src", "found", found, "blockName", block.Name)
		return "", fmt.Errorf("no src block found at position")
	}

	lang := "unknown"
	if len(block.Parameters) > 0 {
		lang = block.Parameters[0]
	}
	slog.Debug("Block details", "lang", lang, "pos", block.Pos, "parameters", block.Parameters)

	// Extract code from block children
	code := org.String(block.Children...)
	if code == "" {
		slog.Debug("No code content in block", "children", len(block.Children))
		return "", fmt.Errorf("no code content found in block")
	}
	slog.Debug("Code extracted", "codeLen", len(code), "code", code)

	// Map language to executable
	var cmd *exec.Cmd
	switch lang {
	case "python", "python3":
		cmd = exec.Command("python3", "-c", code)
	case "bash", "sh", "shell":
		cmd = exec.Command("bash", "-c", code)
	case "js", "javascript":
		cmd = exec.Command("node", "-e", code)
	case "ruby":
		cmd = exec.Command("ruby", "-e", code)
	default:
		slog.Debug("Language not supported", "lang", lang, "code", code)
		return "", fmt.Errorf("unsupported language: %s", lang)
	}
	slog.Debug("Command created", "command", cmd, "cmdName", cmd.Args[0])

	// Execute and capture output
	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.Error("Code execution failed", "error", err, "exitCode", cmd.ProcessState.ExitCode(), "output", string(output))
		return fmt.Sprintf("Error: %v\nOutput: %s", err, string(output)), nil
	}

	slog.Debug("Code execution successful", "outputLen", len(output))
	return string(output), nil
}
