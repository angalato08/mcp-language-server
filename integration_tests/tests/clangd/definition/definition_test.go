package definition_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/angalato08/mcp-language-server/integration_tests/tests/clangd/internal"
	"github.com/angalato08/mcp-language-server/integration_tests/tests/common"
	"github.com/angalato08/mcp-language-server/internal/tools"
)

// TestReadDefinition tests the ReadDefinition tool with various C++ type definitions
func TestReadDefinition(t *testing.T) {
	// Helper function to open all files and wait for indexing
	openAllFilesAndWait := func(suite *common.TestSuite) {
		// Open one file so that clangd loads compiles commands and begins indexing
		filesToOpen := []string{
			"src/main.cpp",
		}

		for _, file := range filesToOpen {
			filePath := filepath.Join(suite.WorkspaceDir, file)
			// Use suite.Context (which doesn't have the short timeout) for setup
			err := suite.Client.OpenFile(suite.Context, filePath)
			if err != nil {
				// Don't fail the test, some files might not exist in certain tests
				t.Logf("Note: Failed to open %s: %v", file, err)
			}
		}
		// Wait for indexing to complete. clangd won't index files until they are opened.
		time.Sleep(5 * time.Second)
	}

	suite := internal.GetTestSuite(t)

	// Open all files and wait for clangd to index them
	openAllFilesAndWait(suite)

	tests := []struct {
		name         string
		symbolName   string
		expectedText string
		snapshotName string
	}{
		{
			name:         "Function",
			symbolName:   "foo_bar",
			expectedText: "void foo_bar()",
			snapshotName: "foobar",
		},
		{
			name:         "Class",
			symbolName:   "TestClass",
			expectedText: "class TestClass",
			snapshotName: "class",
		},
		{
			name:         "Method",
			symbolName:   "method",
			expectedText: "void method(int param)",
			snapshotName: "method",
		},
		{
			name:         "Struct",
			symbolName:   "TestStruct",
			expectedText: "TestStruct",
			snapshotName: "struct",
		},
		{
			name:         "Type",
			symbolName:   "TestType",
			expectedText: "TestType",
			snapshotName: "type",
		},
		{
			name:         "Constant",
			symbolName:   "TEST_CONSTANT",
			expectedText: "const int TEST_CONSTANT",
			snapshotName: "constant",
		},
		{
			name:         "Variable",
			symbolName:   "TEST_VARIABLE",
			expectedText: "int TEST_VARIABLE",
			snapshotName: "variable",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Create a fresh timeout for EACH tool call
			ctx, cancel := context.WithTimeout(suite.Context, 10*time.Second)
			defer cancel()

			// Call the ReadDefinition tool
			result, err := tools.ReadDefinition(ctx, suite.Client, tc.symbolName)
			if err != nil {
				t.Fatalf("Failed to read definition: %v", err)
			}

			// Check that the result contains relevant information
			if !strings.Contains(result, tc.expectedText) {
				t.Errorf("Definition does not contain expected text: %s", tc.expectedText)
			}

			// Use snapshot testing to verify exact output
			common.SnapshotTest(t, "clangd", "definition", tc.snapshotName, result)
		})
	}
}

func TestReadDefinitionInAnotherFile(t *testing.T) {
	// Helper function to open all files and wait for indexing
	openAllFilesAndWait := func(suite *common.TestSuite) {
		// Open all files to ensure clangd indexes everything
		filesToOpen := []string{
			"src/main.cpp",
		}

		for _, file := range filesToOpen {
			filePath := filepath.Join(suite.WorkspaceDir, file)
			err := suite.Client.OpenFile(suite.Context, filePath)
			if err != nil {
				// Don't fail the test, some files might not exist in certain tests
				t.Logf("Note: Failed to open %s: %v", file, err)
			}
		}
		time.Sleep(5 * time.Second)
	}

	suite := internal.GetTestSuite(t)

	// Open all files and wait for clangd to index them
	openAllFilesAndWait(suite)

	tests := []struct {
		name         string
		symbolName   string
		expectedText string
		snapshotName string
	}{
		{
			name:         "Function",
			symbolName:   "helperFunction",
			expectedText: "void helperFunction()",
			snapshotName: "helperFunction",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Create a fresh timeout for EACH tool call
			ctx, cancel := context.WithTimeout(suite.Context, 10*time.Second)
			defer cancel()

			// Call the ReadDefinition tool
			result, err := tools.ReadDefinition(ctx, suite.Client, tc.symbolName)
			if err != nil {
				t.Fatalf("Failed to read definition: %v", err)
			}

			// Check that the result contains relevant information
			if !strings.Contains(result, tc.expectedText) {
				t.Errorf("Definition does not contain expected text: %s", tc.expectedText)
			}

			// Use snapshot testing to verify exact output
			common.SnapshotTest(t, "clangd", "definition", tc.snapshotName, result)
		})
	}
}

func TestGetDefinitionAtPosition(t *testing.T) {
	suite := internal.GetTestSuite(t)

	// Explicitly open files to trigger indexing
	filesToOpen := []string{"src/main.cpp", "src/helper.cpp", "src/consumer.cpp"}
	for _, f := range filesToOpen {
		_ = suite.Client.OpenFile(suite.Context, filepath.Join(suite.WorkspaceDir, f))
	}

	// Wait for indexing
	time.Sleep(10 * time.Second)

	tests := []struct {
		name         string
		filePath     string
		line         int
		column       int
		expectedText string
	}{
		{
			name:         "Call to helperFunction",
			filePath:     "src/main.cpp",
			line:         11,
			column:       3,
			expectedText: "void helperFunction()",
		},
		{
			name:         "Call to foo_bar",
			filePath:     "src/main.cpp",
			line:         14,
			column:       3,
			expectedText: "void foo_bar()",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			absPath := filepath.Join(suite.WorkspaceDir, tc.filePath)
			ctx, cancel := context.WithTimeout(suite.Context, 10*time.Second)
			defer cancel()

			result, err := tools.ReadDefinitionAtPosition(ctx, suite.Client, absPath, tc.line, tc.column)
			if err != nil {
				t.Fatalf("Failed to get definition at position: %v", err)
			}

			if !strings.Contains(result, tc.expectedText) {
				t.Errorf("Definition does not contain expected text: %s\nResult: %s", tc.expectedText, result)
			}
		})
	}
}
