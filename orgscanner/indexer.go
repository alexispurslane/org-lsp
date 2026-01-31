// Package orgscanner provides core functionality for scanning, parsing,
// and extracting structured data from org-mode files.
package orgscanner

import (
	"log/slog"
	"sync"
	"time"
)

func NewOrgScanner(root string) *OrgScanner {
	return &OrgScanner{
		ProcessedFiles: &ProcessedFiles{
			Files:     make(map[string]*FileInfo),
			UuidIndex: sync.Map{},
			TagMap:    make(map[string]map[string]bool),
		},
		LastScanTime: time.Now(),
		Root:         root,
	}
}

// Process performs an incremental scan and processes all file messages.
// It executes the appropriate action (parse or delete) for each file.
func (s *OrgScanner) Process() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Get file messages (what action to take for each file)
	messages, err := s.scanUnlocked()

	if err != nil || len(messages) == 0 {
		slog.Debug("No file changes detected")
		s.LastScanTime = time.Now()
		return err
	}

	// Phase 1: Process all deletions
	for _, msg := range messages {
		if msg.Action != ShouldDelete {
			continue
		}
		path := msg.Info.Path

		// Cleanup UUIDs
		for uuid := range msg.Info.UUIDs {
			s.ProcessedFiles.UuidIndex.Delete(uuid)
		}

		// Cleanup TagMap - remove this file from all tag sets
		for _, tag := range msg.Info.Tags {
			if tagSet, ok := s.ProcessedFiles.TagMap[tag]; ok {
				delete(tagSet, path)
				// Clean up empty tag sets
				if len(tagSet) == 0 {
					delete(s.ProcessedFiles.TagMap, tag)
				}
			}
		}

		// Remove from Files map
		delete(s.ProcessedFiles.Files, path)
		slog.Debug("Removed file from index", "path", path)
	}

	// Phase 2: Process all parses concurrently
	var wg sync.WaitGroup
	var mu sync.Mutex // Protects Files and TagMap updates

	for _, msg := range messages {
		if msg.Action != ShouldParse {
			continue
		}

		wg.Add(1)
		go func(m FileMessage) {
			defer wg.Done()

			// Do what we can concurrently
			parsed, err := ParseFile(m.Info.Path, s.Root)
			if err != nil || parsed == nil {
				return
			}

			// Remove old UUIDs for this file if it exists (re-parsing case)
			if oldFile, exists := s.ProcessedFiles.Files[parsed.Path]; exists {
				for uuid := range oldFile.UUIDs {
					s.ProcessedFiles.UuidIndex.Delete(uuid)
				}
			}

			// Put the new UUIDs in
			for uuid, info := range parsed.UUIDs {
				s.ProcessedFiles.UuidIndex.Store(uuid, HeaderLocation{
					FilePath: parsed.Path,
					Position: info.Position,
					Title:    info.Title,
				})
			}

			// Now we need to lock to update the tags and file list
			mu.Lock()
			defer mu.Unlock()

			// Update tag map - add this file's path to each tag set
			for _, tag := range parsed.Tags {
				if s.ProcessedFiles.TagMap[tag] == nil {
					s.ProcessedFiles.TagMap[tag] = make(map[string]bool)
				}
				s.ProcessedFiles.TagMap[tag][parsed.Path] = true
			}

			// Store/Update in Files map (as pointer)
			s.ProcessedFiles.Files[parsed.Path] = parsed
		}(msg)
	}
	wg.Wait()
	s.LastScanTime = time.Now()

	slog.Info("Incremental scan complete",
		"messages_processed", len(messages),
		"files_total", len(s.ProcessedFiles.Files))

	return nil
}
