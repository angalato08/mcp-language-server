package references_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/angalato08/mcp-language-server/integration_tests/tests/common"
	"github.com/angalato08/mcp-language-server/integration_tests/tests/rust/internal"
	"github.com/angalato08/mcp-language-server/internal/tools"
)

// TestFindReferences tests the FindReferences tool with Rust symbols
// that have references across different files
func TestFindReferences(t *testing.T) {
	// Helper function to open all files and wait for indexing
	openAllFilesAndWait := func(suite *common.TestSuite) {
		// Open all files to ensure rust-analyzer indexes everything
		filesToOpen := []string{
			"src/main.rs",
			"src/types.rs",
			"src/helper.rs",
			"src/consumer.rs",
			"src/another_consumer.rs",
			"src/clean.rs",
		}

		for _, file := range filesToOpen {
			filePath := filepath.Join(suite.WorkspaceDir, file)
			err := suite.Client.OpenFile(suite.Context, filePath)
			if err != nil {
				// Don't fail the test, some files might not exist in certain tests
				t.Logf("Note: Failed to open %s: %v", file, err)
			}
		}
		// Wait for rust-analyzer to index
		time.Sleep(5 * time.Second)
	}

	suite := internal.GetTestSuite(t)

	// Open all files and wait for rust-analyzer to index them
	openAllFilesAndWait(suite)

	ctx, cancel := context.WithTimeout(suite.Context, 10*time.Second)
	defer cancel()

	tests := []struct {
		name          string
		symbolName    string
		expectedText  string
		expectedFiles int // Number of files where references should be found
		snapshotName  string
	}{
		{
			name:          "Function with references across files",
			symbolName:    "helper_function",
			expectedText:  "helper_function",
			expectedFiles: 2,
			snapshotName:  "helper-function",
		},
		{
			name:          "Function with reference in same file",
			symbolName:    "foo_bar",
			expectedText:  "main()",
			expectedFiles: 1, // main.rs
			snapshotName:  "foobar-function",
		},
		{
			name:          "Struct with references across files",
			symbolName:    "SharedStruct",
			expectedText:  "consumer_function",
			expectedFiles: 2, // consumer.rs and another_consumer.rs
			snapshotName:  "shared-struct",
		},
		{
			name:          "Method with references across files",
			symbolName:    "method",
			expectedText:  "method",
			expectedFiles: 1,
			snapshotName:  "struct-method",
		},
		{
			name:          "Interface with references across files",
			symbolName:    "SharedInterface",
			expectedText:  "iface",
			expectedFiles: 2, // consumer.rs and another_consumer.rs
			snapshotName:  "shared-interface",
		},
		{
			name:          "Interface method with references",
			symbolName:    "get_name",
			expectedText:  "get_name",
			expectedFiles: 2,
			snapshotName:  "interface-method",
		},
		{
			name:          "Constant with references across files",
			symbolName:    "SHARED_CONSTANT",
			expectedText:  "SHARED_CONSTANT",
			expectedFiles: 2,
			snapshotName:  "shared-constant",
		},
		{
			name:          "Type with references across files",
			symbolName:    "SharedType",
			expectedText:  "SharedType",
			expectedFiles: 2,
			snapshotName:  "shared-type",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Call the FindReferences tool
			result, err := tools.FindReferences(ctx, suite.Client, tc.symbolName, -1)
			if err != nil {
				t.Fatalf("Failed to find references: %v", err)
			}

			// Check that the result contains relevant information
			if !strings.Contains(result, tc.expectedText) {
				t.Errorf("References do not contain expected text: %s", tc.expectedText)
			}

			// Count how many different files are mentioned in the result
			fileCount := countFilesInResult(result)
			if fileCount < tc.expectedFiles {
				t.Errorf("Expected references in at least %d files, but found in %d files",
					tc.expectedFiles, fileCount)
			}

			// Use snapshot testing to verify exact output
			common.SnapshotTest(t, "rust", "references", tc.snapshotName, result)
		})
	}
}

// countFilesInResult counts the number of unique files mentioned in the result
func countFilesInResult(result string) int {
	fileMap := make(map[string]bool)

	// Any line ending with ".rs" is a file path
	for line := range strings.SplitSeq(result, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasSuffix(trimmed, ".rs") {
			fileMap[trimmed] = true
		}
	}

	return len(fileMap)
}
