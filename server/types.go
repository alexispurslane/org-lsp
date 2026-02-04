package server

import (
	"github.com/alexispurslane/go-org/org"
	"github.com/alexispurslane/org-lsp/orgscanner"
	protocol "go.lsp.dev/protocol"
)

// LinkNode represents a link with its metadata
type LinkNode struct {
	Node     org.Node
	URL      string
	Protocol string
	Position org.Position
}

// CompletionContextType represents the type of completion context
type CompletionContextType string

const (
	ContextTypeNone   CompletionContextType = ""       // No completion context
	ContextTypeID     CompletionContextType = "id"     // ID link completion [[id:...]]
	ContextTypeTag    CompletionContextType = "tag"    // Tag completion in headlines
	ContextTypeFile   CompletionContextType = "file"   // File link completion [[file:...]]
	ContextTypeBlock  CompletionContextType = "block"  // Block type completion #+begin_
	ContextTypeExport CompletionContextType = "export" // Export block completion #+begin_export_
)

// CompletionContext holds detailed context for code completion
type CompletionContext struct {
	Type                CompletionContextType
	FilterPrefix        string // Text typed after the prefix for filtering
	NeedsClosingBracket bool   // True if trigger was "[[" and needs "]]" inserted
}

// State holds the global server state
type State struct {
	OrgScanRoot string
	Scanner     *orgscanner.OrgScanner
	OpenDocs    map[protocol.DocumentURI]*org.Document
	RawContent  map[protocol.DocumentURI]string
	DocVersions map[protocol.DocumentURI]int32
}
