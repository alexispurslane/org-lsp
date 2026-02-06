package integration

import (
	"testing"

	"github.com/MarvinJWendt/testza"
	"go.lsp.dev/protocol"
)

func TestHeadingFolding(t *testing.T) {
	Given("an org file with nested headings", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			content := `* Heading 1
Content under heading 1
More content here
** Subheading 1.1
Content under 1.1
* Heading 2
Content under heading 2`

			tc.GivenFile("folding.org", content).
				GivenOpenFile("folding.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.FoldingRangeParams{
				TextDocumentPositionParams: protocol.TextDocumentPositionParams{
					TextDocument: protocol.TextDocumentIdentifier{
						URI: tc.DocURI("folding.org"),
					},
				},
			}

			When(t, tc, "requesting folding ranges", "textDocument/foldingRange", params, func(t *testing.T, ranges []protocol.FoldingRange) {
				Then("returns folding ranges for all headings", t, func(t *testing.T) {
					testza.AssertLen(t, ranges, 3, "Expected 3 folding ranges (2 headings + 1 subheading)")

					// Heading 1: starts at line 1 (after heading), ends at line 4 (before Heading 2)
					testza.AssertEqual(t, uint32(1), ranges[0].StartLine, "Heading 1 should start at line 1")
					testza.AssertEqual(t, uint32(4), ranges[0].EndLine, "Heading 1 should end at line 4")
					testza.AssertEqual(t, protocol.CommentFoldingRange, ranges[0].Kind, "Headings should use Comment kind")

					// Subheading 1.1: starts at line 4 (after subheading), ends at line 4
					testza.AssertEqual(t, uint32(4), ranges[1].StartLine, "Subheading 1.1 should start at line 4")
					testza.AssertEqual(t, uint32(4), ranges[1].EndLine, "Subheading 1.1 should end at line 4")
					testza.AssertEqual(t, protocol.CommentFoldingRange, ranges[1].Kind, "Subheadings should use Comment kind")

					// Heading 2: starts at line 6 (after heading), ends at line 6
					testza.AssertEqual(t, uint32(6), ranges[2].StartLine, "Heading 2 should start at line 6")
					testza.AssertEqual(t, uint32(6), ranges[2].EndLine, "Heading 2 should end at line 6")
				})
			})
		},
	)
}

func TestBlockFolding(t *testing.T) {
	Given("an org file with source blocks", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			content := `* Notes
Some text here

#+begin_src go
package main
func main() {
    println("hello")
}
#+end_src

More text after
** Subheading
Final content`

			tc.GivenFile("blocks.org", content).
				GivenOpenFile("blocks.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.FoldingRangeParams{
				TextDocumentPositionParams: protocol.TextDocumentPositionParams{
					TextDocument: protocol.TextDocumentIdentifier{
						URI: tc.DocURI("blocks.org"),
					},
				},
			}

			When(t, tc, "requesting folding ranges", "textDocument/foldingRange", params, func(t *testing.T, ranges []protocol.FoldingRange) {
				Then("returns folding ranges including blocks", t, func(t *testing.T) {
					// Should have: Notes heading, src block, Subheading
					testza.AssertLen(t, ranges, 3, "Expected 3 folding ranges")

					// Find the block range (should be Region kind)
					var blockRange *protocol.FoldingRange
					for i := range ranges {
						if ranges[i].Kind == protocol.RegionFoldingRange {
							blockRange = &ranges[i]
							break
						}
					}

					testza.AssertNotNil(t, blockRange, "Should have a block folding range")
					testza.AssertEqual(t, uint32(3), blockRange.StartLine, "Block should start at line 3")
					testza.AssertEqual(t, uint32(8), blockRange.EndLine, "Block should end at line 8")
				})
			})
		},
	)
}

func TestDrawerFolding(t *testing.T) {
	Given("an org file with a properties drawer", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			content := `* Task
:PROPERTIES:
:ID: abc123
:END:
Content here
More content`

			tc.GivenFile("drawer.org", content).
				GivenOpenFile("drawer.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.FoldingRangeParams{
				TextDocumentPositionParams: protocol.TextDocumentPositionParams{
					TextDocument: protocol.TextDocumentIdentifier{
						URI: tc.DocURI("drawer.org"),
					},
				},
			}

			When(t, tc, "requesting folding ranges", "textDocument/foldingRange", params, func(t *testing.T, ranges []protocol.FoldingRange) {
				Then("returns folding ranges including drawers", t, func(t *testing.T) {
					// Should have: drawer (Region), heading (Comment)
					testza.AssertLen(t, ranges, 2, "Expected 2 folding ranges")

					// Find the drawer range (should be Region kind)
					var drawerRange *protocol.FoldingRange
					for i := range ranges {
						if ranges[i].Kind == protocol.RegionFoldingRange {
							drawerRange = &ranges[i]
							break
						}
					}

					testza.AssertNotNil(t, drawerRange, "Should have a drawer folding range")
					testza.AssertEqual(t, uint32(1), drawerRange.StartLine, "Drawer should start at line 1")
					testza.AssertEqual(t, uint32(3), drawerRange.EndLine, "Drawer should end at line 3")
				})
			})
		},
	)
}

func TestEmptyFileFolding(t *testing.T) {
	Given("an empty org file", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			tc.GivenFile("empty.org", "").
				GivenOpenFile("empty.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.FoldingRangeParams{
				TextDocumentPositionParams: protocol.TextDocumentPositionParams{
					TextDocument: protocol.TextDocumentIdentifier{
						URI: tc.DocURI("empty.org"),
					},
				},
			}

			When(t, tc, "requesting folding ranges", "textDocument/foldingRange", params, func(t *testing.T, ranges []protocol.FoldingRange) {
				Then("returns empty result", t, func(t *testing.T) {
					testza.AssertLen(t, ranges, 0, "Empty file should have no folding ranges")
				})
			})
		},
	)
}
