// Package orgscanner provides core functionality for scanning, parsing,
// and extracting structured data from org-mode files as part of
// github.com/alexispurslane/org-lsp.
package orgscanner

import (
	"sync"
	"time"

	"github.com/alexispurslane/go-org/org"
)

// HeaderLocation represents the position and title of a header containing a specific UUID.
type HeaderLocation struct {
	FilePath string
	Position org.Position
	Title    string
}

// UUID represents a globally unique org mode header identifier.
type UUID string

// HeaderIndex represents the index/position of a headline in an org-mode document.
// Deprecated: Use org.Position for precise line/column information.
type HeaderIndex int

// UUIDInfo holds both position and title for a UUID entry.
type UUIDInfo struct {
	Position org.Position
	Title    string
}

// FileUUIDPositions maps UUID strings to their info (position + title) within a file.
type FileUUIDPositions map[UUID]UUIDInfo

// FileInfo contains extracted metadata and content from a parsed org-mode file.
type FileInfo struct {
	Path      string
	ModTime   time.Time
	Preview   string
	Title     string
	Tags      []string
	UUIDs     FileUUIDPositions
	ParsedOrg *org.Document
}

// Equal compares two FileInfo values based on Path.
// This allows semantic equality checks even though FileInfo contains non-comparable fields.
func (f FileInfo) Equal(other FileInfo) bool {
	return f.Path == other.Path
}

// ProcessedFiles holds the results of scanning and parsing org files.
type ProcessedFiles struct {
	Files     map[string]*FileInfo       // path -> file info
	UuidIndex sync.Map                   // map[UUID]HeaderLocation
	TagMap    map[string]map[string]bool // tag -> set of file paths
}

// FileAction indicates what action should be taken for a file during scanning.
type FileAction int

const (
	// ShouldSkip indicates the file is unchanged and should not be processed.
	ShouldSkip FileAction = iota
	// ShouldParse indicates the file is new or modified and should be parsed.
	ShouldParse
	// ShouldDelete indicates the file no longer exists and should be removed from the index.
	ShouldDelete
)

// FileMessage represents a file operation to be performed during scanning.
type FileMessage struct {
	Action FileAction
	Info   *FileInfo
}

// OrgScanner provides incremental scanning capabilities for org-mode files.
// It maintains state between scans to avoid re-parsing unchanged files.
type OrgScanner struct {
	Root           string
	ProcessedFiles *ProcessedFiles
	LastScanTime   time.Time
	mu             sync.RWMutex
}
