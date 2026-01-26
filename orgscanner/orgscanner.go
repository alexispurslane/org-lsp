// Package orgscanner provides core functionality for scanning, parsing,
// and extracting structured data from org-mode files.
package orgscanner

import (
	"log/slog"
	"sync"
	"sync/atomic"
)

// Process scans all .org files from the root directory, parses them in parallel,
// and returns a ProcessedFiles containing all metadata and lookup maps.
// This is the main entry point for the package - it orchestrates the entire
// scanning and parsing pipeline.
func Process(root string) (*ProcessedFiles, error) {
	files, err := Scan(root)
	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		slog.Debug("No .org files found", "root", root)
		return &ProcessedFiles{}, nil
	}

	procFiles := &ProcessedFiles{
		Files:     make([]FileInfo, 0, len(files)),
		UuidIndex: sync.Map{},
		TagMap:    sync.Map{},
	}

	var filesWithUUIDs int64
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, file := range files {
		wg.Add(1)
		go func(f FileInfo) {
			defer wg.Done()

			parsed, err := ParseFile(f.Path, root)
			if err != nil {
				slog.Error("Failed to parse file", "path", f.Path, "error", err)
				return
			}

			if parsed == nil {
				return
			}

			mu.Lock()
			procFiles.Files = append(procFiles.Files, *parsed)
			mu.Unlock()

			if len(parsed.UUIDs) > 0 {
				atomic.AddInt64(&filesWithUUIDs, 1)
			}

			// Populate UUID map
			for uuid, position := range parsed.UUIDs {
				procFiles.UuidIndex.Store(uuid, HeaderLocation{
					FilePath: parsed.Path,
					Position: position,
				})
			}

			// Populate Tag map
			for _, tag := range parsed.Tags {
				existing, _ := procFiles.TagMap.LoadOrStore(tag, []FileInfo{})
				if existingSlice, ok := existing.([]FileInfo); ok {
					procFiles.TagMap.Store(tag, append(existingSlice, *parsed))
				}
			}
		}(file)
	}

	wg.Wait()
	slog.Debug("Processing complete", "files_processed", len(procFiles.Files), "files_with_uuids", int(filesWithUUIDs))

	return procFiles, nil
}
