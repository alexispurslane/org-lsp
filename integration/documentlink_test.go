package integration

import (
	"strings"
	"testing"

	"github.com/MarvinJWendt/testza"
	"go.lsp.dev/protocol"
)

func TestDocumentLinkFileLinks(t *testing.T) {
	Given("a file with file links", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			content := `* Notes
See [[file:target.org][the target file]] for more info.
Also check [[file:another.org]].
`

			tc.GivenFile("links.org", content).
				GivenOpenFile("links.org")

			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.DocumentLinkParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: tc.DocURI("links.org"),
				},
			}

			When(t, tc, "requesting document links", "textDocument/documentLink", params, func(t *testing.T, result []protocol.DocumentLink) {
				Then("returns links for all file references", t, func(t *testing.T) {
					testza.AssertGreaterOrEqual(t, len(result), 2, "Expected at least 2 document links")

					// Debug: log all found targets
					for i, link := range result {
						t.Logf("Link %d: Target=%q Tooltip=%q Range=%d:%d-%d:%d",
							i, string(link.Target), link.Tooltip,
							link.Range.Start.Line, link.Range.Start.Character,
							link.Range.End.Line, link.Range.End.Character)
					}

					// Find the link with description
					var foundTarget, foundAnother bool
					for _, link := range result {
						target := string(link.Target)
						// Links are now resolved to absolute file:// URIs
						if strings.Contains(target, "target.org") && strings.HasPrefix(target, "file://") {
							foundTarget = true
							testza.AssertEqual(t, "the target file", link.Tooltip)
						}
						if strings.Contains(target, "another.org") && strings.HasPrefix(target, "file://") {
							foundAnother = true
						}
					}

					testza.AssertTrue(t, foundTarget, "Expected link to target.org")
					testza.AssertTrue(t, foundAnother, "Expected link to another.org")
				})
			})
		},
	)
}

func TestDocumentLinkIDLinksUnresolved(t *testing.T) {
	Given("a file with id links to non-existent target", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			tc.WithUUID("targetID")

			content := `* Source
See [[id:{{.targetID}}][the target]] for details.
`

			tc.GivenFile("source.org", content).
				GivenOpenFile("source.org")

			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.DocumentLinkParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: tc.DocURI("source.org"),
				},
			}

			When(t, tc, "requesting document links", "textDocument/documentLink", params, func(t *testing.T, result []protocol.DocumentLink) {
				Then("returns unresolved id: links when target not found", t, func(t *testing.T) {
					testza.AssertLen(t, result, 1, "Expected 1 id link")

					// When target doesn't exist, falls back to id: scheme
					target := string(result[0].Target)
					testza.AssertContains(t, target, "id:", "Unresolved ID link should have id: prefix")
				})
			})
		},
	)
}

func TestDocumentLinkIDLinksResolved(t *testing.T) {
	Given("a file with id links to existing target", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			tc.WithUUID("targetID")

			// Create target file with UUID
			targetContent := `* Target Heading
:PROPERTIES:
:ID: {{.targetID}}
:END:
Content here.`

			sourceContent := `* Source
See [[id:{{.targetID}}][the target]] for details.
`

			tc.GivenFile("target.org", targetContent).
				GivenFile("source.org", sourceContent).
				GivenOpenFile("source.org").
				GivenSaveFile("target.org") // Index the target file

			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.DocumentLinkParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: tc.DocURI("source.org"),
				},
			}

			When(t, tc, "requesting document links", "textDocument/documentLink", params, func(t *testing.T, result []protocol.DocumentLink) {
				Then("resolves id links to file:// URIs when target exists", t, func(t *testing.T) {
					testza.AssertLen(t, result, 1, "Expected 1 id link")

					// When target exists, should resolve to file:// URI
					target := string(result[0].Target)
					testza.AssertContains(t, target, "file://", "Resolved ID link should be file:// URI")
					testza.AssertContains(t, target, "target.org", "Resolved link should point to target file")
				})
			})
		},
	)
}

func TestDocumentLinkHTTPLinks(t *testing.T) {
	Given("a file with http links", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			content := `* Web Links
Check out [[https://example.com][Example Site]].
Also see [[http://localhost:8080]] for local dev.
`

			tc.GivenFile("web.org", content).
				GivenOpenFile("web.org")

			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.DocumentLinkParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: tc.DocURI("web.org"),
				},
			}

			When(t, tc, "requesting document links", "textDocument/documentLink", params, func(t *testing.T, result []protocol.DocumentLink) {
				Then("returns links for all http/https references", t, func(t *testing.T) {
					testza.AssertGreaterOrEqual(t, len(result), 2, "Expected at least 2 web links")

					var foundHTTPS, foundHTTP bool
					for _, link := range result {
						target := string(link.Target)
						// HTTP/HTTPS links are returned as-is
						if target == "https://example.com" {
							foundHTTPS = true
						}
						if target == "http://localhost:8080" {
							foundHTTP = true
						}
					}

					testza.AssertTrue(t, foundHTTPS, "Expected https link")
					testza.AssertTrue(t, foundHTTP, "Expected http link")
				})
			})
		},
	)
}

func TestDocumentLinkNoLinks(t *testing.T) {
	Given("a file with no links", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			content := `* Just Text
This is just regular text.
No links here at all.
`

			tc.GivenFile("nolinks.org", content).
				GivenOpenFile("nolinks.org")

			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.DocumentLinkParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: tc.DocURI("nolinks.org"),
				},
			}

			When(t, tc, "requesting document links", "textDocument/documentLink", params, func(t *testing.T, result []protocol.DocumentLink) {
				Then("returns empty result", t, func(t *testing.T) {
					testza.AssertLen(t, result, 0, "Expected no links in document without links")
				})
			})
		},
	)
}

func TestDocumentLinkNestedInHeadings(t *testing.T) {
	Given("a file with links nested in headings and content", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			content := `* Main Heading
Content with [[file:doc1.org][link 1]] here.

** Subheading
More content with [[file:doc2.org]] link.
`

			tc.GivenFile("nested.org", content).
				GivenOpenFile("nested.org")

			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			params := protocol.DocumentLinkParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: tc.DocURI("nested.org"),
				},
			}

			When(t, tc, "requesting document links", "textDocument/documentLink", params, func(t *testing.T, result []protocol.DocumentLink) {
				Then("finds all links including nested ones", t, func(t *testing.T) {
					// Debug: log all found links
					for i, link := range result {
						t.Logf("Link %d: Target=%q Range=%d:%d-%d:%d",
							i, string(link.Target),
							link.Range.Start.Line, link.Range.Start.Character,
							link.Range.End.Line, link.Range.End.Character)
					}

					testza.AssertLen(t, result, 2, "Expected 2 links in nested structure")

					// Verify ranges are set correctly (links can be on same line or span multiple lines)
					for _, link := range result {
						testza.AssertGreaterOrEqual(t, link.Range.Start.Line, uint32(0))
						// End line should be >= start line
						testza.AssertGreaterOrEqual(t, link.Range.End.Line, link.Range.Start.Line)
						// If on same line, end character should be > start character
						if link.Range.End.Line == link.Range.Start.Line {
							testza.AssertGreater(t, link.Range.End.Character, link.Range.Start.Character)
						}
					}
				})
			})
		},
	)
}
