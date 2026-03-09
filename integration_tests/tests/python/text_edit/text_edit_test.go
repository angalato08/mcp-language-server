package text_edit_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/angalato08/mcp-language-server/integration_tests/tests/common"
	"github.com/angalato08/mcp-language-server/integration_tests/tests/python/internal"
	"github.com/angalato08/mcp-language-server/internal/tools"
)

// TestApplyTextEdits tests the edit_file tool with the Python language server
func TestApplyTextEdits(t *testing.T) {
	suite := internal.GetTestSuite(t)

	ctx, cancel := context.WithTimeout(suite.Context, 15*time.Second)
	defer cancel()

	testFileName := "edit_test.py"
	testFilePath := filepath.Join(suite.WorkspaceDir, testFileName)

	initialContent := `"""Module for testing text edits."""


def greet(name: str) -> str:
    """Return a greeting message."""
    return f"Hello, {name}!"


def add(a: int, b: int) -> int:
    """Add two numbers."""
    return a + b
`

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
					StartLine: 6,
					EndLine:   6,
					NewText:   `    return f"Hi there, {name}!"`,
				},
			},
			verifications: []func(t *testing.T, content string){
				func(t *testing.T, content string) {
					if !strings.Contains(content, `Hi there, {name}!`) {
						t.Errorf("Expected modified line not found")
					}
					if strings.Contains(content, `Hello, {name}!`) {
						t.Errorf("Original line should have been replaced")
					}
				},
			},
		},
		{
			name: "Replace function body",
			edits: []tools.TextEdit{
				{
					StartLine: 4,
					EndLine:   6,
					NewText: `def greet(name: str) -> str:
    """Return a greeting message."""
    greeting = f"Hello, {name}!"
    return greeting.upper()`,
				},
			},
			verifications: []func(t *testing.T, content string){
				func(t *testing.T, content string) {
					if !strings.Contains(content, `return greeting.upper()`) {
						t.Errorf("Expected new function body not found")
					}
				},
			},
		},
		{
			name: "Delete line",
			edits: []tools.TextEdit{
				{
					StartLine: 5,
					EndLine:   5,
					NewText:   "",
				},
			},
			verifications: []func(t *testing.T, content string){
				func(t *testing.T, content string) {
					if strings.Contains(content, `Return a greeting message`) {
						t.Errorf("Docstring line should have been deleted")
					}
				},
			},
		},
		{
			name: "Multiple edits",
			edits: []tools.TextEdit{
				{
					StartLine: 6,
					EndLine:   6,
					NewText:   `    return f"Hey, {name}!"`,
				},
				{
					StartLine: 11,
					EndLine:   11,
					NewText:   `    return a * b`,
				},
			},
			verifications: []func(t *testing.T, content string){
				func(t *testing.T, content string) {
					if !strings.Contains(content, `Hey, {name}!`) {
						t.Errorf("First edit not found")
					}
					if !strings.Contains(content, `return a * b`) {
						t.Errorf("Second edit not found")
					}
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := suite.WriteFile(testFileName, initialContent)
			if err != nil {
				t.Fatalf("Failed to reset test file: %v", err)
			}

			result, err := tools.ApplyTextEdits(ctx, suite.Client, testFilePath, tc.edits)
			if err != nil {
				t.Fatalf("Failed to apply text edits: %v", err)
			}

			if !strings.Contains(result, "Successfully applied text edits") {
				t.Errorf("Result does not contain success message: %s", result)
			}

			content, err := suite.ReadFile(testFileName)
			if err != nil {
				t.Fatalf("Failed to read test file: %v", err)
			}

			for _, verify := range tc.verifications {
				verify(t, content)
			}

			snapshotName := strings.ToLower(strings.ReplaceAll(tc.name, " ", "_"))
			common.SnapshotTest(t, "python", "text_edit", snapshotName, result)
		})
	}
}

// TestApplyTextEditsLSPRoundTrip verifies the LSP server is properly notified
// after edit_file modifies a file, by checking that diagnostics update.
func TestApplyTextEditsLSPRoundTrip(t *testing.T) {
	suite := internal.GetTestSuite(t)

	ctx, cancel := context.WithTimeout(suite.Context, 30*time.Second)
	defer cancel()

	testFileName := "lsp_roundtrip_test.py"
	testFilePath := filepath.Join(suite.WorkspaceDir, testFileName)

	// Clean Python code with proper types
	cleanContent := `"""Module for LSP round-trip testing."""


def calculate(x: int, y: int) -> int:
    """Calculate sum of two integers."""
    return x + y
`

	err := suite.WriteFile(testFileName, cleanContent)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Open and wait for initial analysis
	err = suite.Client.OpenFile(ctx, testFilePath)
	if err != nil {
		t.Fatalf("Failed to open file: %v", err)
	}
	time.Sleep(3 * time.Second)

	// Verify clean initially
	result, err := tools.GetDiagnosticsForFile(ctx, suite.Client, testFilePath, 2, true, -1)
	if err != nil {
		t.Fatalf("GetDiagnosticsForFile failed: %v", err)
	}
	if !strings.Contains(result, "No diagnostics found") {
		t.Logf("Note: initial diagnostics (may be non-error): %s", result)
	}

	// Introduce a type error: return a string instead of int
	editResult, err := tools.ApplyTextEdits(ctx, suite.Client, testFilePath, []tools.TextEdit{
		{
			StartLine: 6,
			EndLine:   6,
			NewText:   `    return "not a number"`,
		},
	})
	if err != nil {
		t.Fatalf("ApplyTextEdits failed: %v", err)
	}
	if !strings.Contains(editResult, "Successfully applied text edits") {
		t.Fatalf("Unexpected edit result: %s", editResult)
	}

	// Wait for LSP to re-analyze
	time.Sleep(5 * time.Second)

	// Check diagnostics — pyright should report a type error
	result, err = tools.GetDiagnosticsForFile(ctx, suite.Client, testFilePath, 2, true, -1)
	if err != nil {
		t.Fatalf("GetDiagnosticsForFile failed after edit: %v", err)
	}

	if strings.Contains(result, "No diagnostics found") {
		t.Errorf("Expected diagnostics after introducing type error, but got none. LSP may not have been notified.")
	}

	fmt.Printf("LSP round-trip verified: %s\n", result)
	common.SnapshotTest(t, "python", "text_edit", "lsp_roundtrip_type_error", result)
}
