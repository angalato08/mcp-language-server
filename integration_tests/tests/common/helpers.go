package common

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Logger is an interface for logging in tests
type Logger interface {
	Printf(format string, v ...any)
}

// Helper to copy directories recursively
func CopyDir(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	if err = os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err = CopyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err = CopyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}

	return nil
}

// Helper to copy a single file
func CopyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() {
		if err := srcFile.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to close source file: %v\n", err)
		}
	}()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		if err := dstFile.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to close destination file: %v\n", err)
		}
	}()

	if _, err = io.Copy(dstFile, srcFile); err != nil {
		return err
	}

	return os.Chmod(dst, srcInfo.Mode())
}

// CleanupTestSuites is a helper to clean up all test suites in a test
func CleanupTestSuites(suites ...*TestSuite) {
	for _, suite := range suites {
		if suite != nil {
			suite.Cleanup()
		}
	}
}

// normalizePaths replaces absolute paths in the result with placeholder paths for consistent snapshots
func normalizePaths(input string) string {
	// Simple approach: just replace any path segments that contain workspace/
	// Use the LAST occurrence of /workspace/ to avoid matching user home directory paths
	lines := strings.Split(input, "\n")
	for i, line := range lines {
		// Any line containing a path to a workspace file needs normalization
		if strings.Contains(line, "/workspace/") {
			// Extract everything after the LAST /workspace/
			idx := strings.LastIndex(line, "/workspace/")
			if idx >= 0 {
				lines[i] = "/TEST_OUTPUT/workspace/" + line[idx+len("/workspace/"):]
			}
		}
		// Some tests, e.g. clangd, may include fully qualified paths to the base /workspaces/ directory
		if strings.Contains(line, "/workspaces/") {
			// Extract everything after the LAST /workspaces/
			idx := strings.LastIndex(line, "/workspaces/")
			if idx >= 0 {
				lines[i] = "/TEST_OUTPUT/workspace/" + line[idx+len("/workspaces/"):]
			}
		}
	}

	return strings.Join(lines, "\n")
}

// Patterns that vary between LSP server versions and should be normalized
var lspNormalizers = []struct {
	re   *regexp.Regexp
	repl string
}{
	// gopls struct size annotations: "// size=56 (0x38)" or "// size=56 (0x38), class=64 (0x40)"
	{regexp.MustCompile(`// size=\d+ \(0x[0-9a-fA-F]+\)(?:, class=\d+ \(0x[0-9a-fA-F]+\))?`), "// size=<normalized>"},
	// Markdown code blocks (some LSPs wrap in ```lang ... ```, others don't)
	{regexp.MustCompile("(?s)```[a-zA-Z]*\\n(.*?)\\n```"), "$1"},
	// HTML entities sometimes used in LSP markdown (like &nbsp;)
	{regexp.MustCompile(`&nbsp;`), " "},
	// Separators (some use ---, some use horizontal rules)
	{regexp.MustCompile(`\n---\n`), "\n"},
}

// normalizeOutput applies all normalizations to make snapshots resilient to
// environment differences (paths) and LSP version differences (annotations).
func normalizeOutput(input string) string {
	result := normalizePaths(input)
	for _, n := range lspNormalizers {
		result = n.re.ReplaceAllString(result, n.repl)
	}

	// Normalize whitespace:
	// 1. Replace all windows line endings with unix
	result = strings.ReplaceAll(result, "\r\n", "\n")
	// 2. Trim trailing spaces on each line
	lines := strings.Split(result, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	// 3. Join and trim leading/trailing empty lines
	result = strings.Trim(strings.Join(lines, "\n"), "\n")

	return result
}

// FindRepoRoot locates the repository root by looking for specific indicators
// Exported so it can be used by other packages
func FindRepoRoot() (string, error) {
	// Start from the current directory and walk up until we find the main.go file
	// which is at the repository root
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		// Check if this is the repo root (has a go.mod file)
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			// Found the repo root
			return dir, nil
		}

		// Move up one directory
		parent := filepath.Dir(dir)
		if parent == dir {
			// We've reached the filesystem root without finding repo root
			return "", fmt.Errorf("repository root not found")
		}
		dir = parent
	}
}

// SnapshotTest compares the actual result against an expected result file
// If the file doesn't exist or UPDATE_SNAPSHOTS=true env var is set, it will update the snapshot
func SnapshotTest(t *testing.T, languageName, toolName, testName, actualResult string) {
	// Normalize output to avoid system-specific paths and LSP version differences in snapshots
	actualResult = normalizeOutput(actualResult)

	// Get the absolute path to the snapshots directory
	repoRoot, err := FindRepoRoot()
	if err != nil {
		t.Fatalf("Failed to find repo root: %v", err)
	}

	// Build path based on language/tool/testName hierarchy
	snapshotDir := filepath.Join(repoRoot, "integration_tests", "snapshots", languageName, toolName)
	if err := os.MkdirAll(snapshotDir, 0755); err != nil {
		t.Fatalf("Failed to create snapshots directory: %v", err)
	}

	snapshotFile := filepath.Join(snapshotDir, testName+".snap")

	// Use a package-level flag to control snapshot updates
	updateFlag := os.Getenv("UPDATE_SNAPSHOTS") == "true"

	// If snapshot doesn't exist or update flag is set, write the snapshot
	_, err = os.Stat(snapshotFile)
	if os.IsNotExist(err) || updateFlag {
		if err := os.WriteFile(snapshotFile, []byte(actualResult), 0644); err != nil {
			t.Fatalf("Failed to write snapshot: %v", err)
		}
		if os.IsNotExist(err) {
			t.Logf("Created new snapshot: %s", snapshotFile)
		} else {
			t.Logf("Updated snapshot: %s", snapshotFile)
		}
		return
	}

	// Read the expected result
	expectedBytes, err := os.ReadFile(snapshotFile)
	if err != nil {
		t.Fatalf("Failed to read snapshot: %v", err)
	}
	expected := string(expectedBytes)

	// Compare the results
	if expected != actualResult {
		t.Errorf("Result doesn't match snapshot.\nExpected:\n%s\n\nActual:\n%s", expected, actualResult)

		// Create a diff file for debugging
		diffFile := snapshotFile + ".diff"
		diffContent := fmt.Sprintf("=== Expected ===\n%s\n\n=== Actual ===\n%s", expected, actualResult)
		if err := os.WriteFile(diffFile, []byte(diffContent), 0644); err != nil {
			t.Logf("Failed to write diff file: %v", err)
		} else {
			t.Logf("Wrote diff to: %s", diffFile)
		}
	}
}
