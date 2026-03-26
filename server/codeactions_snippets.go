package server

import (
	"regexp"
	"strings"
	"time"

	"github.com/alexispurslane/go-org/org"
	protocol "go.lsp.dev/protocol"
)

// getSnippetCodeActions returns all applicable snippet-based code actions
// for the given headline at the cursor position.
func getSnippetCodeActions(headline org.Headline, uri protocol.DocumentURI, doc *org.Document, cursorPos protocol.Position) []protocol.CodeAction {
	var actions []protocol.CodeAction

	// 1. Add DEADLINE (only if headline doesn't have DEADLINE)
	if !hasDeadline(headline) {
		date, day := getCurrentDate()
		snippet := "\n  DEADLINE: <${1:" + date + "} ${2:" + day + "}${3: 09:00}>"
		editRange := findInsertionPoint(headline, doc, true)
		actions = append(actions, createSnippetAction(
			"Org: Add DEADLINE",
			protocol.CodeActionKind("quickfix"),
			uri,
			editRange,
			snippet,
		))
	}

	// 2. Add SCHEDULED (only if headline doesn't have SCHEDULED)
	if !hasScheduled(headline) {
		date, day := getCurrentDate()
		snippet := "\n  SCHEDULED: <${1:" + date + "} ${2:" + day + "}${3: 09:00}>"
		editRange := findInsertionPoint(headline, doc, true)
		actions = append(actions, createSnippetAction(
			"Org: Add SCHEDULED",
			protocol.CodeActionKind("quickfix"),
			uri,
			editRange,
			snippet,
		))
	}

	// 3. Add Active Timestamp (always available - at cursor position)
	date, day := getCurrentDate()
	activeSnippet := "<${1:" + date + "} ${2:" + day + "}${3: 09:00}>"
	activeRange := protocol.Range{
		Start: cursorPos,
		End:   cursorPos,
	}
	actions = append(actions, createSnippetAction(
		"Org: Add active timestamp",
		protocol.CodeActionKind("source"),
		uri,
		activeRange,
		activeSnippet,
	))

	// 4. Add Inactive Timestamp (always available - at cursor position)
	inactiveSnippet := "[${1:" + date + "} ${2:" + day + "}${3: 09:00}]"
	inactiveRange := protocol.Range{
		Start: cursorPos,
		End:   cursorPos,
	}
	actions = append(actions, createSnippetAction(
		"Org: Add inactive timestamp",
		protocol.CodeActionKind("source"),
		uri,
		inactiveRange,
		inactiveSnippet,
	))

	// 5. Clock In (only if headline doesn't have incomplete CLOCK)
	if !hasClockIn(headline) {
		clockInSnippet := "\n  CLOCK: [${1:" + date + "} ${2:" + day + "} ${3:09:00}]"
		editRange := findInsertionPoint(headline, doc, true)
		actions = append(actions, createSnippetAction(
			"Org: Clock in",
			protocol.CodeActionKind("source"),
			uri,
			editRange,
			clockInSnippet,
		))
	}

	// 6. Clock Out (only if headline has incomplete CLOCK)
	if hasClockOut(headline) {
		clockOutSnippet := "--[${1:" + date + "} ${2:" + day + "} ${3:09:00}]"
		// For clock out, we append to the end of the existing clock line
		clockRange := findClockRange(headline)
		actions = append(actions, createSnippetAction(
			"Org: Clock out",
			protocol.CodeActionKind("source"),
			uri,
			clockRange,
			clockOutSnippet,
		))
	}

	// 7. Set Priority (only if headline doesn't have priority)
	if !hasPriority(headline) {
		prioritySnippet := "[#${1|A,B,C|}] "
		// Insert after TODO keyword or at the start of title
		editRange := findPriorityInsertionPoint(headline)
		actions = append(actions, createSnippetAction(
			"Org: Set priority",
			protocol.CodeActionKind("quickfix"),
			uri,
			editRange,
			prioritySnippet,
		))
	}

	// 8. Add Tags (always available)
	tagsSnippet := " :${1:tag}:${2::tag2}"
	editRange := findTagsInsertionPoint(headline)
	actions = append(actions, createSnippetAction(
		"Org: Add tags",
		protocol.CodeActionKind("source"),
		uri,
		editRange,
		tagsSnippet,
	))

	// FIXME: These assume that there is not already a pre-existing PROPERTIES drawer. That may not always be the case.
	// We should probably insert properties using the OpenDocument AST from server state, modifying it and then refreshing all the text in the file. Less performance-efficient, but far more reliable.
	// We might want to do the same for all of these, tbh, if the parser supports CLOCK, DEADLINE. SCHEDULE, etc. Or extend it to do so!

	// 9. Set Custom ID (only if headline doesn't have CUSTOM_ID)
	if !hasCustomID(headline) {
		// Generate a suggested ID from the headline title
		suggestedID := slugify(org.String(headline.Title...))
		editRange, drawerExists := findPropertyDrawerInsertionPoint(headline, doc)
		var customIDSnippet string
		if drawerExists {
			// Drawer exists, just add the property line before it
			customIDSnippet = ":CUSTOM_ID: ${1:" + suggestedID + "}\n"
		} else {
			// No drawer, wrap in full PROPERTIES drawer with leading newline
			customIDSnippet = "\n:PROPERTIES:\n:CUSTOM_ID: ${1:" + suggestedID + "}\n:END:"
		}
		actions = append(actions, createSnippetAction(
			"Org: Set CUSTOM_ID",
			protocol.CodeActionKind("source"),
			uri,
			editRange,
			customIDSnippet,
		))
	}

	// 10. Set Effort (only if headline doesn't have EFFORT)
	if !hasEffort(headline) {
		editRange, drawerExists := findPropertyDrawerInsertionPoint(headline, doc)
		var effortSnippet string
		if drawerExists {
			// Drawer exists, just add the property line before it
			effortSnippet = ":EFFORT: ${1|1:00,2:00,4:00,0:30|}\n"
		} else {
			// No drawer, wrap in full PROPERTIES drawer with leading newline
			effortSnippet = "\n:PROPERTIES:\n:EFFORT: ${1|1:00,2:00,4:00,0:30|}\n:END:"
		}
		actions = append(actions, createSnippetAction(
			"Org: Set effort",
			protocol.CodeActionKind("source"),
			uri,
			editRange,
			effortSnippet,
		))
	}

	// 11. Add Generic Property (always available)
	genericPropRange, drawerExists := findPropertyDrawerInsertionPoint(headline, doc)
	var genericPropSnippet string
	if drawerExists {
		// Drawer exists, just add the property line before it
		genericPropSnippet = ":${1:PROPERTY_NAME}: ${2:value}\n"
	} else {
		// No drawer, wrap in full PROPERTIES drawer with leading newline
		genericPropSnippet = "\n:PROPERTIES:\n:${1:PROPERTY_NAME}: ${2:value}\n:END:"
	}
	actions = append(actions, createSnippetAction(
		"Org: Add property",
		protocol.CodeActionKind("source"),
		uri,
		genericPropRange,
		genericPropSnippet,
	))

	// 12. Insert Link (always available - at cursor position)
	linkSnippet := "[[${1:url}][${2:description}]]$0"
	linkRange := protocol.Range{
		Start: cursorPos,
		End:   cursorPos,
	}
	actions = append(actions, createSnippetAction(
		"Org: Insert link",
		protocol.CodeActionKind("source"),
		uri,
		linkRange,
		linkSnippet,
	))

	// 13. Insert ID Link (always available - at cursor position)
	idLinkSnippet := "[[id:${1:id}]]$0"
	idLinkRange := protocol.Range{
		Start: cursorPos,
		End:   cursorPos,
	}
	actions = append(actions, createSnippetAction(
		"Org: Insert ID link",
		protocol.CodeActionKind("source"),
		uri,
		idLinkRange,
		idLinkSnippet,
	))

	return actions
}

// createSnippetAction creates a CodeAction with a snippet-formatted TextEdit
func createSnippetAction(title string, kind protocol.CodeActionKind, uri protocol.DocumentURI, editRange protocol.Range, snippetText string) protocol.CodeAction {
	return protocol.CodeAction{
		Title: title,
		Kind:  kind,
		Edit: &protocol.WorkspaceEdit{
			DocumentChanges: []protocol.TextDocumentEdit{
				{
					TextDocument: protocol.OptionalVersionedTextDocumentIdentifier{
						TextDocumentIdentifier: protocol.TextDocumentIdentifier{
							URI: uri,
						},
					},
					Edits: []any{
						protocol.SnippetTextEdit{
							Range: editRange,
							Snippet: protocol.StringValue{
								Kind:  "snippet",
								Value: snippetText,
							},
						},
					},
				},
			},
		},
	}
}

// createSnippetTextEdit creates a SnippetTextEdit for use in code actions
func createSnippetTextEdit(r protocol.Range, snippet string) protocol.SnippetTextEdit {
	return protocol.SnippetTextEdit{
		Range: r,
		Snippet: protocol.StringValue{
			Kind:  "snippet",
			Value: snippet,
		},
	}
}

// Helper functions to check for existing properties

// hasDeadline checks if the headline has a DEADLINE timestamp
func hasDeadline(headline org.Headline) bool {
	return findPlanningTimestamp(headline.Children, "DEADLINE") != nil
}

// hasScheduled checks if the headline has a SCHEDULED timestamp
func hasScheduled(headline org.Headline) bool {
	return findPlanningTimestamp(headline.Children, "SCHEDULED") != nil
}

// findPlanningTimestamp searches for a timestamp preceded by the given planning keyword
// Returns the timestamp if found, nil otherwise
func findPlanningTimestamp(nodes []org.Node, keyword string) *org.Timestamp {
	for i, node := range nodes {
		// Check if this is a Text node with the keyword
		if text, ok := node.(org.Text); ok && text.Content == keyword+": " {
			// Check if next node is a Timestamp
			if i+1 < len(nodes) {
				if ts, ok := nodes[i+1].(org.Timestamp); ok {
					return &ts
				}
			}
		}

		// Recursively check children of container nodes
		if paragraph, ok := node.(org.Paragraph); ok {
			if ts := findPlanningTimestamp(paragraph.Children, keyword); ts != nil {
				return ts
			}
		}
	}
	return nil
}

// hasPriority checks if the headline has a priority set
func hasPriority(headline org.Headline) bool {
	return headline.Priority != ""
}

// hasClockIn checks if the headline has an incomplete CLOCK entry
func hasClockIn(headline org.Headline) bool {
	for _, child := range headline.Children {
		if text, ok := child.(org.Text); ok {
			content := text.Content
			if strings.Contains(content, "CLOCK:") && !strings.Contains(content, "--[") {
				return true
			}
		}
	}
	return false
}

// hasClockOut checks if the headline has an incomplete CLOCK entry (no end time)
func hasClockOut(headline org.Headline) bool {
	// A clock is incomplete if it has CLOCK: but no closing time (--[)
	return hasClockIn(headline)
}

// hasCustomID checks if the headline has a CUSTOM_ID property
func hasCustomID(headline org.Headline) bool {
	return hasProperty(headline, "CUSTOM_ID")
}

// hasEffort checks if the headline has an EFFORT property
func hasEffort(headline org.Headline) bool {
	return hasProperty(headline, "EFFORT")
}

// hasProperty checks if the headline has a specific property in its PropertyDrawer
func hasProperty(headline org.Headline, propName string) bool {
	if headline.Properties == nil {
		return false
	}
	for _, prop := range headline.Properties.Properties {
		if len(prop) >= 1 && strings.EqualFold(prop[0], propName) {
			return true
		}
	}
	return false
}

// getPropertyValue returns the value of a property if it exists
func getPropertyValue(headline org.Headline, propName string) string {
	if headline.Properties == nil {
		return ""
	}
	for _, prop := range headline.Properties.Properties {
		if len(prop) >= 2 && strings.EqualFold(prop[0], propName) {
			return prop[1]
		}
	}
	return ""
}

// getCurrentDate returns the current date in YYYY-MM-DD format and abbreviated day name
func getCurrentDate() (date, day string) {
	now := time.Now()
	date = now.Format("2006-01-02")

	// Map weekday to abbreviated name
	days := []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
	day = days[now.Weekday()]

	return date, day
}

// slugify converts text to a slug-friendly format for use as CUSTOM_ID
func slugify(text string) string {
	// Convert to lowercase
	text = strings.ToLower(text)

	// Replace common characters with spaces/hyphens
	text = strings.ReplaceAll(text, "_", "-")
	text = strings.ReplaceAll(text, " ", "-")

	// Remove characters that aren't alphanumeric or hyphens
	reg := regexp.MustCompile(`[^a-z0-9-]`)
	text = reg.ReplaceAllString(text, "")

	// Remove multiple consecutive hyphens
	reg = regexp.MustCompile(`-+`)
	text = reg.ReplaceAllString(text, "-")

	// Trim leading/trailing hyphens
	text = strings.Trim(text, "-")

	return text
}

// findInsertionPoint finds the position to insert new content after the headline title
// Calculates the correct position based on the end of the title (or first child if no title)
// Always inserts at the end of the headline line (on the same line as the title)
// The snippet should include a leading newline to move to the next line
func findInsertionPoint(headline org.Headline, doc *org.Document, afterTitle bool) protocol.Range {
	headlinePos := headline.Position()

	// Find the end position of the title
	// Title is []Node, so we need to find the last node's position
	var insertCol int
	if len(headline.Title) > 0 {
		// Get position of the last node in Title
		lastTitleNode := headline.Title[len(headline.Title)-1]
		titlePos := lastTitleNode.Position()
		insertCol = titlePos.EndColumn
	} else if len(headline.Children) > 0 {
		// No title, insert at beginning of first child's line (new line)
		firstChild := headline.Children[0]
		childPos := firstChild.Position()
		return protocol.Range{
			Start: protocol.Position{
				Line:      uint32(childPos.StartLine),
				Character: 0,
			},
			End: protocol.Position{
				Line:      uint32(childPos.StartLine),
				Character: 0,
			},
		}
	} else {
		// No title and no children, use headline end position
		insertCol = headlinePos.EndColumn
	}

	// Always insert at the calculated position on the headline line
	// The snippet includes a leading "\n  " to move to a new indented line
	return protocol.Range{
		Start: protocol.Position{
			Line:      uint32(headlinePos.StartLine),
			Character: uint32(insertCol),
		},
		End: protocol.Position{
			Line:      uint32(headlinePos.StartLine),
			Character: uint32(insertCol),
		},
	}
}

// findPropertyDrawerRange finds the range of an existing PROPERTIES drawer
func findPropertyDrawerRange(headline org.Headline) (start, end int, exists bool) {
	if headline.Properties == nil {
		return 0, 0, false
	}
	pos := headline.Properties.Position()
	return pos.StartLine, pos.EndLine, true
}

// findPropertyDrawerInsertionPoint finds where to insert a property drawer
// If a drawer exists, it inserts inside the drawer (after :PROPERTIES: line).
// Otherwise, it inserts after the headline with full drawer wrapper.
// Returns the range and whether a drawer already exists.
func findPropertyDrawerInsertionPoint(headline org.Headline, doc *org.Document) (protocol.Range, bool) {
	if start, _, exists := findPropertyDrawerRange(headline); exists {
		// Insert inside the existing drawer (after :PROPERTIES: line)
		// The snippet should be just the property line, not wrapped in :PROPERTIES:...:END:
		return protocol.Range{
			Start: protocol.Position{
				Line:      uint32(start + 1),
				Character: 0,
			},
			End: protocol.Position{
				Line:      uint32(start + 1),
				Character: 0,
			},
		}, true
	}

	// No drawer exists, insert after the headline title
	return findInsertionPoint(headline, doc, true), false
}

// findPriorityInsertionPoint finds where to insert priority (after TODO or at start of title)
func findPriorityInsertionPoint(headline org.Headline) protocol.Range {
	pos := headline.Position()

	// Calculate insertion position: start column + stars + space + TODO + space
	// This is where priority should be inserted (after TODO keyword if present)
	titleStartCol := pos.StartColumn + headline.Lvl

	if headline.Status != "" {
		titleStartCol += len(headline.Status) + 2 // +2 for space after TODO and space before priority
	} else {
		titleStartCol += 1 // +1 for space after asterisk
	}

	return protocol.Range{
		Start: protocol.Position{
			Line:      uint32(pos.StartLine),
			Character: uint32(titleStartCol),
		},
		End: protocol.Position{
			Line:      uint32(pos.StartLine),
			Character: uint32(titleStartCol),
		},
	}
}

// findTagsInsertionPoint finds where to insert tags (at the end of the headline)
func findTagsInsertionPoint(headline org.Headline) protocol.Range {
	pos := headline.Position()

	// Insert at end of headline line
	// Note: pos.EndColumn should include any existing tags
	return protocol.Range{
		Start: protocol.Position{
			Line:      uint32(pos.StartLine),
			Character: uint32(pos.EndColumn),
		},
		End: protocol.Position{
			Line:      uint32(pos.StartLine),
			Character: uint32(pos.EndColumn),
		},
	}
}

// findClockRange finds the range of an existing incomplete CLOCK entry
func findClockRange(headline org.Headline) protocol.Range {
	for _, child := range headline.Children {
		if text, ok := child.(org.Text); ok {
			content := text.Content
			if strings.Contains(content, "CLOCK:") && !strings.Contains(content, "--[") {
				// Found incomplete clock, return its end position
				clockPos := text.Position()
				return protocol.Range{
					Start: protocol.Position{
						Line:      uint32(clockPos.EndLine),
						Character: uint32(clockPos.EndColumn),
					},
					End: protocol.Position{
						Line:      uint32(clockPos.EndLine),
						Character: uint32(clockPos.EndColumn),
					},
				}
			}
		}
	}

	// Fallback to headline position
	pos := headline.Position()
	return protocol.Range{
		Start: protocol.Position{
			Line:      uint32(pos.EndLine),
			Character: uint32(pos.EndColumn),
		},
		End: protocol.Position{
			Line:      uint32(pos.EndLine),
			Character: uint32(pos.EndColumn),
		},
	}
}
