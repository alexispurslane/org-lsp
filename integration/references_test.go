package integration

import (
	"testing"

	"github.com/MarvinJWendt/testza"
	"go.lsp.dev/protocol"
)

func TestBacklinks(t *testing.T) {
	Given("a target file with UUID heading and multiple source files linking to it", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			tc.WithUUID("targetID")

			targetContent := `* Target Heading :tag:
:PROPERTIES:
:ID:       {{.targetID}}
:END:
This is the target file.`

			sourceContent1 := `* Source File 1
This file references the target: [[id:{{.targetID}}][target heading]]

** Subsection
Another reference [[id:{{.targetID}}]] here.`

			sourceContent2 := `* Source File 2
Different file with [[id:{{.targetID}}][another reference]].`

			tc.GivenFile("target.org", targetContent).
				GivenFile("source1.org", sourceContent1).
				GivenFile("source2.org", sourceContent2).
				GivenSaveFile("target.org").
				GivenSaveFile("source1.org").
				GivenSaveFile("source2.org").
				GivenOpenFile("target.org")

			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.ReferenceParams{
				TextDocumentPositionParams: protocol.TextDocumentPositionParams{
					TextDocument: protocol.TextDocumentIdentifier{
						URI: protocol.DocumentURI(tc.rootURI + "/target.org"),
					},
					Position: protocol.Position{Line: 0, Character: 5},
				},
				Context: protocol.ReferenceContext{
					IncludeDeclaration: false,
				},
			}

			When(t, tc, "requesting references from target heading", "textDocument/references", params, func(t *testing.T, result []protocol.Location) {
				Then("returns 3 references from 2 source files", t, func(t *testing.T) {
					testza.AssertLen(t, result, 3, "Expected 3 references (2 in source1, 1 in source2)")

					// Verify references come from expected files
					sourceURIs := make(map[string]bool)
					for _, loc := range result {
						sourceURIs[string(loc.URI)] = true
					}

					testza.AssertLen(t, sourceURIs, 2, "Expected references from 2 distinct files")
					testza.AssertTrue(t, sourceURIs[tc.rootURI+"/source1.org"], "Should have reference from source1.org")
					testza.AssertTrue(t, sourceURIs[tc.rootURI+"/source2.org"], "Should have reference from source2.org")
				})
			})
		},
	)
}

func TestEnhancedReferencesFromIDLink(t *testing.T) {
	Given("a target file with UUID and multiple source files with id links", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			tc.WithUUID("targetID")

			targetContent := `* Target Heading
:PROPERTIES:
:ID:       {{.targetID}}
:END:
This is the target.`

			sourceContent1 := `* Source File 1
This file has the [[id:{{.targetID}}][target link]] we'll query from.`

			sourceContent2 := `* Source File 2
Another reference to [[id:{{.targetID}}]].`

			tc.GivenFile("target.org", targetContent).
				GivenFile("source1.org", sourceContent1).
				GivenFile("source2.org", sourceContent2).
				GivenSaveFile("target.org").
				GivenSaveFile("source1.org").
				GivenSaveFile("source2.org").
				GivenOpenFile("source1.org")

			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.ReferenceParams{
				TextDocumentPositionParams: protocol.TextDocumentPositionParams{
					TextDocument: protocol.TextDocumentIdentifier{
						URI: protocol.DocumentURI(tc.rootURI + "/source1.org"),
					},
					Position: protocol.Position{Line: 1, Character: 25},
				},
				Context: protocol.ReferenceContext{
					IncludeDeclaration: false,
				},
			}

			When(t, tc, "requesting references from ID link itself", "textDocument/references", params, func(t *testing.T, result []protocol.Location) {
				Then("returns 2 references including the link being queried", t, func(t *testing.T) {
					testza.AssertLen(t, result, 2, "Expected 2 references (1 in source1, 1 in source2)")

					// Verify references are from expected files
					sourceURIs := make(map[string]bool)
					for _, loc := range result {
						sourceURIs[string(loc.URI)] = true
					}

					testza.AssertLen(t, sourceURIs, 2, "Expected references from 2 distinct files")
					testza.AssertTrue(t, sourceURIs[tc.rootURI+"/source1.org"], "Should include reference from source1.org (the link we're on)")
					testza.AssertTrue(t, sourceURIs[tc.rootURI+"/source2.org"], "Should include reference from source2.org")
				})
			})
		},
	)
}
