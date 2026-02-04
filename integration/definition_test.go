package integration

import (
	"strings"
	"testing"

	"github.com/MarvinJWendt/testza"
	"go.lsp.dev/protocol"
)

func TestFileLinkDefinition(t *testing.T) {
	Given("a source file with a file link and existing target file", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			tc.GivenFile("target.org", "* Target Heading\nContent here").
				GivenFile("source.org", "* Source\nSee [[file:target.org][the target]]").
				GivenOpenFile("source.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.DefinitionParams{
				TextDocumentPositionParams: protocol.TextDocumentPositionParams{
					TextDocument: protocol.TextDocumentIdentifier{
						URI: tc.DocURI("source.org"),
					},
					Position: tc.PosAfter("source.org", "[[file:"),
				},
			}

			When(t, tc, "requesting definition at link position", "textDocument/definition", params, func(t *testing.T, locs []protocol.Location) {
				Then("returns target file location at first line", t, func(t *testing.T) {
					testza.AssertEqual(t, 1, len(locs), "Expected exactly one definition location")
					testza.AssertTrue(t, strings.HasSuffix(string(locs[0].URI), "target.org"), "Location should point to target.org")
					testza.AssertEqual(t, uint32(0), locs[0].Range.Start.Line, "Should point to line 0")
					testza.AssertEqual(t, uint32(0), locs[0].Range.Start.Character, "Should point to character 0")
				})
			})
		},
	)
}

func TestUUIDLinkDefinition(t *testing.T) {
	Given("a target file with UUID property and source file with id link", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			tc.WithUUID("targetID")

			targetContent := `Foo, bar, baz

* Target Heading
:PROPERTIES:
:ID:       {{.targetID}}
:END:
This is a target file with UUID.`

			sourceContent := "* Source File\nSee [[id:{{.targetID}}][the target]] for details."

			tc.GivenFile("target.org", targetContent).
				GivenFile("source.org", sourceContent).
				GivenSaveFile("target.org").
				GivenOpenFile("source.org")

			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.DefinitionParams{
				TextDocumentPositionParams: protocol.TextDocumentPositionParams{
					TextDocument: protocol.TextDocumentIdentifier{
						URI: tc.DocURI("source.org"),
					},
					Position: tc.PosAfter("source.org", "[[id:"),
				},
			}

			When(t, tc, "requesting definition at id link position", "textDocument/definition", params, func(t *testing.T, locs []protocol.Location) {
				Then("returns the heading location with matching UUID", t, func(t *testing.T) {
					testza.AssertEqual(t, 1, len(locs), "Expected exactly one definition location")
					testza.AssertTrue(t, strings.HasSuffix(string(locs[0].URI), "target.org"), "Location should point to target.org")
					testza.AssertEqual(t, uint32(2), locs[0].Range.Start.Line, "Should point to heading on line 2 (0-indexed)")
				})
			})
		},
	)
}
