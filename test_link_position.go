package main

import (
	"fmt"
	"strings"

	"github.com/alexispurslane/go-org/org"
)

func main() {
	// This is the actual content from the helix log
	content := `* Do we really need macros much?
:PROPERTIES:
:ID:       C964F932-BE50-4656-9303-A98E4898BC98
:END:

I've kind of fallen out of love with macros to a certain degree.

Now that we don't just have snippets, but also AI, [[id:B7271B11-8B27-4809-B52E-61C9127BBEC4][to help deal with boilerplate and refactoring]], to a certain degree, yeah --- but there's more to it than that.

Ultimately, it's because the vast majority --- like 99% --- of things you do with macros in Lisp, are just basic textual compression, basically. You get rid of boilerplate, but the resulting DSL is completely isomorphic to the structures and control flow of the underlying code.
`

	doc := org.New().Parse(strings.NewReader(content), "test.org")

	fmt.Println("=== Document Structure ===")
	printNodes(doc.Nodes, 0)

	// Test the specific position from the LSP request
	// LSP: line 7 (0-indexed), char 74 (0-indexed)
	// go-org: line 8 (1-indexed), col 75 (1-indexed)
	targetLine := 8
	targetCol := 75

	fmt.Printf("\n=== Looking for link at position Line %d, Col %d ===\n", targetLine, targetCol)
	found := findLinkAtPosition(doc, targetLine, targetCol)
	if found != nil {
		fmt.Printf("FOUND: RegularLink URL=%s, Protocol=%s\n", found.URL, found.Protocol)
		fmt.Printf("  Position: Line %d:%d - Line %d:%d\n",
			found.Pos.StartLine, found.Pos.StartColumn,
			found.Pos.EndLine, found.Pos.EndColumn)
	} else {
		fmt.Println("NOT FOUND: No RegularLink at this position")
	}
}

func printNodes(nodes []org.Node, indent int) {
	prefix := strings.Repeat("  ", indent)
	for _, node := range nodes {
		switch n := node.(type) {
		case org.Paragraph:
			fmt.Printf("%sParagraph (Pos: %d:%d-%d:%d)\n", prefix,
				n.Pos.StartLine, n.Pos.StartColumn, n.Pos.EndLine, n.Pos.EndColumn)
			printNodes(n.Children, indent+1)
		case org.RegularLink:
			fmt.Printf("%sRegularLink: URL=%s, Protocol=%s\n", prefix, n.URL, n.Protocol)
			fmt.Printf("%s  Pos: Line %d:%d - Line %d:%d\n", prefix,
				n.Pos.StartLine, n.Pos.StartColumn, n.Pos.EndLine, n.Pos.EndColumn)
		case org.Headline:
			fmt.Printf("%sHeadline: %s (Pos: %d:%d-%d:%d)\n", prefix, n.Title,
				n.Pos.StartLine, n.Pos.StartColumn, n.Pos.EndLine, n.Pos.EndColumn)
			printNodes(n.Children, indent+1)
		case org.Text:
			content := n.Content
			if len(content) > 40 {
				content = content[:40] + "..."
			}
			fmt.Printf("%sText: %q (Pos: %d:%d-%d:%d)\n", prefix, content,
				n.Pos.StartLine, n.Pos.StartColumn, n.Pos.EndLine, n.Pos.EndColumn)
		default:
			fmt.Printf("%s%T\n", prefix, node)
		}
	}
}

func findLinkAtPosition(doc *org.Document, targetLine, targetCol int) *org.RegularLink {
	var walk func(nodes []org.Node) *org.RegularLink
	walk = func(nodes []org.Node) *org.RegularLink {
		for _, node := range nodes {
			switch n := node.(type) {
			case org.RegularLink:
				fmt.Printf("Checking RegularLink at Line %d:%d - Line %d:%d (target: %d:%d)\n",
					n.Pos.StartLine, n.Pos.StartColumn, n.Pos.EndLine, n.Pos.EndColumn,
					targetLine, targetCol)
				if targetLine >= n.Pos.StartLine && targetLine <= n.Pos.EndLine &&
					targetCol >= n.Pos.StartColumn && targetCol <= n.Pos.EndColumn {
					return &n
				}
			case org.Paragraph:
				if result := walk(n.Children); result != nil {
					return result
				}
			case org.Headline:
				if result := walk(n.Children); result != nil {
					return result
				}
			}
		}
		return nil
	}
	return walk(doc.Nodes)
}
