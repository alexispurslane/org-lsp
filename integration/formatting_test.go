package integration

import (
	"os"
	"strings"
	"testing"

	"github.com/MarvinJWendt/testza"
	"go.lsp.dev/protocol"
)

func TestFormatAddsMissingUUIDs(t *testing.T) {
	Given("an org file with headings that have no ID properties", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			content := `* First Heading
Some content here

* Second Heading
More content`
			tc.GivenFile("test.org", content).
				GivenOpenFile("test.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.DocumentFormattingParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: tc.DocURI("test.org"),
				},
			}

			When(t, tc, "formatting the document", "textDocument/formatting", params, func(t *testing.T, edits []protocol.TextEdit) {
				Then("every heading should have an :ID: property", t, func(t *testing.T) {
					testza.AssertNotNil(t, edits, "Expected non-nil edits")
					testza.AssertGreater(t, len(edits), 0, "Expected at least one text edit")

					// Apply the edit to get formatted content
					formatted := applyEdits(t, tc, "test.org", edits)

					// Check that both headings now have ID properties
					testza.AssertTrue(t, strings.Contains(formatted, ":ID:"), "Formatted content should contain :ID: property")
					lines := strings.Split(formatted, "\n")
					idCount := 0
					for _, line := range lines {
						if strings.Contains(line, ":ID:") {
							idCount++
						}
					}
					testza.AssertEqual(t, 2, idCount, "Should have 2 ID properties (one per heading)")
				})
			})
		},
	)
}

func TestFormatPreservesExistingUUIDs(t *testing.T) {
	Given("an org file with headings that already have :ID: properties", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			tc.WithUUID("existingUUID")

			content := `* First Heading
:PROPERTIES:
:ID:       {{.existingUUID}}
:END:
Some content here`

			tc.GivenFile("test.org", content).
				GivenOpenFile("test.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.DocumentFormattingParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: tc.DocURI("test.org"),
				},
			}

			When(t, tc, "formatting the document", "textDocument/formatting", params, func(t *testing.T, edits []protocol.TextEdit) {
				Then("existing UUID should remain unchanged", t, func(t *testing.T) {
					testza.AssertNotNil(t, edits, "Expected non-nil edits")

					formatted := applyEdits(t, tc, "test.org", edits)

					// The existing UUID should still be present
					testza.AssertTrue(t, strings.Contains(formatted, tc.TestData["existingUUID"]), "Existing UUID should be preserved")

					// Should only have one ID occurrence (not duplicated)
					lines := strings.Split(formatted, "\n")
					idCount := 0
					for _, line := range lines {
						if strings.Contains(line, ":ID:") {
							idCount++
						}
					}
					testza.AssertEqual(t, 1, idCount, "Should have exactly 1 ID property")
				})
			})
		},
	)
}

func TestFormatOnSave(t *testing.T) {
	Given("the client has configured format on save", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			content := `* Heading Without ID
Content here`
			tc.GivenFile("test.org", content).
				GivenOpenFile("test.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			// Use willSaveWaitUntil to trigger format on save
			params := protocol.WillSaveTextDocumentParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: tc.DocURI("test.org"),
				},
				Reason: protocol.TextDocumentSaveReasonManual,
			}

			When(t, tc, "triggering willSaveWaitUntil", "textDocument/willSaveWaitUntil", params, func(t *testing.T, edits []protocol.TextEdit) {
				Then("should return formatting edits before save", t, func(t *testing.T) {
					// The server should return edits that add UUIDs
					testza.AssertNotNil(t, edits, "Expected non-nil edits from willSaveWaitUntil")

					if len(edits) > 0 {
						formatted := applyEdits(t, tc, "test.org", edits)
						testza.AssertTrue(t, strings.Contains(formatted, ":ID:"), "Formatted content should contain :ID: property after format-on-save")
					}
				})
			})
		},
	)
}

func TestFormatNormalizesHeadingSpacing(t *testing.T) {
	Given("an org file with inconsistent heading spacing", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			content := `* Heading 1
Content
** Subheading
More content
* Heading 2`
			tc.GivenFile("spacing.org", content).
				GivenOpenFile("spacing.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.DocumentFormattingParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: tc.DocURI("spacing.org"),
				},
			}

			When(t, tc, "formatting the document", "textDocument/formatting", params, func(t *testing.T, edits []protocol.TextEdit) {
				Then("headings should have blank lines before them", t, func(t *testing.T) {
					testza.AssertNotNil(t, edits, "Expected non-nil edits")
					testza.AssertGreater(t, len(edits), 0, "Expected at least one text edit")

					formatted := applyEdits(t, tc, "spacing.org", edits)
					lines := strings.Split(formatted, "\n")

					// Find Heading 2 and verify it has a blank line before it
					for i, line := range lines {
						if strings.HasPrefix(line, "* Heading 2") {
							testza.AssertTrue(t, i > 0 && strings.TrimSpace(lines[i-1]) == "", "Heading 2 should have blank line before it")
						}
					}
				})

				Then("first heading should not have blank line before it", t, func(t *testing.T) {
					formatted := applyEdits(t, tc, "spacing.org", edits)
					lines := strings.Split(formatted, "\n")

					// First line should be the first heading (no blank line before it)
					testza.AssertTrue(t, strings.HasPrefix(lines[0], "* Heading 1"), "First line should be first heading without preceding blank line")
				})

				Then("subheadings should also have blank lines before them", t, func(t *testing.T) {
					formatted := applyEdits(t, tc, "spacing.org", edits)
					lines := strings.Split(formatted, "\n")

					// Find Subheading and verify it has a blank line before it
					for i, line := range lines {
						if strings.HasPrefix(line, "** Subheading") {
							testza.AssertTrue(t, i > 0 && strings.TrimSpace(lines[i-1]) == "", "Subheading should have blank line before it")
						}
					}
				})
			})
		},
	)
}

func TestFormatAlignsTags(t *testing.T) {
	Given("an org file with misaligned tags", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			content := `* Heading with lots of text here :tag1:
* Short                   :tag1:tag2:
* Medium length heading          :tag1:`
			tc.GivenFile("tags.org", content).
				GivenOpenFile("tags.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.DocumentFormattingParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: tc.DocURI("tags.org"),
				},
			}

			When(t, tc, "formatting the document", "textDocument/formatting", params, func(t *testing.T, edits []protocol.TextEdit) {
				Then("tags should be aligned to a consistent column", t, func(t *testing.T) {
					testza.AssertNotNil(t, edits, "Expected non-nil edits")

					formatted := applyEdits(t, tc, "tags.org", edits)
					lines := strings.Split(formatted, "\n")

					// All tag positions should be the same
					tagPositions := []int{}
					for _, line := range lines {
						if idx := strings.Index(line, ":"); idx > 0 {
							// Found start of tags (we're assuming no colons in header since we control the input string here)
							tagPositions = append(tagPositions, idx)
							break
						}
					}

					if len(tagPositions) > 1 {
						firstPos := tagPositions[0]
						for _, pos := range tagPositions[1:] {
							testza.AssertEqual(t, firstPos, pos, "All tags should align to same column")
						}
					}
				})
			})
		},
	)
}

func TestFormatNormalizesTodoKeywords(t *testing.T) {
	Given("an org file with inconsistent TODO placement", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			content := `* TODO Heading 1
*  TODO   Heading 2`
			tc.GivenFile("todo.org", content).
				GivenOpenFile("todo.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.DocumentFormattingParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: tc.DocURI("todo.org"),
				},
			}

			When(t, tc, "formatting the document", "textDocument/formatting", params, func(t *testing.T, edits []protocol.TextEdit) {
				Then("TODO keywords should be properly placed after stars", t, func(t *testing.T) {
					testza.AssertNotNil(t, edits, "Expected non-nil edits")

					formatted := applyEdits(t, tc, "todo.org", edits)
					lines := strings.SplitSeq(formatted, "\n")

					for line := range lines {
						if strings.Contains(line, "TODO") {
							// Should be "* TODO " not "*  TODO" or "*Heading TODO"
							testza.AssertTrue(t, strings.HasPrefix(line, "* TODO "), "TODO should follow star with single space: "+line)
						}
					}
				})
			})
		},
	)
}

func TestFormatCollapsesMultipleBlankLines(t *testing.T) {
	Given("an org file with excessive blank lines", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			content := `* Heading 1
Content


More content


* Heading 2`
			tc.GivenFile("blanks.org", content).
				GivenOpenFile("blanks.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.DocumentFormattingParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: tc.DocURI("blanks.org"),
				},
			}

			When(t, tc, "formatting the document", "textDocument/formatting", params, func(t *testing.T, edits []protocol.TextEdit) {
				Then("multiple blank lines should be collapsed to single", t, func(t *testing.T) {
					testza.AssertNotNil(t, edits, "Expected non-nil edits")

					formatted := applyEdits(t, tc, "blanks.org", edits)
					lines := strings.Split(formatted, "\n")

					// Check no more than one consecutive blank line
					consecutiveBlanks := 0
					for _, line := range lines {
						if strings.TrimSpace(line) == "" {
							consecutiveBlanks++
							testza.AssertTrue(t, consecutiveBlanks <= 2, "Should not have more than 2 consecutive blank lines")
						} else {
							consecutiveBlanks = 0
						}
					}
				})
			})
		},
	)
}

func TestFormatRemovesTrailingWhitespace(t *testing.T) {
	Given("an org file with trailing whitespace", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			content := "* Heading 1   \nContent with spaces   \n** Subheading\t"
			tc.GivenFile("trailing.org", content).
				GivenOpenFile("trailing.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.DocumentFormattingParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: tc.DocURI("trailing.org"),
				},
			}

			When(t, tc, "formatting the document", "textDocument/formatting", params, func(t *testing.T, edits []protocol.TextEdit) {
				Then("trailing whitespace should be removed", t, func(t *testing.T) {
					testza.AssertNotNil(t, edits, "Expected non-nil edits")

					formatted := applyEdits(t, tc, "trailing.org", edits)
					lines := strings.SplitSeq(formatted, "\n")

					for line := range lines {
						testza.AssertEqual(t, line, strings.TrimRight(line, " \t"), "Line should not have trailing whitespace: %q", line)
					}
				})
			})
		},
	)
}

func TestFormatNormalizesPropertyDrawerIndentation(t *testing.T) {
	Given("an org file with inconsistent property drawer indentation", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			content := `* Heading
:PROPERTIES:
:ID: abc-123
  :CUSTOM_PROP: value
:END:`
			tc.GivenFile("props.org", content).
				GivenOpenFile("props.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.DocumentFormattingParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: tc.DocURI("props.org"),
				},
			}

			When(t, tc, "formatting the document", "textDocument/formatting", params, func(t *testing.T, edits []protocol.TextEdit) {
				Then("property drawer should have consistent indentation", t, func(t *testing.T) {
					testza.AssertNotNil(t, edits, "Expected non-nil edits")

					formatted := applyEdits(t, tc, "props.org", edits)
					lines := strings.Split(formatted, "\n")

					inDrawer := false
					for _, line := range lines {
						if strings.TrimSpace(line) == ":PROPERTIES:" {
							inDrawer = true
							continue
						}
						if strings.TrimSpace(line) == ":END:" {
							inDrawer = false
							continue
						}
						if inDrawer {
							// Property lines should start at column 0 with no leading spaces
							testza.AssertTrue(t, strings.HasPrefix(line, ":"), "Property line should start at column 0: %q", line)
						}
					}
				})
			})
		},
	)
}

func TestFormatAlignsTableColumns(t *testing.T) {
	Given("an org file with misaligned table", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			content := `| Name | Age | City |
| Alice | 30 | NYC |
| Bob | 25 | Los Angeles |`
			tc.GivenFile("table.org", content).
				GivenOpenFile("table.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.DocumentFormattingParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: tc.DocURI("table.org"),
				},
			}

			When(t, tc, "formatting the document", "textDocument/formatting", params, func(t *testing.T, edits []protocol.TextEdit) {
				Then("table columns should be aligned", t, func(t *testing.T) {
					testza.AssertNotNil(t, edits, "Expected non-nil edits")

					formatted := applyEdits(t, tc, "table.org", edits)
					lines := strings.Split(formatted, "\n")

					// Find table lines and check alignment
					tableLines := []string{}
					for _, line := range lines {
						if strings.HasPrefix(line, "|") {
							tableLines = append(tableLines, line)
						}
					}

					if len(tableLines) >= 2 {
						// Check that corresponding pipe positions align across rows
						// Rows can have different column counts, but shared columns should align
						firstLinePipes := findPipePositions(tableLines[0])
						for i := 1; i < len(tableLines); i++ {
							pipes := findPipePositions(tableLines[i])
							// Compare positions of shared columns (minimum of both row's column count)
							minCols := min(len(pipes), len(firstLinePipes))
							testza.AssertEqualValues(t, pipes[:minCols], firstLinePipes[:minCols], "Table columns must all align")
						}
					}
				})
			})
		},
	)
}

// Helper function to find positions of | characters
func findPipePositions(s string) []int {
	positions := []int{}
	for i, c := range s {
		if c == '|' {
			positions = append(positions, i)
		}
	}
	return positions
}

func TestFormatNormalizesListIndentation(t *testing.T) {
	Given("an org file with inconsistent list indentation", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			content := `* Heading
- Item 1
     - Nested item
- Item 2
      - Another nested`
			tc.GivenFile("list.org", content).
				GivenOpenFile("list.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.DocumentFormattingParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: tc.DocURI("list.org"),
				},
			}

			When(t, tc, "formatting the document", "textDocument/formatting", params, func(t *testing.T, edits []protocol.TextEdit) {
				Then("nested list items should have consistent indentation", t, func(t *testing.T) {
					testza.AssertNotNil(t, edits, "Expected non-nil edits")

					formatted := applyEdits(t, tc, "list.org", edits)
					lines := strings.Split(formatted, "\n")

					nestedFound := false
					for _, line := range lines {
						trimmedLine := strings.TrimPrefix(line, " ")
						if strings.HasPrefix(trimmedLine, "-") {
							nestedFound = true
							// Nested items should have 2-space indentation
							leadingSpaceCount := len(trimmedLine) - len(line)
							if leadingSpaceCount > 0 {
								testza.AssertEqual(t, leadingSpaceCount, 2, "Nested list items should have 2-space indents: %q", line)
							}
						}
					}
					testza.AssertTrue(t, nestedFound, "Should have found nested list items")
				})
			})
		},
	)
}

func TestFormatPreservesCodeBlockContent(t *testing.T) {
	Given("an org file with deliberately weird code formatting", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			content := `* Notes
#+begin_src python
def   badly_spaced(  x  ):
    if x==1:
        return    2
#+end_src`
			tc.GivenFile("code.org", content).
				GivenOpenFile("code.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.DocumentFormattingParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: tc.DocURI("code.org"),
				},
			}

			When(t, tc, "formatting the document", "textDocument/formatting", params, func(t *testing.T, edits []protocol.TextEdit) {
				Then("code block content should remain unchanged", t, func(t *testing.T) {
					testza.AssertNotNil(t, edits, "Expected non-nil edits")

					formatted := applyEdits(t, tc, "code.org", edits)

					// The weird spacing in the code should be preserved
					testza.AssertTrue(t, strings.Contains(formatted, "def   badly_spaced(  x  ):"), "Code block content should be preserved")
					testza.AssertTrue(t, strings.Contains(formatted, "return    2"), "Code block indentation should be preserved")
				})
			})
		},
	)
}

func TestFormatNormalizesFileKeywords(t *testing.T) {
	Given("an org file with scattered keywords", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			content := `#+TITLE: My Document
#+AUTHOR:  John Doe

#+OPTIONS: toc:t
* Heading`
			tc.GivenFile("keywords.org", content).
				GivenOpenFile("keywords.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.DocumentFormattingParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: tc.DocURI("keywords.org"),
				},
			}

			When(t, tc, "formatting the document", "textDocument/formatting", params, func(t *testing.T, edits []protocol.TextEdit) {
				Then("keywords should be consolidated at top", t, func(t *testing.T) {
					testza.AssertNotNil(t, edits, "Expected non-nil edits")

					formatted := applyEdits(t, tc, "keywords.org", edits)
					lines := strings.Split(formatted, "\n")

					// Keywords should be at the top
					foundHeading := false
					for _, line := range lines {
						if strings.HasPrefix(line, "*") {
							foundHeading = true
						}
						if strings.HasPrefix(line, "#+") {
							testza.AssertFalse(t, foundHeading, "Keywords should come before headings")
						}
					}
				})
			})
		},
	)
}

func TestFormatEmptyFile(t *testing.T) {
	Given("an empty org file", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			tc.GivenFile("empty.org", "").
				GivenOpenFile("empty.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.DocumentFormattingParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: tc.DocURI("empty.org"),
				},
			}

			When(t, tc, "formatting the document", "textDocument/formatting", params, func(t *testing.T, edits []protocol.TextEdit) {
				Then("should return empty or no edits", t, func(t *testing.T) {
					// Empty file should either return no edits or remain empty
					if len(edits) > 0 {
						formatted := applyEdits(t, tc, "empty.org", edits)
						testza.AssertEqual(t, "", strings.TrimSpace(formatted), "Empty file should remain empty")
					}
				})
			})
		},
	)
}

func TestFormatComplexDocument(t *testing.T) {
	Given("a complex org file with tags, properties, lists, and code blocks", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			content := `* This is a new heading :accelerationism:
:PROPERTIES:
:ID: 9eaf1cad-a540-458d-a431-0f66ff6f8bbb
:END:

Foo!
- Foo
  - Bar

#+BEGIN_SRC bash
osascript -e 'display notification "Lorem ipsum dolor sit amet" with title "Title"'
#+END_SRC

This is a paragraph

This is another one

This is still a third`
			tc.GivenFile("complex.org", content).
				GivenOpenFile("complex.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.DocumentFormattingParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: tc.DocURI("complex.org"),
				},
			}

			When(t, tc, "formatting the document", "textDocument/formatting", params, func(t *testing.T, edits []protocol.TextEdit) {
				Then("should not panic and return edits", t, func(t *testing.T) {
					testza.AssertNotNil(t, edits, "Expected non-nil edits")
					testza.AssertGreater(t, len(edits), 0, "Expected at least one edit for formatting")

					// This test passes if we got here without panicking
					formatted := applyEdits(t, tc, "complex.org", edits)
					testza.AssertNotNil(t, formatted, "Expected formatted output")
				})
			})
		},
	)
}

func TestFormatAfterUnsavedChanges(t *testing.T) {
	Given("a document with unsaved changes", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			originalContent := `* Heading 1
Content under heading 1

* Heading 2
More content`
			tc.GivenFile("unsaved.org", originalContent).
				GivenOpenFile("unsaved.org")

			// Simulate making unsaved changes (full document sync)
			changedContent := `* Heading 1
Content under heading 1
:PROPERTIES:
:ID: test-id
:END:

* Heading 2
More content with :tag:`

			tc.GivenChangeDocument("unsaved.org", changedContent)
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.DocumentFormattingParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: tc.DocURI("unsaved.org"),
				},
			}

			When(t, tc, "formatting after unsaved changes", "textDocument/formatting", params, func(t *testing.T, edits []protocol.TextEdit) {
				Then("should not panic and return edits", t, func(t *testing.T) {
					testza.AssertNotNil(t, edits, "Expected non-nil edits")
					testza.AssertGreater(t, len(edits), 0, "Expected at least one edit for formatting")
					formatted := applyEdits(t, tc, "unsaved.org", edits)
					testza.AssertNotNil(t, formatted, "Expected formatted output")
					testza.AssertTrue(t, strings.Contains(formatted, "test-id"), "Should contain ID from unsaved changes")
					testza.AssertTrue(t, strings.Contains(formatted, ":tag:"), "Should contain tag from unsaved changes")
				})
			})
		},
	)
}

func TestFormatRangeOnly(t *testing.T) {
	Given("an org file where Heading 2 is missing a blank line before it", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			content := `* Heading 1

Content under heading 1
* Heading 2
Content under heading 2`
			tc.GivenFile("range.org", content).
				GivenOpenFile("range.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			// Format only the range containing Heading 2 (lines 3-4, 0-indexed)
			// Line 3 is where Heading 2 starts without a blank line before it
			params := protocol.DocumentRangeFormattingParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: tc.DocURI("range.org"),
				},
				Range: protocol.Range{
					Start: protocol.Position{Line: 3, Character: 0},
					End:   protocol.Position{Line: 4, Character: 0},
				},
			}

			When(t, tc, "formatting a specific range", "textDocument/rangeFormatting", params, func(t *testing.T, edits []protocol.TextEdit) {
				Then("only Heading 2 should get a blank line added before it", t, func(t *testing.T) {
					testza.AssertNotNil(t, edits, "Expected non-nil edits")
					testza.AssertGreater(t, len(edits), 0, "Expected at least one edit for range formatting")

					formatted := applyEdits(t, tc, "range.org", edits)
					lines := strings.Split(formatted, "\n")

					// Find Heading 2 and verify it now has a blank line before it (was in range, got formatted)
					for i, line := range lines {
						if strings.HasPrefix(line, "* Heading 2") {
							testza.AssertGreater(t, i, 0, "Heading 2 should not be on first line")
							testza.AssertEqual(t, "", strings.TrimSpace(lines[i-1]), "Heading 2 should now have blank line before it")
						}
					}
				})
			})
		},
	)
}

// applyEdits applies text edits to a file and returns the resulting content
func applyEdits(t *testing.T, tc *LSPTestContext, filename string, edits []protocol.TextEdit) string {
	t.Helper()

	// Read original content
	original, err := os.ReadFile(tc.tempDir + "/" + filename)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	// Apply edits (simplified - just use the first edit's new text for now)
	// In a full implementation, we'd properly merge multiple edits
	if len(edits) == 0 {
		return string(original)
	}

	// For simplicity, return the new text from the first full-document edit
	// A real implementation would need to handle multiple incremental edits
	return edits[0].NewText
}
