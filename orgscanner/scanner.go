// Package orgscanner provides core functionality for scanning, parsing,
// and extracting structured data from org-mode files.
package orgscanner

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Scan compares the filesystem against the current index and returns
// a list of file messages indicating what action to take for each file.
// It only returns files that need parsing or deletion - unchanged files are skipped.
func (s *OrgScanner) Scan() ([]FileMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.scanUnlocked()
}

// scanUnlocked is the internal scan implementation that assumes lock is held.
func (s *OrgScanner) scanUnlocked() ([]FileMessage, error) {
	// Get current files on disk
	diskFiles, err := scanFilesystem(s.Root)
	if err != nil {
		return nil, err
	}

	var messages []FileMessage

	// Build a lookup set of on-disk files
	currentFiles := make(map[string]FileInfo)
	for _, f := range diskFiles {
		currentFiles[f.Path] = f
	}

	// Build a lookup set for the files we've already processed
	existingFiles := make(map[string]FileInfo)
	for _, f := range s.ProcessedFiles.Files {
		existingFiles[f.Path] = f
	}

	// Check for deleted files
	for _, f := range s.ProcessedFiles.Files {
		if _, exists := currentFiles[f.Path]; !exists {
			messages = append(messages, FileMessage{
				Action: ShouldDelete,
				Info:   f,
			})
		}
	}

	// Check for new or modified files
	for _, file := range diskFiles {
		processedFile, ok := existingFiles[file.Path]
		needsParse := !ok || processedFile.ModTime.Before(file.ModTime)

		finalFile := file
		if ok {
			finalFile = processedFile
		}

		if needsParse {
			messages = append(messages, FileMessage{
				Action: ShouldParse,
				Info:   finalFile,
			})
		}
	}

	return messages, nil
}

// scanFilesystem is the internal implementation that walks the directory tree.
func scanFilesystem(root string) ([]FileInfo, error) {
	slog.Debug("Scanning directory for .org files", "root", root)
	var files []FileInfo

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		if !d.IsDir() && strings.HasSuffix(path, ".org") {
			info, err := d.Info()
			if err != nil {
				slog.Error("Error getting file info", "path", path, "error", err)
				return nil
			}

			relPath := strings.TrimPrefix(path, root+string(filepath.Separator))
			if relPath == path {
				relPath = strings.TrimPrefix(path, root)
			}

			files = append(files, FileInfo{
				Path:    relPath,
				ModTime: info.ModTime(),
			})
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	return files, nil
}
