// Package orgscanner provides core functionality for scanning, parsing,
// and extracting structured data from org-mode files.
package orgscanner

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Scan walks the directory tree from root and collects all .org files.
// Returns a slice of FileInfo with relative paths and modification times.
func Scan(root string) ([]FileInfo, error) {
	slog.Debug("Scanning directory for .org files", "root", root)
	var files []FileInfo

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			// Continue scanning on individual file errors
			return nil
		}

		if !d.IsDir() && strings.HasSuffix(path, ".org") {
			info, err := d.Info()
			if err != nil {
				slog.Error("Error getting file info", "path", path, "error", err)
				return nil
			}

			// Get relative path from root
			relPath := strings.TrimPrefix(path, root+string(filepath.Separator))
			if relPath == path {
				relPath = strings.TrimPrefix(path, root)
			}

			slog.Debug("Found .org file", "path", relPath, "mod_time", info.ModTime())
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
