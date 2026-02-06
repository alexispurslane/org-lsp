package integration

import (
	"testing"

	"github.com/MarvinJWendt/testza"
	"go.lsp.dev/protocol"
)

func TestDocumentSymbols(t *testing.T) {
	Given("a file with nested headings", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			content := `* First Level Heading
Some content here.

** Second Level Heading
More content.

*** Third Level Heading
Deep content.

** Another Second Level
More stuff.

* Another First Level
Final content.
`

			tc.GivenFile("outline.org", content).
				GivenOpenFile("outline.org")

			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.DocumentSymbolParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: protocol.DocumentURI(tc.rootURI + "/outline.org"),
				},
			}

			When(t, tc, "requesting document symbols", "textDocument/documentSymbol", params, func(t *testing.T, result []protocol.DocumentSymbol) {
				Then("returns hierarchical symbol outline", t, func(t *testing.T) {
					testza.AssertGreaterOrEqual(t, len(result), 2, "Expected at least 2 top-level headings")

					// Find first level heading
					var foundFirstLevel, foundSecondLevel, foundThirdLevel bool

					for _, sym := range result {
						if sym.Name == "First Level Heading" {
							foundFirstLevel = true
							testza.AssertEqual(t, protocol.SymbolKindNamespace, sym.Kind, "First level should be Namespace")
							testza.AssertGreaterOrEqual(t, len(sym.Children), 2, "First level should have children")

							// Check children
							for _, child := range sym.Children {
								if child.Name == "Second Level Heading" {
									foundSecondLevel = true
									testza.AssertEqual(t, protocol.SymbolKindClass, child.Kind, "Second level should be Class")
									testza.AssertGreaterOrEqual(t, len(child.Children), 1, "Second level should have children")

									// Check grandchild
									for _, grandchild := range child.Children {
										if grandchild.Name == "Third Level Heading" {
											foundThirdLevel = true
											testza.AssertEqual(t, protocol.SymbolKindMethod, grandchild.Kind, "Third level should be Method")
										}
									}
								}
							}
						}
					}

					testza.AssertTrue(t, foundFirstLevel, "Expected to find 'First Level Heading'")
					testza.AssertTrue(t, foundSecondLevel, "Expected to find 'Second Level Heading'")
					testza.AssertTrue(t, foundThirdLevel, "Expected to find 'Third Level Heading'")
				})
			})
		},
	)
}

func TestWorkspaceSymbols(t *testing.T) {
	Given("multiple files with UUID headings", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			tc.WithUUID("alphaID").WithUUID("betaID").WithUUID("meetingID").WithUUID("shoppingID")

			content1 := `* Project Alpha :work:
:PROPERTIES:
:ID:       {{.alphaID}}
:END:
First project description.

* Meeting Notes
:PROPERTIES:
:ID:       {{.meetingID}}
:END:
Meeting content here.
** Action Items
Some actions.
`

			content2 := `* Project Beta :personal:
:PROPERTIES:
:ID:       {{.betaID}}
:END:
Second project description.

* Shopping List
:PROPERTIES:
:ID:       {{.shoppingID}}
:END:
Items to buy.
`

			tc.GivenFile("workspace1.org", content1).
				GivenFile("workspace2.org", content2).
				GivenSaveFile("workspace1.org")

			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			// Test 1: Empty query returns all symbols
			When(t, tc, "requesting all workspace symbols", "workspace/symbol", protocol.WorkspaceSymbolParams{Query: ""}, func(t *testing.T, result []protocol.SymbolInformation) {
				Then("returns all indexed headings", t, func(t *testing.T) {
					testza.AssertGreaterOrEqual(t, len(result), 4, "Expected at least 4 indexed headings")

					// Verify specific headings exist
					foundNames := make(map[string]bool)
					for _, sym := range result {
						foundNames[sym.Name] = true
						if sym.Name == "Project Alpha" {
							testza.AssertEqual(t, protocol.SymbolKindNamespace, sym.Kind, "Workspace symbols should be Interface kind")
						}
					}

					testza.AssertTrue(t, foundNames["Project Alpha"], "Expected to find 'Project Alpha'")
					testza.AssertTrue(t, foundNames["Project Beta"], "Expected to find 'Project Beta'")
					testza.AssertTrue(t, foundNames["Meeting Notes"], "Expected to find 'Meeting Notes'")
					testza.AssertTrue(t, foundNames["Shopping List"], "Expected to find 'Shopping List'")
				})
			})

			// Test 2: Query filtering - search for "Project"
			When(t, tc, "searching workspace symbols with 'Project' query", "workspace/symbol", protocol.WorkspaceSymbolParams{Query: "Project"}, func(t *testing.T, result []protocol.SymbolInformation) {
				Then("returns only matching symbols", t, func(t *testing.T) {
					testza.AssertGreater(t, len(result), 0, "Expected filtered results for 'Project'")

					for _, sym := range result {
						testza.AssertContains(t, sym.Name, "Project", "All results should contain 'Project'")
					}
				})
			})

			// Test 3: Query filtering - case insensitive
			When(t, tc, "searching with case-insensitive query 'shopping'", "workspace/symbol", protocol.WorkspaceSymbolParams{Query: "shopping"}, func(t *testing.T, result []protocol.SymbolInformation) {
				Then("returns matching symbol", t, func(t *testing.T) {
					testza.AssertLen(t, result, 1, "Expected exactly 1 result for 'shopping'")
					testza.AssertEqual(t, "Shopping List", result[0].Name)
				})
			})
		},
	)
}
