package integration

import (
	"testing"

	"github.com/MarvinJWendt/testza"
	"go.lsp.dev/protocol"
)

func TestCodeActionHeadingToList(t *testing.T) {
	Given("a file with headings", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			content := `* First Heading
Content for first heading.

* Second Heading
Content for second heading.
** Nested Heading
Nested content.
`

			tc.GivenFile("headings.org", content).
				GivenOpenFile("headings.org")

			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.CodeActionParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: protocol.DocumentURI(tc.rootURI + "/headings.org"),
				},
				Range: protocol.Range{
					Start: protocol.Position{Line: 0, Character: 0},
					End:   protocol.Position{Line: 2, Character: 0},
				},
				Context: protocol.CodeActionContext{},
			}

			When(t, tc, "requesting code actions for heading range", "textDocument/codeAction", params, func(t *testing.T, result []protocol.CodeAction) {
				Then("returns heading to list conversion actions", t, func(t *testing.T) {
					testza.AssertGreaterOrEqual(t, len(result), 2, "Expected at least 2 code actions for headings")

					var foundOrderedList, foundBulletList bool
					for _, action := range result {
						if action.Title == "Convert headings to ordered list" {
							foundOrderedList = true
							testza.AssertEqual(t, protocol.RefactorRewrite, action.Kind)
							testza.AssertNotNil(t, action.Edit)
							testza.AssertNotNil(t, action.Edit.Changes)

							changes := action.Edit.Changes
							edit := changes[protocol.DocumentURI(tc.rootURI+"/headings.org")]
							testza.AssertGreaterOrEqual(t, len(edit), 1, "Expected at least one text edit")
							testza.AssertGreater(t, len(edit[0].NewText), 0, "Transformation should produce non-empty output")
							testza.AssertEqual(t, "1. First Heading\n   Content for first heading.\n\n", edit[0].NewText)
						}
						if action.Title == "Convert headings to bullet list" {
							foundBulletList = true
							testza.AssertEqual(t, protocol.RefactorRewrite, action.Kind)

							changes := action.Edit.Changes
							edit := changes[protocol.DocumentURI(tc.rootURI+"/headings.org")]
							testza.AssertGreaterOrEqual(t, len(edit), 1, "Expected at least one text edit")
							testza.AssertGreater(t, len(edit[0].NewText), 0, "Expected non-empty transformation output")
							testza.AssertEqual(t, "- First Heading\n  Content for first heading.\n\n", edit[0].NewText)
						}
					}

					testza.AssertTrue(t, foundOrderedList, "Expected 'Convert headings to ordered list' action")
					testza.AssertTrue(t, foundBulletList, "Expected 'Convert headings to bullet list' action")

					Then("edit range starts at selection position", t, func(t *testing.T) {
						for _, action := range result {
							if action.Title == "Convert headings to ordered list" {
								edit := action.Edit.Changes[protocol.DocumentURI(tc.rootURI+"/headings.org")][0]
								// Edit range should start at the selection start (0:0)
								testza.AssertEqual(t, uint32(0), edit.Range.Start.Line, "Edit should start at selection line")
								testza.AssertEqual(t, uint32(0), edit.Range.Start.Character, "Edit should start at selection character")
								// Edit range may extend longer than selection
								testza.AssertGreaterOrEqual(t, edit.Range.End.Line, uint32(2), "Edit end line should be at or after selection end")
							}
						}
					})
				})
			})

			// Second When with nonzero starting range
			nonzeroParams := protocol.CodeActionParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: protocol.DocumentURI(tc.rootURI + "/headings.org"),
				},
				Range: protocol.Range{
					Start: protocol.Position{Line: 2, Character: 0}, // Start at "* Second Heading"
					End:   protocol.Position{Line: 6, Character: 0},
				},
				Context: protocol.CodeActionContext{},
			}

			When(t, tc, "requesting code actions for nonzero range starting at line 2", "textDocument/codeAction", nonzeroParams, func(t *testing.T, result []protocol.CodeAction) {
				Then("returns conversion actions with edit starting at line 2", t, func(t *testing.T) {
					testza.AssertGreaterOrEqual(t, len(result), 1, "Expected at least 1 code action")

					var foundOrderedList bool
					for _, action := range result {
						if action.Title == "Convert headings to ordered list" {
							foundOrderedList = true
							edit := action.Edit.Changes[protocol.DocumentURI(tc.rootURI+"/headings.org")][0]

							// Verify edit starts at the nonzero selection position
							testza.AssertEqual(t, uint32(2), edit.Range.Start.Line, "Edit should start at selection line 2")
							testza.AssertEqual(t, uint32(0), edit.Range.Start.Character, "Edit should start at selection character 0")

							// Verify content is for the selected heading
							testza.AssertContains(t, edit.NewText, "Second Heading", "Content should include selected heading")
						}
					}

					testza.AssertTrue(t, foundOrderedList, "Expected 'Convert headings to ordered list' action")
				})
			})
		},
	)
}

func TestCodeActionListToHeading(t *testing.T) {
	Given("a file with a list", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			content := `- First list item
- Second list item
  - Nested item
`

			tc.GivenFile("list.org", content).
				GivenOpenFile("list.org")

			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.CodeActionParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: protocol.DocumentURI(tc.rootURI + "/list.org"),
				},
				Range: protocol.Range{
					Start: protocol.Position{Line: 2, Character: 5},
					End:   protocol.Position{Line: 2, Character: 5},
				},
				Context: protocol.CodeActionContext{},
			}

			When(t, tc, "requesting code actions for list item", "textDocument/codeAction", params, func(t *testing.T, result []protocol.CodeAction) {
				Then("returns list to heading conversion action", t, func(t *testing.T) {
					testza.AssertGreaterOrEqual(t, len(result), 1, "Expected at least 1 code action for list")

					// Find the list conversion action
					var foundListToHeading bool
					for _, action := range result {
						if action.Title == "Convert list to headings" {
							foundListToHeading = true
							testza.AssertEqual(t, protocol.RefactorRewrite, action.Kind)
							testza.AssertNotNil(t, action.Edit)

							// Verify the transformation produces correct output
							changes := action.Edit.Changes
							edit := changes[protocol.DocumentURI(tc.rootURI+"/list.org")]
							testza.AssertGreaterOrEqual(t, len(edit), 1, "Expected at least one text edit")
							testza.AssertGreater(t, len(edit[0].NewText), 0, "Expected non-empty transformation output")
							testza.AssertEqual(t, "* Nested item\n", edit[0].NewText)
						}
					}

					testza.AssertTrue(t, foundListToHeading, "Expected 'Convert list to headings' action")
				})
			})
		},
	)
}

func TestCodeActionCodeBlockEval(t *testing.T) {
	Given("a file with a code block", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			content := `* Test Heading

#+begin_src python
print("hello")
#+end_src
`

			tc.GivenFile("codeblock.org", content).
				GivenOpenFile("codeblock.org")

			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.CodeActionParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: protocol.DocumentURI(tc.rootURI + "/codeblock.org"),
				},
				Range: protocol.Range{
					Start: protocol.Position{Line: 2, Character: 10},
					End:   protocol.Position{Line: 2, Character: 10},
				},
				Context: protocol.CodeActionContext{},
			}

			When(t, tc, "requesting code actions for code block", "textDocument/codeAction", params, func(t *testing.T, result []protocol.CodeAction) {
				Then("returns code block evaluation action", t, func(t *testing.T) {
					testza.AssertGreaterOrEqual(t, len(result), 1, "Expected at least 1 code action for code block")

					// Find the code block evaluation action
					var foundEvalAction bool
					for _, action := range result {
						if action.Title == "Evaluate python code block" {
							foundEvalAction = true
							testza.AssertEqual(t, protocol.QuickFix, action.Kind)
							testza.AssertNotNil(t, action.Command)
							testza.AssertEqual(t, "org.executeCodeBlock", action.Command.Command)
						}
					}

					testza.AssertTrue(t, foundEvalAction, "Expected 'Evaluate python code block' action")
				})
			})
		},
	)
}
