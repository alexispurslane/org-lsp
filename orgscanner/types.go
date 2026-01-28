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

// ProcessedFiles holds the results of scanning and parsing org files.
type ProcessedFiles struct {
	Files     []FileInfo
	UuidIndex sync.Map // map[UUID]HeaderLocation
	TagMap    sync.Map // map[string][]FileInfo
}
