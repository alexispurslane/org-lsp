// testcolor colorizes Go test output, highlighting PASS/FAIL and test paths.
// Usage: go test -v ./... | go run cmd/testcolor/main.go
package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
)

const (
	colorReset   = "\033[0m"
	colorRed     = "\033[31m"
	colorGreen   = "\033[32m"
	colorYellow  = "\033[33m"
	colorBlue    = "\033[34m"
	colorMagenta = "\033[35m"
	colorCyan    = "\033[36m"
	colorGray    = "\033[90m"
)

var (
	// Match test result lines like "--- PASS: TestName" or "--- FAIL: TestName"
	resultLineRegex = regexp.MustCompile(`^(---\s+)(PASS|FAIL)(\s*:\s*)(.+)$`)

	// Match summary lines like "PASS" or "FAIL" at the start
	summaryLineRegex = regexp.MustCompile(`^(PASS|FAIL)(\s+|$)`)

	// Match test names in run lines like "=== RUN   TestName"
	runLineRegex = regexp.MustCompile(`^(===\s+RUN\s+)(.+)$`)

	// Match ok/FAIL package lines
	packageLineRegex = regexp.MustCompile(`^(ok|FAIL)\s+(\S+)`)

	// Match test paths (chains of identifiers separated by slashes)
	testPathRegex = regexp.MustCompile(`\b([A-Z][a-zA-Z0-9_]*)(/[a-zA-Z0-9_]+)+\b`)
)

type tally struct {
	pass int
	fail int
}

func main() {
	t := &tally{}
	scanner := bufio.NewScanner(os.Stdin)

	for scanner.Scan() {
		line := scanner.Text()
		colorized := colorizeLine(line, t)
		fmt.Println(colorized)
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "%sError reading input: %v%s\n", colorRed, err, colorReset)
		os.Exit(1)
	}

	printSummary(t)
}

func colorizeLine(line string, t *tally) string {
	// Check for --- PASS/FAIL lines first (most specific)
	if matches := resultLineRegex.FindStringSubmatch(line); matches != nil {
		prefix := matches[1]
		result := matches[2]
		separator := matches[3]
		testName := matches[4]

		var resultColor string
		if result == "PASS" {
			resultColor = colorGreen
			t.pass++
		} else {
			resultColor = colorRed
			t.fail++
		}

		// Colorize any test paths within the test name
		colorizedTestName := colorizeTestPaths(testName)

		return fmt.Sprintf("%s%s%s%s%s%s%s",
			prefix, resultColor, result, colorReset, separator, colorizedTestName, colorReset)
	}

	// Check for summary PASS/FAIL at start of line
	if matches := summaryLineRegex.FindStringSubmatch(line); matches != nil {
		result := matches[1]
		rest := line[len(result):]

		var resultColor string
		if result == "PASS" {
			resultColor = colorGreen
			t.pass++
		} else {
			resultColor = colorRed
			t.fail++
		}

		return fmt.Sprintf("%s%s%s%s", resultColor, result, colorReset, colorizeTestPaths(rest))
	}

	// Check for === RUN lines
	if matches := runLineRegex.FindStringSubmatch(line); matches != nil {
		prefix := matches[1]
		testName := matches[2]
		return fmt.Sprintf("%s%s%s%s", colorGray, prefix, colorReset, colorizeTestPaths(testName))
	}

	// Check for ok/FAIL package lines
	if matches := packageLineRegex.FindStringSubmatch(line); matches != nil {
		result := matches[1]
		rest := line[len(result):]

		var resultColor string
		if result == "ok" {
			resultColor = colorGreen
		} else {
			resultColor = colorRed
		}

		return fmt.Sprintf("%s%s%s%s", resultColor, result, colorReset, colorizeTestPaths(rest))
	}

	// For other lines, just colorize test paths
	return colorizeTestPaths(line)
}

func colorizeTestPaths(line string) string {
	// Replace test paths with colorized versions
	return testPathRegex.ReplaceAllStringFunc(line, func(match string) string {
		parts := strings.Split(match, "/")
		var colorized []string

		for i, part := range parts {
			if i == 0 {
				// First part (test name) in cyan
				colorized = append(colorized, colorCyan+part+colorReset)
			} else if strings.HasPrefix(part, "given_") || strings.HasPrefix(part, "Given_") {
				colorized = append(colorized, colorBlue+part+colorReset)
			} else if strings.HasPrefix(part, "when_") || strings.HasPrefix(part, "When_") {
				colorized = append(colorized, colorMagenta+part+colorReset)
			} else if strings.HasPrefix(part, "then_") || strings.HasPrefix(part, "Then_") {
				colorized = append(colorized, colorYellow+part+colorReset)
			} else {
				colorized = append(colorized, colorGray+part+colorReset)
			}
		}

		return strings.Join(colorized, "/")
	})
}

func printSummary(t *tally) {
	fmt.Println()
	fmt.Println(strings.Repeat("─", 50))
	fmt.Println("SUMMARY")
	fmt.Println(strings.Repeat("─", 50))

	total := t.pass + t.fail

	if t.pass > 0 {
		fmt.Printf("%s✓ Passed: %d%s\n", colorGreen, t.pass, colorReset)
	}
	if t.fail > 0 {
		fmt.Printf("%s✗ Failed: %d%s\n", colorRed, t.fail, colorReset)
	}

	fmt.Printf("Total: %d\n", total)
	fmt.Println(strings.Repeat("─", 50))

	if t.fail > 0 {
		os.Exit(1)
	}
}
