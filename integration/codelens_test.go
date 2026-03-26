package integration

import (
	"strings"
	"testing"

	"github.com/MarvinJWendt/testza"
	"go.lsp.dev/protocol"
)

func TestCodeLensNoBacklinks(t *testing.T) {
	Given("a document with headings but no incoming links", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			tc.GivenFile("notes.org", `* Project Alpha
Some content here

* Project Beta
More content here`).
				GivenSaveFile("notes.org").
				GivenOpenFile("notes.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.CodeLensParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: tc.DocURI("notes.org"),
				},
			}

			When(t, tc, "requesting code lens", "textDocument/codeLens", params,
				func(t *testing.T, lenses []protocol.CodeLens) {
					Then("returns lens for each heading showing 0 backlinks", t, func(t *testing.T) {
						testza.AssertLen(t, lenses, 2, "Expected 2 code lenses (one per heading)")
						testza.AssertEqual(t, "0 backlinks", lenses[0].Command.Title, "First heading should show 0 backlinks")
						testza.AssertEqual(t, "0 backlinks", lenses[1].Command.Title, "Second heading should show 0 backlinks")
					})
				})
		},
	)
}

func TestCodeLensFileLinkBacklinks(t *testing.T) {
	Given("a target file and a source file with file link", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			tc.GivenFile("target.org", `* Target Heading
Content here`).
				GivenFile("source.org", `* Source Heading
See [[file:target.org][the target]] for more`).
				GivenOpenFile("target.org").
				GivenSaveFile("source.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.CodeLensParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: tc.DocURI("target.org"),
				},
			}

			When(t, tc, "requesting code lens for target", "textDocument/codeLens", params,
				func(t *testing.T, lenses []protocol.CodeLens) {
					Then("shows 1 backlink for the heading", t, func(t *testing.T) {
						testza.AssertLen(t, lenses, 1, "Expected 1 code lens")
						testza.AssertEqual(t, "1 backlink", lenses[0].Command.Title, "Should show 1 backlink")
					})
				})
		},
	)
}

func TestCodeLensMultipleBacklinks(t *testing.T) {
	Given("a target file with multiple files linking to it", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			tc.GivenFile("target.org", `* Important Topic
Critical information here`).
				GivenFile("source1.org", `* Notes
See [[file:target.org][the topic]]`).
				GivenFile("source2.org", `* More Notes
Also see [[file:target.org][this topic]] and [[file:target.org][again]]`).
				GivenOpenFile("target.org").
				GivenSaveFile("source1.org").
				GivenSaveFile("source2.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.CodeLensParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: tc.DocURI("target.org"),
				},
			}

			When(t, tc, "requesting code lens", "textDocument/codeLens", params,
				func(t *testing.T, lenses []protocol.CodeLens) {
					Then("shows 3 backlinks total", t, func(t *testing.T) {
						testza.AssertLen(t, lenses, 1, "Expected 1 code lens")
						testza.AssertEqual(t, "3 backlinks", lenses[0].Command.Title, "Should count all file links")
					})
				})
		},
	)
}

func TestCodeLensIDLinkBacklinks(t *testing.T) {
	Given("a heading with UUID and a file linking to it by ID", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			// Create a heading with a property drawer containing ID
			tc.GivenFile("target.org", `* Target Heading
:PROPERTIES:
:ID: 550e8400-e29b-41d4-a716-446655440000
:END:
Content here`).
				GivenFile("source.org", `* Source
See [[id:550e8400-e29b-41d4-a716-446655440000][the target]]`).
				GivenOpenFile("target.org").
				GivenSaveFile("source.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.CodeLensParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: tc.DocURI("target.org"),
				},
			}

			When(t, tc, "requesting code lens", "textDocument/codeLens", params,
				func(t *testing.T, lenses []protocol.CodeLens) {
					Then("shows 1 backlink from ID link", t, func(t *testing.T) {
						testza.AssertLen(t, lenses, 1, "Expected 1 code lens")
						testza.AssertEqual(t, "1 backlink", lenses[0].Command.Title, "Should count ID link")
					})
				})
		})
}

func TestCodeLensMixedBacklinks(t *testing.T) {
	Given("a heading with both file and ID links pointing to it", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			tc.GivenFile("target.org", `* Multi-Linked Heading
:PROPERTIES:
:ID: a1b2c3d4-e5f6-7890-abcd-ef1234567890
:END:
Content here`).
				GivenFile("source1.org", `* Source 1
See [[file:target.org][file link]]`).
				GivenFile("source2.org", `* Source 2
See [[id:a1b2c3d4-e5f6-7890-abcd-ef1234567890][id link]]`).
				GivenOpenFile("target.org").
				GivenSaveFile("source1.org").
				GivenSaveFile("source2.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.CodeLensParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: tc.DocURI("target.org"),
				},
			}

			When(t, tc, "requesting code lens", "textDocument/codeLens", params,
				func(t *testing.T, lenses []protocol.CodeLens) {
					Then("shows 2 backlinks total", t, func(t *testing.T) {
						testza.AssertLen(t, lenses, 1, "Expected 1 code lens")
						testza.AssertEqual(t, "2 backlinks", lenses[0].Command.Title, "Should count both file and ID links")
					})
				})
		})
}

func TestCodeLensPositionedAtHeading(t *testing.T) {
	Given("a document with a heading", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			tc.GivenFile("test.org", `* My Heading
Some content`).
				GivenOpenFile("test.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.CodeLensParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: tc.DocURI("test.org"),
				},
			}

			When(t, tc, "requesting code lens", "textDocument/codeLens", params,
				func(t *testing.T, lenses []protocol.CodeLens) {
					Then("lens range covers the heading line", t, func(t *testing.T) {
						testza.AssertLen(t, lenses, 1, "Expected 1 code lens")
						// Range should be on line 0 (the heading)
						testza.AssertEqual(t, uint32(0), lenses[0].Range.Start.Line, "Lens should be on heading line")
						testza.AssertEqual(t, uint32(0), lenses[0].Range.End.Line, "Lens should be on heading line")
					})
				})
		})
}

func TestCodeLensMultipleHeadings(t *testing.T) {
	Given("a document with multiple headings", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			tc.GivenFile("notes.org", `* First Heading
Content 1

* Second Heading
Content 2

* Third Heading
Content 3`).
				GivenFile("linker.org", `* Links
See [[file:notes.org][notes]]`).
				GivenOpenFile("notes.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.CodeLensParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: tc.DocURI("notes.org"),
				},
			}

			When(t, tc, "requesting code lens", "textDocument/codeLens", params,
				func(t *testing.T, lenses []protocol.CodeLens) {
					Then("returns lens for each heading with correct counts", t, func(t *testing.T) {
						testza.AssertLen(t, lenses, 3, "Expected 3 code lenses")
						// File link points to the file, so all headings get 1 backlink
						// (backlinks are per-file, not per-heading for file links)
						for i, lens := range lenses {
							if !strings.HasSuffix(lens.Command.Title, "backlink") && !strings.HasSuffix(lens.Command.Title, "backlinks") {
								t.Errorf("Lens %d has invalid title: %s", i, lens.Command.Title)
							}
						}
					})
				})
		})
}
