package integration

import (
	"testing"

	"github.com/MarvinJWendt/testza"
	"go.lsp.dev/protocol"
)

func TestHoverFileLink(t *testing.T) {
	Given("a target file with content and source file with file link", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			targetContent := `* Target File
This is the target file with some content.
** Subheading
More content here.`

			sourceContent := "* Source File\nHover over [[file:target.org][this link]] to see preview."

			tc.GivenFile("target.org", targetContent).
				GivenFile("source.org", sourceContent).
				GivenOpenFile("source.org")

			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.HoverParams{
				TextDocumentPositionParams: protocol.TextDocumentPositionParams{
					TextDocument: protocol.TextDocumentIdentifier{
						URI: protocol.DocumentURI(tc.rootURI + "/source.org"),
					},
					Position: protocol.Position{Line: 1, Character: 15},
				},
			}

			When(t, tc, "requesting hover at file link position", "textDocument/hover", params, func(t *testing.T, result *protocol.Hover) {
				Then("returns hover with markdown content containing target preview", t, func(t *testing.T) {
					testza.AssertNotNil(t, result, "Expected hover result")

					markupContent := result.Contents
					testza.AssertEqual(t, protocol.Markdown, markupContent.Kind, "Expected markdown content")

					content := markupContent.Value
					testza.AssertContains(t, content, "FILE Link", "Expected 'FILE Link' in hover")
					testza.AssertContains(t, content, "target.org", "Expected target filename in hover")
					testza.AssertContains(t, content, "```org", "Expected org code block in hover")
					testza.AssertContains(t, content, "Target File", "Expected heading content in preview")
				})
			})
		},
	)
}

func TestHoverIDLink(t *testing.T) {
	Given("a target file with UUID heading and source file with id link", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			tc.WithUUID("targetID")

			targetContent := `Before heading

* UUID Target Heading :test:tag:
:PROPERTIES:
:ID:       {{.targetID}}
:END:
This heading has UUID property.
Some more content below.
** Subheading with details
Even more nested content here.`

			sourceContent := "* Source\nSee [[id:{{.targetID}}][UUID target]] for info."

			tc.GivenFile("target.org", targetContent).
				GivenFile("source.org", sourceContent).
				GivenSaveFile("target.org").
				GivenOpenFile("source.org")

			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.HoverParams{
				TextDocumentPositionParams: protocol.TextDocumentPositionParams{
					TextDocument: protocol.TextDocumentIdentifier{
						URI: protocol.DocumentURI(tc.rootURI + "/source.org"),
					},
					Position: protocol.Position{Line: 1, Character: 10},
				},
			}

			When(t, tc, "requesting hover at id link position", "textDocument/hover", params, func(t *testing.T, result *protocol.Hover) {
				Then("returns hover with markdown content containing heading preview", t, func(t *testing.T) {
					testza.AssertNotNil(t, result, "Expected hover result")

					markupContent := result.Contents

					content := markupContent.Value
					testza.AssertContains(t, content, "ID Link", "Expected 'ID Link' in hover")
					testza.AssertContains(t, content, "target.org", "Expected target filename in hover")
					testza.AssertContains(t, content, "```org", "Expected org code block in hover")
					testza.AssertContains(t, content, "UUID Target Heading", "Expected heading title in preview")
				})
			})
		},
	)
}

func TestHoverNoLink(t *testing.T) {
	Given("a file with regular text and no links", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			tc.GivenFile("no-link.org", "* Heading\nJust regular text without links.").
				GivenOpenFile("no-link.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.HoverParams{
				TextDocumentPositionParams: protocol.TextDocumentPositionParams{
					TextDocument: protocol.TextDocumentIdentifier{
						URI: protocol.DocumentURI(tc.rootURI + "/no-link.org"),
					},
					Position: protocol.Position{Line: 1, Character: 5},
				},
			}

			When(t, tc, "requesting hover on regular text", "textDocument/hover", params, func(t *testing.T, result *protocol.Hover) {
				Then("returns nil hover", t, func(t *testing.T) {
					testza.AssertNil(t, result, "Expected nil hover for non-link text")
				})
			})
		},
	)
}
