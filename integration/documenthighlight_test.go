package integration

import (
	"testing"

	"github.com/MarvinJWendt/testza"
	"go.lsp.dev/protocol"
)

func TestDocumentHighlightTag(t *testing.T) {
	Given("a file with multiple headlines sharing a tag", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			content := `* First Heading :work:
Content here.

* Second Heading :personal:
Different content.

* Third Heading :work:
More work content.
`

			tc.GivenFile("tags.org", content).
				GivenOpenFile("tags.org")

			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			// Position cursor on the :work: tag in first heading
			params := protocol.DocumentHighlightParams{
				TextDocumentPositionParams: protocol.TextDocumentPositionParams{
					TextDocument: protocol.TextDocumentIdentifier{
						URI: tc.DocURI("tags.org"),
					},
					Position: tc.PosAfter("tags.org", ":wor"),
				},
			}

			When(t, tc, "requesting highlights on a tag", "textDocument/documentHighlight", params, func(t *testing.T, result []protocol.DocumentHighlight) {
				Then("returns highlights for all headlines with that tag", t, func(t *testing.T) {
					testza.AssertGreaterOrEqual(t, len(result), 2, "Expected at least 2 highlights (both :work: headings)")

					// Verify we got highlights for line 0 and line 6 (both have :work:)
					highlightedLines := make(map[uint32]bool)
					for _, h := range result {
						highlightedLines[h.Range.Start.Line] = true
					}

					testza.AssertTrue(t, highlightedLines[0], "Expected highlight on line 0 (First Heading)")
					testza.AssertTrue(t, highlightedLines[6], "Expected highlight on line 6 (Third Heading)")
					testza.AssertFalse(t, highlightedLines[3], "Should not highlight line 3 (personal tag)")
				})
			})
		},
	)
}

func TestDocumentHighlightTagNotOnTag(t *testing.T) {
	Given("a file with tagged headlines", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			content := `* First Heading :work:
Content here.
`

			tc.GivenFile("notag.org", content).
				GivenOpenFile("notag.org")

			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			// Position cursor in the content area, not on the tag
			params := protocol.DocumentHighlightParams{
				TextDocumentPositionParams: protocol.TextDocumentPositionParams{
					TextDocument: protocol.TextDocumentIdentifier{
						URI: tc.DocURI("notag.org"),
					},
					Position: tc.PosAfter("notag.org", "Content"),
				},
			}

			When(t, tc, "requesting highlights not on a tag", "textDocument/documentHighlight", params, func(t *testing.T, result []protocol.DocumentHighlight) {
				Then("returns empty result", t, func(t *testing.T) {
					testza.AssertLen(t, result, 0, "Expected no highlights when not on a tag")
				})
			})
		},
	)
}

func TestDocumentHighlightLink(t *testing.T) {
	Given("a file with multiple links to the same target", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			content := `* Notes
See [[file:target.org][target]] for info.
Also check [[file:target.org]] for more.
And [[file:other.org][different target]].

* More content
Reference [[file:target.org][the target]] again.
`

			tc.GivenFile("links.org", content).
				GivenOpenFile("links.org")

			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			// Position cursor on first link to target.org
			params := protocol.DocumentHighlightParams{
				TextDocumentPositionParams: protocol.TextDocumentPositionParams{
					TextDocument: protocol.TextDocumentIdentifier{
						URI: tc.DocURI("links.org"),
					},
					Position: tc.PosAfter("links.org", "[[file:target.org][tar"),
				},
			}

			When(t, tc, "requesting highlights on a link", "textDocument/documentHighlight", params, func(t *testing.T, result []protocol.DocumentHighlight) {
				Then("returns highlights for all links to same target", t, func(t *testing.T) {
					// Should find 3 links to target.org (lines 1, 2, and 6)
					testza.AssertEqual(t, 3, len(result), "Expected 3 highlights for target.org links")

					// Verify the ranges are for the link positions
					for _, highlight := range result {
						testza.AssertEqual(t, protocol.DocumentHighlightKindRead, highlight.Kind)
					}
				})
			})
		},
	)
}

func TestDocumentHighlightIDLink(t *testing.T) {
	Given("a file with id links", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			tc.WithUUID("testID")

			content := `* Source
Link to [[id:{{.testID}}][target]] here.
Another [[id:{{.testID}}][reference]] to same.
And [[id:different-id][different target]].
`

			tc.GivenFile("idlinks.org", content).
				GivenOpenFile("idlinks.org")

			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			// Position cursor on first id link
			params := protocol.DocumentHighlightParams{
				TextDocumentPositionParams: protocol.TextDocumentPositionParams{
					TextDocument: protocol.TextDocumentIdentifier{
						URI: tc.DocURI("idlinks.org"),
					},
					Position: tc.PosAfter("idlinks.org", "[[id:"),
				},
			}

			When(t, tc, "requesting highlights on an id link", "textDocument/documentHighlight", params, func(t *testing.T, result []protocol.DocumentHighlight) {
				Then("returns highlights for all links with same id", t, func(t *testing.T) {
					// Should find 2 links with the same testID
					testza.AssertEqual(t, 2, len(result), "Expected 2 highlights for same ID links")
				})
			})
		},
	)
}

func TestDocumentHighlightNoLinks(t *testing.T) {
	Given("a file with no links", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			content := `* Just a heading
Some regular text without any links.
- List item 1
- List item 2
`

			tc.GivenFile("nolinks.org", content).
				GivenOpenFile("nolinks.org")

			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.DocumentHighlightParams{
				TextDocumentPositionParams: protocol.TextDocumentPositionParams{
					TextDocument: protocol.TextDocumentIdentifier{
						URI: tc.DocURI("nolinks.org"),
					},
					Position: tc.PosAfter("nolinks.org", "regular"),
				},
			}

			When(t, tc, "requesting highlights in document without links", "textDocument/documentHighlight", params, func(t *testing.T, result []protocol.DocumentHighlight) {
				Then("returns empty result", t, func(t *testing.T) {
					testza.AssertLen(t, result, 0, "Expected no highlights")
				})
			})
		},
	)
}
