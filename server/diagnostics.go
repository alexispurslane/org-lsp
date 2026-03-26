package server

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/alexispurslane/go-org/org"
	"github.com/alexispurslane/org-lsp/orgscanner"
	protocol "go.lsp.dev/protocol"
)

// PublishDiagnosticsForDocument validates links and publishes diagnostics to the client.
// Call this from DidOpen, DidChange, or DidSave.
func PublishDiagnosticsForDocument(state *State, uri protocol.DocumentURI, doc *org.Document) {
	if state == nil || state.Client == nil {
		slog.Debug("Skipping diagnostics - client not available")
		return
	}

	diagnostics := validateDocument(state, uri, doc)

	params := protocol.PublishDiagnosticsParams{
		URI:         uri,
		Diagnostics: diagnostics,
	}

	ctx := context.Background()
	if err := state.Client.PublishDiagnostics(ctx, &params); err != nil {
		slog.Error("Failed to publish diagnostics", "uri", uri, "error", err)
	} else {
		slog.Debug("Published diagnostics", "uri", uri, "count", len(diagnostics))
	}
}

func validateDocument(state *State, uri protocol.DocumentURI, doc *org.Document) []protocol.Diagnostic {
	var diagnostics []protocol.Diagnostic

	var walkNodes func(node org.Node)
	walkNodes = func(node org.Node) {
		if link, ok := node.(org.RegularLink); ok {
			if d := validateLink(state, uri, link); d != nil {
				diagnostics = append(diagnostics, *d)
			}
		}
		node.Range(func(n org.Node) bool {
			walkNodes(n)
			return true
		})
	}

	for _, node := range doc.Nodes {
		walkNodes(node)
	}

	return diagnostics
}

func validateLink(state *State, uri protocol.DocumentURI, link org.RegularLink) *protocol.Diagnostic {
	switch link.Protocol {
	case "file":
		return validateFileLink(uri, link)
	case "id":
		return validateIDLink(state, uri, link)
	}
	return nil
}

func validateFileLink(currentURI protocol.DocumentURI, link org.RegularLink) *protocol.Diagnostic {
	currentPath := uriToPath(string(currentURI))
	linkPath := strings.TrimPrefix(link.URL, "file:")

	if strings.HasPrefix(linkPath, "~") {
		if homeDir, err := os.UserHomeDir(); err == nil {
			linkPath = strings.Replace(linkPath, "~", homeDir, 1)
		}
	}
	linkPath = os.ExpandEnv(linkPath)

	if !filepath.IsAbs(linkPath) {
		currentDir := filepath.Dir(currentPath)
		linkPath = filepath.Join(currentDir, linkPath)
	}
	linkPath = filepath.Clean(linkPath)

	if _, err := os.Stat(linkPath); err != nil {
		severity := protocol.DiagnosticSeverityError
		msg := fmt.Sprintf("File not found: %s", filepath.Base(linkPath))
		if !os.IsNotExist(err) {
			severity = protocol.DiagnosticSeverityWarning
			msg = fmt.Sprintf("Cannot access file: %s", filepath.Base(linkPath))
		}
		return &protocol.Diagnostic{
			Range:    toProtocolRange(link.Pos),
			Severity: severity,
			Message:  msg,
			Source:   "org-lsp",
		}
	}
	return nil
}

func validateIDLink(state *State, currentURI protocol.DocumentURI, link org.RegularLink) *protocol.Diagnostic {
	if state == nil || state.Scanner == nil || state.Scanner.ProcessedFiles == nil {
		return &protocol.Diagnostic{
			Range:    toProtocolRange(link.Pos),
			Severity: protocol.DiagnosticSeverityWarning,
			Message:  "Scanner not initialized",
			Source:   "org-lsp",
		}
	}

	uuid := strings.TrimPrefix(link.URL, "id:")
	if uuid == "" {
		return &protocol.Diagnostic{
			Range:    toProtocolRange(link.Pos),
			Severity: protocol.DiagnosticSeverityWarning,
			Message:  "Empty ID link",
			Source:   "org-lsp",
		}
	}

	_, found := state.Scanner.ProcessedFiles.UuidIndex.Load(orgscanner.UUID(uuid))
	if !found {
		return &protocol.Diagnostic{
			Range:    toProtocolRange(link.Pos),
			Severity: protocol.DiagnosticSeverityError,
			Message:  fmt.Sprintf("ID not found: %s", uuid),
			Source:   "org-lsp",
		}
	}
	return nil
}
