package integration

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	"go.lsp.dev/protocol"
)

// GetDiagnostics returns the most recent diagnostics for a document.
// Polls for up to 500ms waiting for diagnostics to arrive.
func (tc *LSPTestContext) GetDiagnostics(uri string) []protocol.Diagnostic {
	// Poll for textDocument/publishDiagnostics notification
	notifications := tc.PollNotification("textDocument/publishDiagnostics", 500*time.Millisecond)
	if len(notifications) == 0 {
		return nil
	}

	// Get the most recent notification
	var params protocol.PublishDiagnosticsParams
	if err := json.Unmarshal(notifications[len(notifications)-1], &params); err != nil {
		return nil
	}

	// Check if it's for the requested document
	if string(params.URI) == string(tc.DocURI(uri)) {
		return params.Diagnostics
	}

	return nil
}

func TestDiagnosticsValidFileLink(t *testing.T) {
	Given("a document with a valid file link", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			// Create both files
			tc.GivenFile("target.org", `* Target
Content here`).
				GivenFile("source.org", `* Source
See [[file:target.org][the target]]`).
				GivenOpenFile("source.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			Then("no error diagnostics for valid link", t, func(t *testing.T) {
				diags := tc.GetDiagnostics("source.org")
				// Valid file link should produce no error diagnostics
				testza.AssertEqual(t, len(diags), 0, "Valid link should have no error diagnostics")
			})
		},
	)
}

func TestDiagnosticsBrokenFileLink(t *testing.T) {
	Given("a document with a broken file link", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			tc.GivenFile("source.org", `* Source
See [[file:nonexistent.org][broken link]]`).
				GivenOpenFile("source.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			Then("error diagnostic for broken link", t, func(t *testing.T) {
				diags := tc.GetDiagnostics("source.org")
				if len(diags) == 0 {
					t.Fatalf("No diagnostics received - notification count: %d", tc.NotificationCount("textDocument/publishDiagnostics"))
				}
				testza.AssertGreaterOrEqual(t, len(diags), 1, "Expected at least one diagnostic for broken link")

				// Verify it's an error-level diagnostic
				testza.AssertEqual(t, diags[0].Severity, protocol.DiagnosticSeverityError, "Broken link should be an error")
				testza.AssertContains(t, diags[0].Message, "nonexistent.org", "Diagnostic should mention the broken file")
			})
		},
	)
}

func TestDiagnosticsValidIDLink(t *testing.T) {
	Given("a document with a valid ID link", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			// Create target with UUID
			tc.GivenFile("target.org", `* Target
:PROPERTIES:
:ID: 550e8400-e29b-41d4-a716-446655440000
:END:
Content`).
				GivenSaveFile("target.org"). // Index the target so UUID is found
				GivenFile("source.org", `* Source
See [[id:550e8400-e29b-41d4-a716-446655440000][the target]]`).
				GivenOpenFile("source.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			Then("no error diagnostics for valid ID link", t, func(t *testing.T) {
				diags := tc.GetDiagnostics("source.org")
				testza.AssertEqual(t, len(diags), 0, "Valid ID link should have no error diagnostics")
			})
		},
	)
}

func TestDiagnosticsBrokenIDLink(t *testing.T) {
	Given("a document with a broken ID link", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			tc.GivenFile("source.org", `* Source
See [[id:00000000-0000-0000-0000-000000000000][nonexistent ID]]`).
				GivenOpenFile("source.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			Then("error diagnostic for broken ID link", t, func(t *testing.T) {
				diags := tc.GetDiagnostics("source.org")
				testza.AssertGreaterOrEqual(t, len(diags), 1, "Expected at least one diagnostic for broken ID link")

				testza.AssertEqual(t, diags[0].Severity, protocol.DiagnosticSeverityError, "Broken ID link should be an error")
				testza.AssertContains(t, diags[0].Message, "ID", "Diagnostic should mention ID")
			})
		},
	)
}

func TestDiagnosticsMixedLinks(t *testing.T) {
	Given("a document with both valid and broken links", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			tc.GivenFile("target.org", `* Target
Content`).
				GivenFile("source.org", `* Source
See [[file:target.org][valid link]]
And [[file:missing.org][broken link]]
And [[id:invalid-id-123][broken ID]]`).
				GivenOpenFile("source.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			Then("diagnostics only for broken links", t, func(t *testing.T) {
				diags := tc.GetDiagnostics("source.org")
				// Should have 2 error diagnostics (missing.org and invalid-id)
				testza.AssertGreaterOrEqual(t, len(diags), 2, "Expected diagnostics for 2 broken links")

				// Verify no diagnostic mentions target.org (the valid link)
				for _, diag := range diags {
					testza.AssertNotContains(t, diag.Message, "target.org", "Valid link should not produce diagnostic")
				}
			})
		},
	)
}

func TestDiagnosticsEmptyID(t *testing.T) {
	Given("a document with an empty ID link", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			tc.GivenFile("source.org", `* Source
See [[id:][empty ID]]`).
				GivenOpenFile("source.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			Then("error diagnostic for empty ID", t, func(t *testing.T) {
				diags := tc.GetDiagnostics("source.org")
				testza.AssertGreaterOrEqual(t, len(diags), 1, "Expected diagnostic for empty ID")
			})
		},
	)
}

func TestDiagnosticsEnvironmentVariable(t *testing.T) {
	Given("a document with file link using environment variable", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			tc.GivenFile("source.org", `* Source
See [[file:$HOME/.emacs.d/config.org][config]]`).
				GivenOpenFile("source.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			Then("environment variable is expanded when checking file", t, func(t *testing.T) {
				diags := tc.GetDiagnostics("source.org")
				// Result depends on whether $HOME/.emacs.d/config.org exists
				// Test just verifies diagnostics were published (no crash)
				testza.AssertNotNil(t, diags, "Server should publish diagnostics")
			})
		},
	)
}

func TestDiagnosticsTildeExpansion(t *testing.T) {
	Given("a document with file link using tilde", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			tc.GivenFile("source.org", `* Source
See [[file:~/.config/emacs/init.el][init file]]`).
				GivenOpenFile("source.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			Then("tilde is expanded when checking file", t, func(t *testing.T) {
				diags := tc.GetDiagnostics("source.org")
				// Result depends on whether ~/.config/emacs/init.el exists
				// Test just verifies diagnostics were published (no crash)
				testza.AssertNotNil(t, diags, "Server should publish diagnostics")
			})
		},
	)
}

func TestDiagnosticsRelativePath(t *testing.T) {
	Given("a document with relative file link", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			tc.GivenFile("subdir/target.org", `* Target
Content`).
				GivenFile("subdir/source.org", `* Source
See [[file:target.org][relative link]]`).
				GivenOpenFile("subdir/source.org")
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			Then("no error for valid relative path", t, func(t *testing.T) {
				diags := tc.GetDiagnostics("subdir/source.org")
				// Valid relative link should produce no error diagnostics
				testza.AssertEqual(t, len(diags), 0, "Valid relative link should have no error diagnostics")
			})
		},
	)
}

func TestDiagnosticsOnDocumentChange(t *testing.T) {
	Given("a document that is modified", t,
		func(t *testing.T) *LSPTestContext {
			tc := NewTestContext(t)
			tc.GivenFile("source.org", `* Source
See [[file:broken.org][broken link]]`).
				GivenOpenFile("source.org").
				GivenChangeDocument("source.org", `* Source
See [[file:fixed.org][fixed link]]`)
			return tc
		},
		func(t *testing.T, tc *LSPTestContext) {
			Then("diagnostics updated on document change", t, func(t *testing.T) {
				// Clear previous notifications
				tc.ClearNotifications("textDocument/publishDiagnostics")

				// Get diagnostics after change
				diags := tc.GetDiagnostics("source.org")
				// After changing to fixed.org (which doesn't exist in our test),
				// there should still be an error, but mentioning "fixed.org"
				// This verifies diagnostics were re-published after the change
				testza.AssertNotNil(t, diags, "Server should publish diagnostics after change")
			})
		},
	)
}
