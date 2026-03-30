package integration

import (
	"strings"
	"testing"

	"github.com/MarvinJWendt/testza"
	"go.lsp.dev/protocol"
)

func TestFormatAddsBlankLineAfterPropertyDrawer(t *testing.T) {
	Given("a heading with a property drawer and content following it", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			content := `* Heading One
:PROPERTIES:
:ID:       test-id-123
:END:
Content under heading 1

* Heading Two
:PROPERTIES:
:ID:       test-id-456
:END:
Content under heading 2

* Heading Three
Content without properties drawer`
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
				Then("there should be a blank line between :END: and the following content", t, func(t *testing.T) {
					testza.AssertNotNil(t, edits, "Expected non-nil edits")
					formatted := applyEdits(t, tc, "test.org", edits)

					// Debug: print formatted content
					t.Logf("=== FORMATTED CONTENT ===")
					t.Logf("%s", formatted)
					t.Logf("=== END FORMATTED CONTENT ===")
					t.Logf("")

					// Check for blank lines after each :END: that's followed by content
					lines := strings.Split(formatted, "\n")
					for i, line := range lines {
						if strings.Contains(line, ":END:") && i+1 < len(lines) {
							// The line after :END: should be blank
							testza.AssertEqual(t, "", lines[i+1],
								"Line %d after :END: should be blank, got: %q", i+1, lines[i+1])
						}
					}

					// Verify specific patterns in the formatted output
					testza.AssertTrue(t, strings.Contains(formatted, ":END:\n\nContent under"),
						"Should have blank line between :END: and following content")
				})
			})
		},
	)
}

func TestFormatBlankLineAfterExistingPropertyDrawer(t *testing.T) {
	Given("a document with existing properties drawers that may or may not have blank lines", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			tc.WithUUID("existing_id_1")
			tc.WithUUID("existing_id_2")

			content := `* Heading One
:PROPERTIES:
:ID:       {{.existing_id_1}}
:END:
Missing blank line here

* Heading Two
:PROPERTIES:
:ID:       {{.existing_id_2}}
:END:

Has blank line here`
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
				Then("blank lines are added after all property drawers", t, func(t *testing.T) {
					testza.AssertNotNil(t, edits, "Expected non-nil edits")
					formatted := applyEdits(t, tc, "test.org", edits)

					// Debug: print formatted content
					t.Logf("=== FORMATTED CONTENT ===")
					t.Logf("%s", formatted)
					t.Logf("=== END FORMATTED CONTENT ===")
					t.Logf("")

					// Verify both have blank lines after :END:
					lines := strings.Split(formatted, "\n")
					endIndices := []int{}
					for i, line := range lines {
						if strings.Contains(line, ":END:") {
							endIndices = append(endIndices, i)
						}
					}
					testza.AssertEqual(t, 2, len(endIndices), "Should find 2 :END: lines")

					for _, endIdx := range endIndices {
						testza.AssertGreater(t, len(lines), endIdx+1,
							"Should have a line after :END: at index %d", endIdx)
						testza.AssertEqual(t, "", lines[endIdx+1],
							"Line after :END: at index %d should be blank", endIdx+1)
					}

					// Verify the specific UUIDs are preserved
					testza.AssertTrue(t, strings.Contains(formatted, tc.TestData["existing_id_1"]),
						"First UUID should be preserved")
					testza.AssertTrue(t, strings.Contains(formatted, tc.TestData["existing_id_2"]),
						"Second UUID should be preserved")
				})
			})
		},
	)
}
