package integration

import (
	"testing"

	"github.com/MarvinJWendt/testza"
	"go.lsp.dev/protocol"
)

func TestSingleHeadingToBulletList(t *testing.T) {
	Given("a file with a single heading", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			tc.GivenFile("test.org", `* First heading
Some content under the heading
`)
			tc.GivenOpenFile("test.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			// Select the entire heading line
			params := protocol.CodeActionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: tc.DocURI("test.org")},
				Range: protocol.Range{
					Start: protocol.Position{Line: 0, Character: 0},
					End:   protocol.Position{Line: 0, Character: 15},
				},
			}

			When(t, tc, "requesting code actions", "textDocument/codeAction", params,
				func(t *testing.T, actions []protocol.CodeAction) {
					Then("should offer heading to list conversion", t, func(t *testing.T) {
						testza.AssertGreater(t, len(actions), 0, "Should have code actions available")

						// Find the bullet list action
						var found bool
						for _, action := range actions {
							if action.Title == "Org: Convert headings to bullet list" {
								found = true

								// Apply the edit
								edits := action.Edit.Changes[tc.DocURI("test.org")]
								testza.AssertLen(t, edits, 1, "Should have one text edit")

								// Check that the edit produces valid list output
								newText := edits[0].NewText
								testza.AssertContains(t, newText, "- First heading", "Should convert to bullet list item")
							}
						}
						testza.AssertTrue(t, found, "Should have bullet list conversion action")
					})
				})
		},
	)
}

func TestMultipleHeadingsToBulletList(t *testing.T) {
	Given("a file with multiple headings", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			tc.GivenFile("test.org", `* First heading
Some content

* Second heading
More content

* Third heading
Even more content
`)
			tc.GivenOpenFile("test.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			// Select all three headings
			params := protocol.CodeActionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: tc.DocURI("test.org")},
				Range: protocol.Range{
					Start: protocol.Position{Line: 0, Character: 0},
					End:   protocol.Position{Line: 6, Character: 20},
				},
			}

			When(t, tc, "requesting code actions", "textDocument/codeAction", params,
				func(t *testing.T, actions []protocol.CodeAction) {
					Then("should convert all headings to list items", t, func(t *testing.T) {
						for _, action := range actions {
							if action.Title == "Org: Convert headings to bullet list" {
								edits := action.Edit.Changes[tc.DocURI("test.org")]
								testza.AssertLen(t, edits, 1, "Should have one text edit")

								newText := edits[0].NewText
								testza.AssertContains(t, newText, "- First heading", "Should have first list item")
								testza.AssertContains(t, newText, "- Second heading", "Should have second list item")
								testza.AssertContains(t, newText, "- Third heading", "Should have third list item")

								// Check that content is preserved
								testza.AssertContains(t, newText, "Some content", "Should preserve first heading content")
								testza.AssertContains(t, newText, "More content", "Should preserve second heading content")
								testza.AssertContains(t, newText, "Even more content", "Should preserve third heading content")
							}
						}
					})
				})
		},
	)
}

func TestHeadingWithChildrenToBulletList(t *testing.T) {
	Given("a file with nested headings", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			tc.GivenFile("test.org", `* Parent heading
Some parent content

** Child heading
Child content

** Another child
More child content
`)
			tc.GivenOpenFile("test.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			// Select just the parent heading line
			params := protocol.CodeActionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: tc.DocURI("test.org")},
				Range: protocol.Range{
					Start: protocol.Position{Line: 0, Character: 0},
					End:   protocol.Position{Line: 0, Character: 15},
				},
			}

			When(t, tc, "requesting code actions", "textDocument/codeAction", params,
				func(t *testing.T, actions []protocol.CodeAction) {
					Then("should convert heading and children to nested list", t, func(t *testing.T) {
						for _, action := range actions {
							if action.Title == "Org: Convert headings to bullet list" {
								edits := action.Edit.Changes[tc.DocURI("test.org")]
								testza.AssertLen(t, edits, 1, "Should have one text edit")

								newText := edits[0].NewText
								testza.AssertContains(t, newText, "- Parent heading", "Should have parent as list item")
								testza.AssertContains(t, newText, "+ Child heading", "Should have child with deeper bullet")
								testza.AssertContains(t, newText, "+ Another child", "Should have another child")

								// Check content preservation
								testza.AssertContains(t, newText, "Some parent content", "Should preserve parent content")
								testza.AssertContains(t, newText, "Child content", "Should preserve child content")
								testza.AssertContains(t, newText, "More child content", "Should preserve other child content")
							}
						}
					})
				})
		},
	)
}

func TestHeadingToOrderedList(t *testing.T) {
	Given("a file with headings", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			tc.GivenFile("test.org", `* First item
Content one

* Second item
Content two

* Third item
Content three
`)
			tc.GivenOpenFile("test.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			// Select all headings
			params := protocol.CodeActionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: tc.DocURI("test.org")},
				Range: protocol.Range{
					Start: protocol.Position{Line: 0, Character: 0},
					End:   protocol.Position{Line: 6, Character: 15},
				},
			}

			When(t, tc, "requesting code actions", "textDocument/codeAction", params,
				func(t *testing.T, actions []protocol.CodeAction) {
					Then("should convert to ordered list with proper numbering", t, func(t *testing.T) {
						for _, action := range actions {
							if action.Title == "Org: Convert headings to ordered list" {
								edits := action.Edit.Changes[tc.DocURI("test.org")]
								testza.AssertLen(t, edits, 1, "Should have one text edit")

								newText := edits[0].NewText
								testza.AssertContains(t, newText, "1. First item", "Should have numbered first item")
								testza.AssertContains(t, newText, "2. Second item", "Should have numbered second item")
								testza.AssertContains(t, newText, "3. Third item", "Should have numbered third item")

								// Check content preservation
								testza.AssertContains(t, newText, "Content one", "Should preserve first content")
								testza.AssertContains(t, newText, "Content two", "Should preserve second content")
								testza.AssertContains(t, newText, "Content three", "Should preserve third content")
							}
						}
					})
				})
		},
	)
}

func TestHeadingWithPropertiesDrawer(t *testing.T) {
	Given("a heading with properties drawer", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			tc.GivenFile("test.org", `** Part one
:PROPERTIES:
:ID: part-one-id
:END:

Some content here.

*** Birth rates
:PROPERTIES:
:ID: birth-rates-id
:END:

The reason for this is simple.

*** Pro-natalist policies
:PROPERTIES:
:ID: pro-natalist-id
:END:

Some argue that government intervention can fix this.
`)
			tc.GivenOpenFile("test.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			// Select just the Birth rates heading and its properties drawer
			params := protocol.CodeActionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: tc.DocURI("test.org")},
				Range: protocol.Range{
					Start: protocol.Position{Line: 6, Character: 0},
					End:   protocol.Position{Line: 9, Character: 0},
				},
			}

			When(t, tc, "requesting code actions", "textDocument/codeAction", params,
				func(t *testing.T, actions []protocol.CodeAction) {
					Then("should convert heading to list item preserving content", t, func(t *testing.T) {
						var foundAction bool
						for _, action := range actions {
							if action.Title == "Org: Convert headings to bullet list" {
								foundAction = true
								edits := action.Edit.Changes[tc.DocURI("test.org")]
								testza.AssertLen(t, edits, 1, "Should have one text edit")

								edit := edits[0]
								newText := edit.NewText

								// Check that properties drawer is preserved
								testza.AssertContains(t, newText, ":PROPERTIES:", "Should preserve properties drawer")
								testza.AssertContains(t, newText, ":ID: birth-rates-id", "Should preserve ID property")
								testza.AssertContains(t, newText, ":END:", "Should preserve properties drawer end")

								// Check that content is preserved
								testza.AssertContains(t, newText, "The reason for this is simple", "Should preserve content")

								// Check that heading is converted to list
								testza.AssertContains(t, newText, "- Birth rates", "Should convert heading to list item")

								// Check that the NEXT heading is NOT included (this is the bug!)
								testza.AssertNotContains(t, newText, "Pro-natalist policies", "Should not include next heading")
							}
						}
						testza.AssertTrue(t, foundAction, "Should have bullet list conversion action")
					})
				})
		},
	)
}

func TestHeadingPreservesFullSubtree(t *testing.T) {
	Given("a heading with nested content and subheadings", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			tc.GivenFile("test.org", `** Parent heading
:PROPERTIES:
:ID: parent-id
:END:

Parent content here.

*** Child heading
:PROPERTIES:
:ID: child-id
:END:

Child content here.

**** Grandchild heading
:PROPERTIES:
:ID: grandchild-id
:END:

Grandchild content.

*** Sibling heading
:PROPERTIES:
:ID: sibling-id
:END:

Sibling content.
`)
			tc.GivenOpenFile("test.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			// Select the parent heading and its properties
			params := protocol.CodeActionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: tc.DocURI("test.org")},
				Range: protocol.Range{
					Start: protocol.Position{Line: 0, Character: 0},
					End:   protocol.Position{Line: 3, Character: 0},
				},
			}

			When(t, tc, "requesting code actions", "textDocument/codeAction", params,
				func(t *testing.T, actions []protocol.CodeAction) {
					Then("should convert entire subtree to nested list", t, func(t *testing.T) {
						for _, action := range actions {
							if action.Title == "Org: Convert headings to bullet list" {
								edits := action.Edit.Changes[tc.DocURI("test.org")]
								testza.AssertLen(t, edits, 1, "Should have one text edit")

								newText := edits[0].NewText

								// Parent should become bullet
								testza.AssertContains(t, newText, "- Parent heading", "Should convert parent to list")

								// Child should become indented bullet (+)
								testza.AssertContains(t, newText, "+ Child heading", "Should convert child to nested list")

								// Grandchild should become doubly indented bullet (*)
								testza.AssertContains(t, newText, "* Grandchild heading", "Should convert grandchild to doubly nested list")

								// Sibling should be at same level as child (+)
								testza.AssertContains(t, newText, "+ Sibling heading", "Should convert sibling to same level as child")

								// All content should be preserved
								testza.AssertContains(t, newText, "Parent content", "Should preserve parent content")
								testza.AssertContains(t, newText, "Child content", "Should preserve child content")
								testza.AssertContains(t, newText, "Grandchild content", "Should preserve grandchild content")
								testza.AssertContains(t, newText, "Sibling content", "Should preserve sibling content")

								// All properties should be preserved
								testza.AssertContains(t, newText, ":ID: parent-id", "Should preserve parent ID")
								testza.AssertContains(t, newText, ":ID: child-id", "Should preserve child ID")
								testza.AssertContains(t, newText, ":ID: grandchild-id", "Should preserve grandchild ID")
								testza.AssertContains(t, newText, ":ID: sibling-id", "Should preserve sibling ID")
							}
						}
					})
				})
		},
	)
}

func TestLargeHeadingConversion(t *testing.T) {
	Given("a large heading with extensive content and subheadings", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			tc.GivenFile("test.org", `** Part one
:PROPERTIES:
:ID: part-one-id
:END:

Some intro content here.

*** Birth rates
:PROPERTIES:
:ID: birth-rates-id
:END:

The reason for this is simple, and it's one countries as diverse as China, France, and Japan are facing right now. The future is here, it's just not evenly distributed yet.

As a nation gets wealthier and more educated — as GDP per capita and educational attainment increase — its birth rate inevitably declines. While we don't fully understand all the nuanced sociological, economic, and cultural reasons for this, it is an observable pattern across time and across a vastly different number of countries and cultures.

There are a number of reasons for this: one, the richer and more educated a population is, the busier they become engaging in their careers, educating themselves, and actually enjoying the fruits of that wealth through travel, personal projects, and self-fulfillment.

**** Economic factors
:PROPERTIES:
:ID: economic-factors-id
:END:

The insane time and wealth investment of having children, which can temporarily or even permanently grind your entire life to a halt and redirects your focus from attainment and enjoyment towards the well-being of others, becomes a stark contrast to a life of personal freedom.

Then, at the same time, the traditional incentives towards having children essentially evaporate. You no longer need kids to support you in old age, as personal savings and the general social safety net take over that role.

**** Cultural shifts
:PROPERTIES:
:ID: cultural-shifts-id
:END:

Furthermore, as people become wealthier and more educated, they tend to move into more urban areas where the amount of available living space decreases, meaning you simply don't have the space for children to naturally play and grow.

But perhaps the most complex and powerful driver of this decline is the skyrocketing cost of competitive child-rearing.

*** Pro-natalist policies
:PROPERTIES:
:ID: pro-natalist-id
:END:

Some argue that government intervention can fix this through positive financial incentives, but if you look at countries with "gold standard" semi-socialist pro-natalist policies like in Scandinavia, which provide things like a year of paid leave, universal high-quality childcare, and massive subsidies, you still see abysmally low birth rates.

** Part two
:PROPERTIES:
:ID: part-two-id
:END:

There's a problem with just using immigration as a panacea for this, though: it's thinking too locally.
`)
			tc.GivenOpenFile("test.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			// Select just the Birth rates heading and its properties drawer
			// Lines 6-9 (0-indexed: line 6 is "*** Birth rates", line 9 is ":END:")
			params := protocol.CodeActionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: tc.DocURI("test.org")},
				Range: protocol.Range{
					Start: protocol.Position{Line: 6, Character: 0},
					End:   protocol.Position{Line: 9, Character: 6},
				},
			}

			When(t, tc, "requesting code actions", "textDocument/codeAction", params,
				func(t *testing.T, actions []protocol.CodeAction) {
					Then("should preserve ALL content in the Birth rates section", t, func(t *testing.T) {
						var foundAction bool
						for _, action := range actions {
							if action.Title == "Org: Convert headings to bullet list" {
								foundAction = true
								edits := action.Edit.Changes[tc.DocURI("test.org")]
								testza.AssertLen(t, edits, 1, "Should have one text edit")

								edit := edits[0]
								newText := edit.NewText

								// Debug: print the new text to see what we're getting
								t.Logf("NewText length: %d", len(newText))
								t.Logf("NewText:\n%s", newText)

								// Check that properties drawer is preserved
								testza.AssertContains(t, newText, ":PROPERTIES:", "Should preserve properties drawer")
								testza.AssertContains(t, newText, ":ID: birth-rates-id", "Should preserve ID property")

								// Check that the main content is preserved
								testza.AssertContains(t, newText, "The reason for this is simple", "Should preserve main content")
								testza.AssertContains(t, newText, "As a nation gets wealthier", "Should preserve second paragraph")
								testza.AssertContains(t, newText, "There are a number of reasons", "Should preserve third paragraph")

								// Check that subheadings are converted but preserved
								testza.AssertContains(t, newText, "Economic factors", "Should preserve Economic factors subheading")
								testza.AssertContains(t, newText, "Cultural shifts", "Should preserve Cultural shifts subheading")

								// Check that subheading content is preserved
								testza.AssertContains(t, newText, "The insane time and wealth investment", "Should preserve Economic factors content")
								testza.AssertContains(t, newText, "Furthermore, as people become wealthier", "Should preserve Cultural shifts content")

								// Check that subheading properties are preserved
								testza.AssertContains(t, newText, ":ID: economic-factors-id", "Should preserve Economic factors ID")
								testza.AssertContains(t, newText, ":ID: cultural-shifts-id", "Should preserve Cultural shifts ID")

								// CRITICAL: Check that we DON'T include content from other sections
								testza.AssertNotContains(t, newText, "Pro-natalist policies", "Should NOT include next major heading")
								testza.AssertNotContains(t, newText, "Some argue that government intervention", "Should NOT include Pro-natalist content")
								testza.AssertNotContains(t, newText, "Part two", "Should NOT include Part two heading")
								testza.AssertNotContains(t, newText, "There's a problem with just using immigration", "Should NOT include Part two content")
							}
						}
						testza.AssertTrue(t, foundAction, "Should have bullet list conversion action")
					})
				})
		},
	)
}
