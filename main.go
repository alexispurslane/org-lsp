package main

import (
	"fmt"
	"strings"

	"github.com/niklasfasching/go-org/org"
)

func main() {
	// Test input with a headline and paragraph
	input := `This is a top-level paragraph.
It has multiple lines.

* A headline below`

	// Parse the document
	doc := org.New().Parse(strings.NewReader(input), "test.org")

	if doc.Error != nil {
		fmt.Printf("Parse error: %v\n", doc.Error)
		return
	}

	fmt.Printf("=== Position Tracking Test ===\n")
	fmt.Printf("Document has %d nodes\n", len(doc.Nodes))

	for _, node := range doc.Nodes {
		if headline, ok := node.(org.Headline); ok {
			fmt.Printf("\nHeadline found: %s\n", headline.String())
			fmt.Printf("Position: StartLine=%d, StartColumn=%d, EndLine=%d, EndColumn=%d\n",
				headline.Pos.StartLine, headline.Pos.StartColumn,
				headline.Pos.EndLine, headline.Pos.EndColumn)

			if headline.Pos.StartLine == 0 {
				fmt.Printf("❌ FAIL: Headline StartLine is 0 (not set)\n")
			} else {
				fmt.Printf("✅ PASS: Headline StartLine is %d\n", headline.Pos.StartLine)
			}
		}
		if paragraph, ok := node.(org.Paragraph); ok {
			fmt.Printf("\nParagraph found: %s\n", paragraph.String())
			fmt.Printf("Position: StartLine=%d, StartColumn=%d, EndLine=%d, EndColumn=%d\n",
				paragraph.Pos.StartLine, paragraph.Pos.StartColumn,
				paragraph.Pos.EndLine, paragraph.Pos.EndColumn)

			if paragraph.Pos.StartLine == 0 {
				fmt.Printf("❌ FAIL: Paragraph StartLine is 0 (not set)\n")
			} else {
				fmt.Printf("✅ PASS: Paragraph StartLine is %d\n", paragraph.Pos.StartLine)
			}

			if paragraph.Pos.EndLine == 0 {
				fmt.Printf("❌ FAIL: Paragraph EndLine is 0 (not set)\n")
			} else {
				fmt.Printf("✅ PASS: Paragraph EndLine is %d\n", paragraph.Pos.EndLine)
			}
		}
	}
}
