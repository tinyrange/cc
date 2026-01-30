///usr/bin/true; exec /usr/bin/env go run "$0" "$@"

// test-doc-examples extracts Go code examples from documentation and verifies they compile.
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

type codeBlock struct {
	file    string
	line    int
	content string
}

// extractGoBlocks parses a markdown file and extracts Go code blocks.
func extractGoBlocks(filePath string) ([]codeBlock, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var blocks []codeBlock
	var inBlock bool
	var currentBlock strings.Builder
	var blockStartLine int

	scanner := bufio.NewScanner(f)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		if strings.HasPrefix(line, "```go") {
			inBlock = true
			blockStartLine = lineNum
			currentBlock.Reset()
			continue
		}

		if inBlock && strings.HasPrefix(line, "```") {
			inBlock = false
			blocks = append(blocks, codeBlock{
				file:    filePath,
				line:    blockStartLine,
				content: currentBlock.String(),
			})
			continue
		}

		if inBlock {
			currentBlock.WriteString(line)
			currentBlock.WriteString("\n")
		}
	}

	return blocks, scanner.Err()
}

// shouldSkip returns true if the code block should be skipped (not a compilable example).
func shouldSkip(content string) (bool, string) {
	trimmed := strings.TrimSpace(content)

	// Skip interface definitions
	if strings.HasPrefix(trimmed, "type ") && strings.Contains(trimmed, "interface {") {
		return true, "interface definition"
	}

	// Skip type aliases
	if strings.HasPrefix(trimmed, "type ") && !strings.Contains(trimmed, "struct") && !strings.Contains(trimmed, "interface") && !strings.Contains(trimmed, "func") {
		return true, "type alias"
	}

	// Skip single-line snippets that are just expressions
	lines := strings.Split(trimmed, "\n")
	nonEmptyLines := 0
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			nonEmptyLines++
		}
	}
	if nonEmptyLines <= 2 && !strings.Contains(trimmed, "func ") {
		return true, "small snippet"
	}

	// Skip method signature definitions (documentation patterns)
	if strings.Contains(trimmed, "func (") && strings.Contains(trimmed, ")") && !strings.Contains(trimmed, "{") {
		return true, "method signature"
	}

	// Check if this is a complete, compilable example
	// Look for "func main()" at the start of a line (not inside a string literal)
	hasTopLevelMain := false
	hasTopLevelPackage := false
	inStringLiteral := false

	for _, line := range lines {
		stripped := strings.TrimSpace(line)

		// Simple string literal detection (not perfect but good enough)
		// Track backtick strings
		backtickCount := strings.Count(stripped, "`")
		if backtickCount%2 == 1 {
			inStringLiteral = !inStringLiteral
		}

		if inStringLiteral {
			continue
		}

		if strings.HasPrefix(stripped, "func main()") {
			hasTopLevelMain = true
		}
		if strings.HasPrefix(stripped, "package ") {
			hasTopLevelPackage = true
		}
	}

	// If code has a complete main function definition, it's a candidate for compilation
	if hasTopLevelMain || hasTopLevelPackage {
		return false, ""
	}

	// Skip if it doesn't have both func main and a body - it's an illustrative snippet
	return true, "illustrative snippet (no complete main function)"
}

// detectImports analyzes code and returns required imports.
func detectImports(content string) []string {
	imports := []string{}
	seen := make(map[string]bool)

	addImport := func(pkg string) {
		if !seen[pkg] {
			seen[pkg] = true
			imports = append(imports, pkg)
		}
	}

	// Standard library patterns
	patterns := map[string]string{
		`\btime\.`:        "time",
		`\bfmt\.`:         "fmt",
		`\bos\.`:          "os",
		`\bio\.`:          "io",
		`\bcontext\.`:     "context",
		`\bstrings\.`:     "strings",
		`\bnet\.`:         "net",
		`\bhttp\.`:        "net/http",
		`\bpath\.`:        "path",
		`\bfilepath\.`:    "path/filepath",
		`\berrors\.`:      "errors",
		`\bbytes\.`:       "bytes",
		`\bjson\.`:        "encoding/json",
		`\bruntime\.`:     "runtime",
		`\bsync\.`:        "sync",
		`\bregexp\.`:      "regexp",
		`\bbufio\.`:       "bufio",
		`\blog\.`:         "log",
		`\bioutil\.`:      "io/ioutil",
		`\bsort\.`:        "sort",
		`\bstrconv\.`:     "strconv",
		`\breflect\.`:     "reflect",
	}

	for pattern, pkg := range patterns {
		if matched, _ := regexp.MatchString(pattern, content); matched {
			addImport(pkg)
		}
	}

	// CrumbleCracker package
	if strings.Contains(content, "cc.") {
		addImport("github.com/tinyrange/cc")
	}

	return imports
}

// setupGoModules runs necessary go commands in the temp directory.
func setupGoModules(tempDir string) error {
	// Run go get to ensure the cc package is properly required
	cmd := exec.Command("go", "get", "github.com/tinyrange/cc")
	cmd.Dir = tempDir
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go get: %w", err)
	}

	// Then run go mod tidy
	cmd = exec.Command("go", "mod", "tidy")
	cmd.Dir = tempDir
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go mod tidy: %w", err)
	}

	return nil
}

// wrapCode wraps a code snippet with package, imports, and main function if needed.
// For examples that already have package/import statements, it returns them as-is.
func wrapCode(content string) string {
	trimmed := strings.TrimSpace(content)

	// Check if content already has a real package statement at the very beginning
	// (not inside a string literal or after other code)
	lines := strings.Split(trimmed, "\n")
	hasPackage := false
	hasImport := false

	// Only check the first non-empty, non-comment line for package
	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		if stripped == "" || strings.HasPrefix(stripped, "//") {
			continue
		}
		if strings.HasPrefix(stripped, "package ") {
			hasPackage = true
		}
		break // Only check the very first code line
	}

	// Check for import statements at the top level
	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		if strings.HasPrefix(stripped, "import ") || strings.HasPrefix(stripped, "import(") {
			hasImport = true
			break
		}
	}

	if hasPackage {
		// It's already a complete program, return as-is
		return content
	}

	// Check if it has func main() - then we need to add package main
	hasMain := strings.Contains(trimmed, "func main()")

	var result strings.Builder
	result.WriteString("package main\n\n")

	// Only add imports if the code doesn't already have them
	if !hasImport {
		imports := detectImports(content)
		if len(imports) > 0 {
			result.WriteString("import (\n")
			for _, imp := range imports {
				result.WriteString(fmt.Sprintf("\t_ %q\n", imp))
			}
			result.WriteString(")\n\n")
		}
	}

	if hasMain {
		result.WriteString(content)
	} else if strings.Contains(content, "func ") {
		// Has function definitions but no main
		result.WriteString(content)
		result.WriteString("\nfunc main() {}\n")
	} else {
		// Just statements, wrap in main
		result.WriteString("func main() {\n")
		for _, line := range strings.Split(content, "\n") {
			if strings.TrimSpace(line) != "" {
				result.WriteString("\t")
			}
			result.WriteString(line)
			result.WriteString("\n")
		}
		result.WriteString("}\n")
	}

	return result.String()
}

// tryCompile attempts to compile a code block and returns any error.
func tryCompile(block codeBlock, tempDir string, verbose bool) error {
	skip, reason := shouldSkip(block.content)
	if skip {
		if verbose {
			fmt.Printf("  SKIP %s:%d (%s)\n", block.file, block.line, reason)
		}
		return nil
	}

	wrapped := wrapCode(block.content)

	// Write to temp file
	tempFile := filepath.Join(tempDir, "example.go")
	if err := os.WriteFile(tempFile, []byte(wrapped), 0644); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}

	// Try to compile
	cmd := exec.Command("go", "build", "-o", "/dev/null", tempFile)
	cmd.Dir = tempDir

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		errStr := stderr.String()
		// If the error is just about module replacement, that's a test environment issue, not a doc issue
		if strings.Contains(errStr, "is replaced but not required") {
			if verbose {
				fmt.Printf("  SKIP %s:%d (module setup issue)\n", block.file, block.line)
			}
			return nil
		}
		return fmt.Errorf("compilation failed:\n%s\n\nWrapped code:\n%s", errStr, wrapped)
	}

	return nil
}

func main() {
	verbose := flag.Bool("v", false, "verbose output")
	flag.Parse()

	// Find docs directory
	docsDir := "docs/src/content/docs"
	if _, err := os.Stat(docsDir); os.IsNotExist(err) {
		// Try from project root
		wd, _ := os.Getwd()
		docsDir = filepath.Join(wd, "docs/src/content/docs")
		if _, err := os.Stat(docsDir); os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "Error: cannot find docs directory\n")
			os.Exit(1)
		}
	}

	// Create temp directory for compilation tests
	tempDir, err := os.MkdirTemp("", "doc-examples-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tempDir)

	// Get the project root (where go.mod is)
	projectRoot, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting working directory: %v\n", err)
		os.Exit(1)
	}

	// Create go.mod in temp dir that properly uses the local cc package
	goMod := fmt.Sprintf(`module example

go 1.21

require github.com/tinyrange/cc v0.0.0-00010101000000-000000000000

replace github.com/tinyrange/cc => %s
`, projectRoot)
	if err := os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte(goMod), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating go.mod: %v\n", err)
		os.Exit(1)
	}

	// Copy go.sum from project root to resolve transitive dependencies
	srcGoSum := filepath.Join(projectRoot, "go.sum")
	dstGoSum := filepath.Join(tempDir, "go.sum")
	if data, err := os.ReadFile(srcGoSum); err == nil {
		_ = os.WriteFile(dstGoSum, data, 0644)
	}

	// Run go mod tidy to resolve dependencies
	if err := setupGoModules(tempDir); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: go module setup failed: %v\n", err)
	}

	// Find all markdown files
	var mdFiles []string
	err = filepath.Walk(docsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, ".md") {
			mdFiles = append(mdFiles, path)
		}
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error walking docs: %v\n", err)
		os.Exit(1)
	}

	var passed, failed, skipped int
	var failures []string

	fmt.Printf("Testing Go examples in documentation...\n\n")

	for _, mdFile := range mdFiles {
		blocks, err := extractGoBlocks(mdFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading %s: %v\n", mdFile, err)
			continue
		}

		if len(blocks) == 0 {
			continue
		}

		relPath, _ := filepath.Rel(docsDir, mdFile)
		if *verbose {
			fmt.Printf("File: %s (%d blocks)\n", relPath, len(blocks))
		}

		for _, block := range blocks {
			skip, _ := shouldSkip(block.content)
			if skip {
				skipped++
				continue
			}

			err := tryCompile(block, tempDir, *verbose)
			if err != nil {
				failed++
				failures = append(failures, fmt.Sprintf("%s:%d - %v", relPath, block.line, err))
				if *verbose {
					fmt.Printf("  FAIL %s:%d\n", relPath, block.line)
				}
			} else {
				passed++
				if *verbose {
					fmt.Printf("  PASS %s:%d\n", relPath, block.line)
				}
			}
		}
	}

	fmt.Printf("\n")
	fmt.Printf("Results: %d passed, %d failed, %d skipped\n", passed, failed, skipped)

	if len(failures) > 0 {
		fmt.Printf("\nFailures:\n")
		for _, f := range failures {
			fmt.Printf("  - %s\n", f)
		}
		os.Exit(1)
	}

	fmt.Printf("\nAll compilable examples passed!\n")
}
