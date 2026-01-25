package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/alexispurslane/org-lsp/orgscanner"
)

func main() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("Failed to get home directory: %v", err)
	}

	blogPath := filepath.Join(homeDir, "Sync/Alexis Files Private/Notes/blog")

	// Check if directory exists
	if _, err := os.Stat(blogPath); os.IsNotExist(err) {
		log.Fatalf("Directory does not exist: %s", blogPath)
	}

	fmt.Printf("Scanning org files in: %s\n\n", blogPath)

	processed, err := orgscanner.Process(blogPath)
	if err != nil {
		log.Fatalf("Failed to process org files: %v", err)
	}

	fmt.Printf("=== Summary ===\n")
	fmt.Printf("Total files processed: %d\n", len(processed.Files))

	// Count UUIDs
	uuidCount := 0
	processed.UuidMap.Range(func(key, value interface{}) bool {
		uuidCount++
		return true
	})
	fmt.Printf("Total UUIDs found: %d\n", uuidCount)

	// Count unique tags
	tagCount := 0
	processed.TagMap.Range(func(key, value interface{}) bool {
		tagCount++
		return true
	})
	fmt.Printf("Total unique tags: %d\n", tagCount)

	// Show first few file titles
	if len(processed.Files) > 0 {
		fmt.Printf("\n=== Sample Files ===\n")
		for i, file := range processed.Files {
			if i >= 5 {
				break
			}
			fmt.Printf("- %s\n", file.Path)
			fmt.Printf("  Title: \"%s\"\n", file.Title)
			if len(file.Tags) > 0 {
				fmt.Printf("  Tags: %v\n", file.Tags)
			}
			if len(file.UUIDs) > 0 {
				fmt.Printf("  UUIDs: %d found\n", len(file.UUIDs))
			}
			if file.Preview != "" {
				preview := file.Preview
				if len(preview) > 100 {
					preview = preview[:100] + "..."
				}
				fmt.Printf("  Preview: \"%s\"\n", preview)
			}
			fmt.Println()
		}
	}

	// Show some UUID to location mappings
	if uuidCount > 0 {
		fmt.Printf("=== Sample UUIDs ===\n")
		count := 0
		processed.UuidMap.Range(func(key, value interface{}) bool {
			if count >= 3 {
				return false
			}
			uuid := key.(orgscanner.UUID)
			loc := value.(orgscanner.HeaderLocation)
			fmt.Printf("- %s\n", uuid)
			fmt.Printf("  File: %s\n", loc.FilePath)
			fmt.Printf("  Header Index: %d\n", loc.HeaderIndex)
			fmt.Println()
			count++
			return true
		})
	}

	// Show some tags
	if tagCount > 0 {
		fmt.Printf("=== Tags Found ===\n")
		count := 0
		processed.TagMap.Range(func(key, value interface{}) bool {
			if count >= 10 {
				return false
			}
			tag := key.(string)
			files := value.([]orgscanner.FileInfo)
			fmt.Printf("- %s (%d files)\n", tag, len(files))
			count++
			return true
		})
	}
}
