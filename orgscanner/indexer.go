// Package orgscanner provides core functionality for scanning, parsing,
// and extracting structured data from org-mode files.
package orgscanner

import (
	"log/slog"
	"slices"
	"sync"
	"time"
)

func NewOrgScanner(root string) *OrgScanner {
	return &OrgScanner{
		ProcessedFiles: &ProcessedFiles{
			Files:     make([]FileInfo, 0),
			UuidIndex: sync.Map{},
			TagMap:    sync.Map{},
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
	pathsToDelete := make(map[string]struct{})
	for _, msg := range messages {
		if msg.Action == ShouldDelete {
			pathsToDelete[msg.Info.Path] = struct{}{}
			// Cleanup UUIDs
			for uuid := range msg.Info.UUIDs {
				s.ProcessedFiles.UuidIndex.Delete(uuid)
			}
		}
	}

	s.ProcessedFiles.Files = slices.DeleteFunc(s.ProcessedFiles.Files, func(f FileInfo) bool {
		_, found := pathsToDelete[f.Path]
		return found
	})

	// Phase 3: Process all parses concurrently
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

			// (sync.Map is fine here as keys are unique per header)
			for uuid, info := range parsed.UUIDs {
				s.ProcessedFiles.UuidIndex.Store(uuid, HeaderLocation{
					FilePath: parsed.Path,
					Position: info.Position,
					Title:    info.Title,
				})
			}

			// Now we need to lock to update the tags and file list, which are not thread safe
			mu.Lock()
			defer mu.Unlock()

			// Update tag map
			for _, tag := range parsed.Tags {
				existing, _ := s.ProcessedFiles.TagMap.LoadOrStore(tag, []FileInfo{})

				if existingSlice, ok := existing.([]FileInfo); ok {
					// Check if file already in tag list
					found := false

					for i, fi := range existingSlice {
						if fi.Path == parsed.Path {
							existingSlice[i] = *parsed
							found = true
							break
						}
					}

					if !found {
						s.ProcessedFiles.TagMap.Store(tag, append(existingSlice, *parsed))
					} else {
						s.ProcessedFiles.TagMap.Store(tag, existingSlice)
					}
				}
			}

			// Update or Append to Files
			idx := slices.IndexFunc(s.ProcessedFiles.Files, func(f FileInfo) bool {
				return f.Path == parsed.Path
			})
			if idx >= 0 {
				s.ProcessedFiles.Files[idx] = *parsed
			} else {
				s.ProcessedFiles.Files = append(s.ProcessedFiles.Files, *parsed)
			}
		}(msg)
	}
	wg.Wait()
	s.LastScanTime = time.Now()

	slog.Info("Incremental scan complete",
		"messages_processed", len(messages),
		"files_total", len(s.ProcessedFiles.Files))

	return nil
}
