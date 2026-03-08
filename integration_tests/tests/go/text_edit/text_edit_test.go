package text_edit_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/angalato08/mcp-language-server/integration_tests/tests/common"
	"github.com/angalato08/mcp-language-server/integration_tests/tests/go/internal"
	"github.com/angalato08/mcp-language-server/internal/tools"
)

// TestApplyTextEdits tests the ApplyTextEdits tool with various edit scenarios
func TestApplyTextEdits(t *testing.T) {
	suite := internal.GetTestSuite(t)

	ctx, cancel := context.WithTimeout(suite.Context, 10*time.Second)
	defer cancel()

	// Create a test file with known content we can edit
	testFileName := "edit_test.go"
	testFilePath := filepath.Join(suite.WorkspaceDir, testFileName)

	initialContent := `package main

import "fmt"

// TestFunction is a function we will edit
func TestFunction() {
	fmt.Println("Hello, world!")
	fmt.Println("This is a test function")
	fmt.Println("With multiple lines")
}

// AnotherFunction is another function that will be edited
func AnotherFunction() {
	fmt.Println("This is another function")
	fmt.Println("That we can modify")
}
`

	// Write the test file using the suite's method to ensure proper handling
	err := suite.WriteFile(testFileName, initialContent)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	tests := []struct {
		name          string
		edits         []tools.TextEdit
		verifications []func(t *testing.T, content string)
	}{
		{
			name: "Replace single line",
			edits: []tools.TextEdit{
				{
					StartLine: 7,
					EndLine:   7,
					NewText:   `	fmt.Println("Modified line")`,
				},
			},
			verifications: []func(t *testing.T, content string){
				func(t *testing.T, content string) {
					if !strings.Contains(content, `fmt.Println("Modified line")`) {
						t.Errorf("Expected modified line not found in content")
					}
					if strings.Contains(content, `fmt.Println("Hello, world!")`) {
						t.Errorf("Original line should have been replaced")
					}
				},
			},
		},
		{
			name: "Replace multiple lines",
			edits: []tools.TextEdit{
				{
					StartLine: 6,
					EndLine:   9,
					NewText: `func TestFunction() {
		fmt.Println("This is a completely modified function")
		fmt.Println("With fewer lines")
	}`,
				},
			},
			verifications: []func(t *testing.T, content string){
				func(t *testing.T, content string) {
					if !strings.Contains(content, `fmt.Println("This is a completely modified function")`) {
						t.Errorf("Expected new function content not found")
					}
					if !strings.Contains(content, `fmt.Println("With fewer lines")`) {
						t.Errorf("Expected new function content not found")
					}
					if strings.Contains(content, `fmt.Println("With multiple lines")`) {
						t.Errorf("Original line should have been replaced")
					}
				},
			},
		},
		{
			name: "Insert at a line (by replacing it and including original content)",
			edits: []tools.TextEdit{
				{
					StartLine: 8,
					EndLine:   8,
					NewText: `	fmt.Println("This is a test function")
	fmt.Println("This is an inserted line")`,
				},
			},
			verifications: []func(t *testing.T, content string){
				func(t *testing.T, content string) {
					if !strings.Contains(content, `fmt.Println("This is an inserted line")`) {
						t.Errorf("Expected inserted line not found in content")
					}
					if !strings.Contains(content, `fmt.Println("This is a test function")`) {
						t.Errorf("Original line should still be present in the content")
					}
				},
			},
		},
		{
			name: "Delete line",
			edits: []tools.TextEdit{
				{
					StartLine: 8,
					EndLine:   8,
					NewText:   "",
				},
			},
			verifications: []func(t *testing.T, content string){
				func(t *testing.T, content string) {
					if count := strings.Count(content, `fmt.Println("This is a test function")`); count != 0 {
						t.Errorf("Expected line to be deleted, but found %d occurrences", count)
					}
				},
			},
		},
		{
			name: "Multiple edits in same file",
			edits: []tools.TextEdit{
				{
					StartLine: 7,
					EndLine:   7,
					NewText:   `	fmt.Println("First modification")`,
				},
				{
					StartLine: 14,
					EndLine:   14,
					NewText:   `	fmt.Println("Second modification")`,
				},
			},
			verifications: []func(t *testing.T, content string){
				func(t *testing.T, content string) {
					if !strings.Contains(content, `fmt.Println("First modification")`) {
						t.Errorf("First modification not found")
					}
					if !strings.Contains(content, `fmt.Println("Second modification")`) {
						t.Errorf("Second modification not found")
					}
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Reset the file before each test
			err := suite.WriteFile(testFileName, initialContent)
			if err != nil {
				t.Fatalf("Failed to reset test file: %v", err)
			}

			// Call the ApplyTextEdits tool with the non-URL file path
			result, err := tools.ApplyTextEdits(ctx, suite.Client, testFilePath, tc.edits)
			if err != nil {
				t.Fatalf("Failed to apply text edits: %v", err)
			}

			// Verify the result message
			if !strings.Contains(result, "Successfully applied text edits") {
				t.Errorf("Result does not contain success message: %s", result)
			}

			// Read the file content after edits
			content, err := suite.ReadFile(testFileName)
			if err != nil {
				t.Fatalf("Failed to read test file after edits: %v", err)
			}

			// Run all verification functions
			for _, verify := range tc.verifications {
				verify(t, content)
			}

			// Use snapshot testing to verify the exact result
			snapshotName := strings.ToLower(strings.ReplaceAll(tc.name, " ", "_"))
			common.SnapshotTest(t, "go", "text_edit", snapshotName, result)
		})
	}
}

// TestApplyTextEditsLSPRoundTrip verifies the LSP server is properly notified
// after edit_file modifies a file, by checking that diagnostics update.
func TestApplyTextEditsLSPRoundTrip(t *testing.T) {
	suite := internal.GetTestSuite(t)

	ctx, cancel := context.WithTimeout(suite.Context, 30*time.Second)
	defer cancel()

	// Create a clean Go file
	testFileName := "lsp_roundtrip_test.go"
	testFilePath := filepath.Join(suite.WorkspaceDir, testFileName)

	cleanContent := `package main

import "fmt"

func RoundTripFunc() string {
	return fmt.Sprintf("hello %s", "world")
}
`

	err := suite.WriteFile(testFileName, cleanContent)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Open the file in the LSP and wait for initial diagnostics
	err = suite.Client.OpenFile(ctx, testFilePath)
	if err != nil {
		t.Fatalf("Failed to open file: %v", err)
	}
	time.Sleep(2 * time.Second)

	// Verify the file is clean initially
	result, err := tools.GetDiagnosticsForFile(ctx, suite.Client, testFilePath, 2, true, -1)
	if err != nil {
		t.Fatalf("GetDiagnosticsForFile failed: %v", err)
	}
	if !strings.Contains(result, "No diagnostics found") {
		t.Fatalf("Expected clean file initially but got diagnostics: %s", result)
	}

	// Use edit_file to introduce a type error: change return type mismatch
	// Replace `return fmt.Sprintf("hello %s", "world")` with `return 42`
	editResult, err := tools.ApplyTextEdits(ctx, suite.Client, testFilePath, []tools.TextEdit{
		{
			StartLine: 6,
			EndLine:   6,
			NewText:   `	return 42`,
		},
	})
	if err != nil {
		t.Fatalf("ApplyTextEdits failed: %v", err)
	}
	if !strings.Contains(editResult, "Successfully applied text edits") {
		t.Fatalf("Unexpected edit result: %s", editResult)
	}

	// Wait for LSP to process the notification
	time.Sleep(3 * time.Second)

	// Check diagnostics — LSP should now report a type error
	result, err = tools.GetDiagnosticsForFile(ctx, suite.Client, testFilePath, 2, true, -1)
	if err != nil {
		t.Fatalf("GetDiagnosticsForFile failed after edit: %v", err)
	}

	if strings.Contains(result, "No diagnostics found") {
		t.Errorf("Expected diagnostics after introducing type error, but got none. LSP was not notified of the change.")
	}

	// Should mention the type mismatch (returning int instead of string)
	if !strings.Contains(result, "int") && !strings.Contains(result, "return") && !strings.Contains(result, "cannot") {
		t.Errorf("Expected type error diagnostic but got: %s", result)
	}

	fmt.Printf("LSP round-trip verified: %s\n", result)

	common.SnapshotTest(t, "go", "text_edit", "lsp_roundtrip_type_error", result)
}

// TestApplyTextEditsWithBorderCases tests edge cases for the ApplyTextEdits tool
func TestApplyTextEditsWithBorderCases(t *testing.T) {
	suite := internal.GetTestSuite(t)

	ctx, cancel := context.WithTimeout(suite.Context, 10*time.Second)
	defer cancel()

	// Create a test file with known content we can edit
	testFileName := "edge_case_test.go"
	testFilePath := filepath.Join(suite.WorkspaceDir, testFileName)

	initialContent := `package main

import "fmt"

// EmptyFunction is an empty function we will edit
func EmptyFunction() {
}

// SingleLineFunction is a single line function
func SingleLineFunction() { fmt.Println("Single line") }

// LastFunction is the last function in the file
func LastFunction() {
	fmt.Println("Last function")
}
`

	// Write the test file using the suite's method
	err := suite.WriteFile(testFileName, initialContent)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	tests := []struct {
		name          string
		edits         []tools.TextEdit
		verifications []func(t *testing.T, content string)
	}{
		{
			name: "Edit empty function",
			edits: []tools.TextEdit{
				{
					StartLine: 6,
					EndLine:   7,
					NewText: `func EmptyFunction() {
		fmt.Println("No longer empty")
	}`,
				},
			},
			verifications: []func(t *testing.T, content string){
				func(t *testing.T, content string) {
					if !strings.Contains(content, `fmt.Println("No longer empty")`) {
						t.Errorf("Expected new function content not found")
					}
				},
			},
		},
		{
			name: "Edit single line function",
			edits: []tools.TextEdit{
				{
					StartLine: 10,
					EndLine:   10,
					NewText: `func SingleLineFunction() { 
		fmt.Println("Now a multi-line function") 
	}`,
				},
			},
			verifications: []func(t *testing.T, content string){
				func(t *testing.T, content string) {
					if !strings.Contains(content, `fmt.Println("Now a multi-line function")`) {
						t.Errorf("Expected new function content not found")
					}
					if strings.Contains(content, `fmt.Println("Single line")`) {
						t.Errorf("Original function should have been replaced")
					}
				},
			},
		},
		{
			name: "Append to end of file",
			edits: []tools.TextEdit{
				{
					StartLine: 15, // Last line of the file (the closing brace of LastFunction)
					EndLine:   15,
					NewText: `}

// NewFunction is a new function at the end of the file
func NewFunction() {
	fmt.Println("This is a new function")
}`,
				},
			},
			verifications: []func(t *testing.T, content string){
				func(t *testing.T, content string) {
					if !strings.Contains(content, `NewFunction is a new function at the end of the file`) {
						t.Errorf("Expected new function comment not found")
					}
					if !strings.Contains(content, `fmt.Println("This is a new function")`) {
						t.Errorf("Expected new function content not found")
					}
					// Verify there's no syntax error with double closing braces
					if strings.Contains(content, "}}") {
						t.Errorf("Found syntax error with double closing braces at end of file")
					}
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Reset the file before each test
			err := suite.WriteFile(testFileName, initialContent)
			if err != nil {
				t.Fatalf("Failed to reset test file: %v", err)
			}

			// Call the ApplyTextEdits tool
			result, err := tools.ApplyTextEdits(ctx, suite.Client, testFilePath, tc.edits)
			if err != nil {
				t.Fatalf("Failed to apply text edits: %v", err)
			}

			// Verify the result message
			if !strings.Contains(result, "Successfully applied text edits") {
				t.Errorf("Result does not contain success message: %s", result)
			}

			// Read the file content after edits
			content, err := suite.ReadFile(testFileName)
			if err != nil {
				t.Fatalf("Failed to read test file after edits: %v", err)
			}

			// Run all verification functions
			for _, verify := range tc.verifications {
				verify(t, content)
			}

			// Use snapshot testing to verify the exact result
			snapshotName := strings.ToLower(strings.ReplaceAll(tc.name, " ", "_"))
			common.SnapshotTest(t, "go", "text_edit", snapshotName, result)
		})
	}
}
