package integration

import (
	"strings"
	"testing"

	"github.com/MarvinJWendt/testza"
	"go.lsp.dev/protocol"
)

func ptrTo[T any](v T) *T {
	return &v
}

func TestIDCompletion(t *testing.T) {
	Given("a target file with UUID heading and source file with [[id: prefix", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			tc.WithUUID("targetID")

			targetContent := `* Target Heading
:PROPERTIES:
:ID:       {{.targetID}}
:END:
Content here.`

			sourceContent := "* Source Heading\nSome text with [[id:"

			tc.GivenFile("target.org", targetContent).
				GivenFile("source.org", sourceContent).
				GivenSaveFile("target.org").
				GivenOpenFile("source.org")

			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.CompletionParams{
				TextDocumentPositionParams: protocol.TextDocumentPositionParams{
					TextDocument: protocol.TextDocumentIdentifier{
						URI: protocol.DocumentURI(tc.rootURI + "/source.org"),
					},
					Position: protocol.Position{Line: 1, Character: 20},
				},
			}

			When(t, tc, "requesting completion after [[id:", "textDocument/completion", params, func(t *testing.T, result *protocol.CompletionList) {
				Then("returns ID completion items with heading title as label and UUID as insert text", t, func(t *testing.T) {
					testza.AssertNotNil(t, result, "Expected completion result")
					testza.AssertFalse(t, result.IsIncomplete, "Expected complete list")

					// Find ID reference items
					var idItems []protocol.CompletionItem
					for _, item := range result.Items {
						if item.Kind == protocol.CompletionItemKindReference {
							idItems = append(idItems, item)
						}
					}
					testza.AssertTrue(t, len(idItems) > 0, "Expected ID completion items")

					// Find our test UUID
					foundTarget := false
					for _, item := range idItems {
						if strings.HasPrefix(item.InsertText, tc.TestData["targetID"]) {
							foundTarget = true
							testza.AssertEqual(t, "Target Heading", item.Label, "Label should be heading title")
							testza.AssertTrue(t, strings.HasSuffix(item.InsertText, "]]"), "InsertText should include closing brackets")
							break
						}
					}
					testza.AssertTrue(t, foundTarget, "Expected to find Target Heading in completion")
				})
			})
		},
	)
}

func TestTagCompletion(t *testing.T) {
	Given("a file with tags and source file with : prefix in headline", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)

			targetContent := `* Target Heading :testtag:anothertag:
:PROPERTIES:
:ID:       22222222-2222-2222-2222-222222222222
:END:
Content here.`

			sourceContent := "* Source Heading :"

			tc.GivenFile("target.org", targetContent).
				GivenFile("source.org", sourceContent).
				GivenSaveFile("target.org").
				GivenOpenFile("source.org")

			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.CompletionParams{
				TextDocumentPositionParams: protocol.TextDocumentPositionParams{
					TextDocument: protocol.TextDocumentIdentifier{
						URI: protocol.DocumentURI(tc.rootURI + "/source.org"),
					},
					Position: protocol.Position{Line: 0, Character: 18},
				},
			}

			When(t, tc, "requesting completion after : in headline", "textDocument/completion", params, func(t *testing.T, result *protocol.CompletionList) {
				Then("returns tag completion items", t, func(t *testing.T) {
					testza.AssertNotNil(t, result, "Expected completion result")

					// Find tag items
					var tagItems []protocol.CompletionItem
					for _, item := range result.Items {
						if item.Kind == protocol.CompletionItemKindProperty {
							tagItems = append(tagItems, item)
						}
					}
					testza.AssertTrue(t, len(tagItems) > 0, "Expected tag completion items")

					// Check for expected tags
					foundTestTag := false
					foundAnotherTag := false
					for _, item := range tagItems {
						if item.Label == "testtag" {
							foundTestTag = true
						}
						if item.Label == "anothertag" {
							foundAnotherTag = true
						}
					}
					testza.AssertTrue(t, foundTestTag, "Expected to find testtag")
					testza.AssertTrue(t, foundAnotherTag, "Expected to find anothertag")
				})
			})
		},
	)
}

func TestFileLinkCompletion(t *testing.T) {
	Given("multiple org files and source file with [[file: prefix", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)

			tc.GivenFile("target1.org", "* Test File 1\nContent here.").
				GivenFile("target2.org", "* Test File 2\nMore content.").
				GivenFile("source.org", "* Source File\nLink to file: [[file:").
				GivenSaveFile("target1.org").
				GivenOpenFile("source.org")

			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.CompletionParams{
				TextDocumentPositionParams: protocol.TextDocumentPositionParams{
					TextDocument: protocol.TextDocumentIdentifier{
						URI: protocol.DocumentURI(tc.rootURI + "/source.org"),
					},
					Position: protocol.Position{Line: 1, Character: 21},
				},
			}

			When(t, tc, "requesting completion after [[file:", "textDocument/completion", params, func(t *testing.T, result *protocol.CompletionList) {
				Then("returns file completion items for org files", t, func(t *testing.T) {
					testza.AssertNotNil(t, result, "Expected completion result")

					// Debug: print all items
					t.Logf("Total completion items: %d", len(result.Items))
					for i, item := range result.Items {
						t.Logf("Item %d: Label=%q Kind=%v", i, item.Label, item.Kind)
					}

					// Find file items
					var fileItems []protocol.CompletionItem
					for _, item := range result.Items {
						if item.Kind == protocol.CompletionItemKindFile {
							fileItems = append(fileItems, item)
						}
					}
					t.Logf("File completion items found: %d", len(fileItems))
					testza.AssertTrue(t, len(fileItems) > 0, "Expected file completion items")

					// Check that our test files are in the results
					foundTarget1 := false
					foundTarget2 := false
					for _, item := range fileItems {
						if strings.Contains(item.Label, "target1.org") {
							foundTarget1 = true
						}
						if strings.Contains(item.Label, "target2.org") {
							foundTarget2 = true
						}
					}
					testza.AssertTrue(t, foundTarget1, "Expected to find target1.org in completions")
					testza.AssertTrue(t, foundTarget2, "Expected to find target2.org in completions")
				})
			})
		},
	)
}

func TestBlockTypeCompletion(t *testing.T) {
	Given("a file with #+begin_ prefix", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			tc.GivenFile("blocks.org", "#+begin_").
				GivenOpenFile("blocks.org")

			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.CompletionParams{
				TextDocumentPositionParams: protocol.TextDocumentPositionParams{
					TextDocument: protocol.TextDocumentIdentifier{
						URI: protocol.DocumentURI(tc.rootURI + "/blocks.org"),
					},
					Position: protocol.Position{Line: 0, Character: 8},
				},
			}

			When(t, tc, "requesting completion after #+begin_", "textDocument/completion", params, func(t *testing.T, result *protocol.CompletionList) {
				Then("returns block type completion items", t, func(t *testing.T) {
					testza.AssertNotNil(t, result, "Expected completion result")

					// Find keyword items (block types)
					var blockItems []protocol.CompletionItem
					for _, item := range result.Items {
						if item.Kind == protocol.CompletionItemKindKeyword {
							blockItems = append(blockItems, item)
						}
					}
					testza.AssertTrue(t, len(blockItems) > 0, "Expected block type completion items")

					// Check for expected block types
					foundTypes := make(map[string]bool)
					for _, item := range blockItems {
						foundTypes[item.Label] = true
					}

					testza.AssertTrue(t, foundTypes["#+begin_quote"], "Expected '#+begin_quote' block type")
					testza.AssertTrue(t, foundTypes["#+begin_src"], "Expected '#+begin_src' block type")
					testza.AssertTrue(t, foundTypes["#+begin_verse"], "Expected '#+begin_verse' block type")
				})
			})
		},
	)
}

func TestExportBlockCompletion(t *testing.T) {
	Given("a file with #+begin_export_ prefix", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			tc.GivenFile("export.org", "#+begin_export_").
				GivenOpenFile("export.org")

			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.CompletionParams{
				TextDocumentPositionParams: protocol.TextDocumentPositionParams{
					TextDocument: protocol.TextDocumentIdentifier{
						URI: protocol.DocumentURI(tc.rootURI + "/export.org"),
					},
					Position: protocol.Position{Line: 0, Character: 15},
				},
			}

			When(t, tc, "requesting completion after #+begin_export_", "textDocument/completion", params, func(t *testing.T, result *protocol.CompletionList) {
				Then("returns export format completion items", t, func(t *testing.T) {
					testza.AssertNotNil(t, result, "Expected completion result")

					// Find keyword items
					var exportItems []protocol.CompletionItem
					for _, item := range result.Items {
						if item.Kind == protocol.CompletionItemKindKeyword {
							exportItems = append(exportItems, item)
						}
					}
					testza.AssertTrue(t, len(exportItems) > 0, "Expected export block completion items")

					// Check for expected export types
					foundTypes := make(map[string]bool)
					for _, item := range exportItems {
						foundTypes[item.Label] = true
					}

					testza.AssertTrue(t, foundTypes["#+begin_export_html"], "Expected '#+begin_export_html' export type")
					testza.AssertTrue(t, foundTypes["#+begin_export_latex"], "Expected '#+begin_export_latex' export type")
				})
			})
		},
	)
}

func TestBracketClosingBehavior(t *testing.T) {
	Given("source file with existing ]] brackets after cursor", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			tc.WithUUID("targetID")

			targetContent := `* Target Heading
:PROPERTIES:
:ID:       {{.targetID}}
:END:
Content here.`

			// Note: cursor goes between "id:" and "]]"
			sourceContent := "* Test\nSome text [[id:]] more"

			tc.GivenFile("target.org", targetContent).
				GivenFile("source.org", sourceContent).
				GivenSaveFile("target.org").
				GivenOpenFile("source.org")

			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.CompletionParams{
				TextDocumentPositionParams: protocol.TextDocumentPositionParams{
					TextDocument: protocol.TextDocumentIdentifier{
						URI: protocol.DocumentURI(tc.rootURI + "/source.org"),
					},
					Position: protocol.Position{Line: 1, Character: 19},
				},
			}

			When(t, tc, "requesting completion when ]] already exists", "textDocument/completion", params, func(t *testing.T, result *protocol.CompletionList) {
				Then("returns ID items without closing brackets", t, func(t *testing.T) {
					testza.AssertNotNil(t, result, "Expected completion result")

					// Find ID reference items
					for _, item := range result.Items {
						if item.Kind == protocol.CompletionItemKindReference {
							testza.AssertFalse(t, strings.HasSuffix(item.InsertText, "]]"),
								"InsertText should NOT end with ]] when brackets already exist, got %s", item.InsertText)
						}
					}
				})
			})
		},
	)
}
