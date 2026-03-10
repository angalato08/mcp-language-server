package text_edit_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/angalato08/mcp-language-server/integration_tests/tests/common"
	"github.com/angalato08/mcp-language-server/integration_tests/tests/typescript/internal"
	"github.com/angalato08/mcp-language-server/internal/tools"
)

// TestApplyTextEdits tests the edit_file tool with the TypeScript language server
func TestApplyTextEdits(t *testing.T) {
	suite := internal.GetTestSuite(t)

	ctx, cancel := context.WithTimeout(suite.Context, 15*time.Second)
	defer cancel()

	testFileName := "edit_test.ts"
	testFilePath := filepath.Join(suite.WorkspaceDir, testFileName)

	initialContent := `// Test file for text edits

function greet(name: string): string {
  return "Hello, " + name + "!";
}

function add(a: number, b: number): number {
  return a + b;
}

const message: string = greet("world");
console.log(message);
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
					StartLine: 4,
					EndLine:   4,
					NewText:   `  return ` + "`Hello, ${name}!`" + `;`,
				},
			},
			verifications: []func(t *testing.T, content string){
				func(t *testing.T, content string) {
					if !strings.Contains(content, "${name}") {
						t.Errorf("Expected template literal not found")
					}
				},
			},
		},
		{
			name: "Replace function body",
			edits: []tools.TextEdit{
				{
					StartLine: 3,
					EndLine:   5,
					NewText: `function greet(name: string): string {
  const greeting = "Hello, " + name;
  return greeting + "!";
}`,
				},
			},
			verifications: []func(t *testing.T, content string){
				func(t *testing.T, content string) {
					if !strings.Contains(content, `const greeting = "Hello, " + name`) {
						t.Errorf("Expected new function body not found")
					}
				},
			},
		},
		{
			name: "Delete line",
			edits: []tools.TextEdit{
				{
					StartLine: 12,
					EndLine:   12,
					NewText:   "",
				},
			},
			verifications: []func(t *testing.T, content string){
				func(t *testing.T, content string) {
					if strings.Contains(content, `console.log(message)`) {
						t.Errorf("console.log line should have been deleted")
					}
				},
			},
		},
		{
			name: "Multiple edits",
			edits: []tools.TextEdit{
				{
					StartLine: 4,
					EndLine:   4,
					NewText:   `  return "Hi, " + name + "!";`,
				},
				{
					StartLine: 8,
					EndLine:   8,
					NewText:   `  return a * b;`,
				},
			},
			verifications: []func(t *testing.T, content string){
				func(t *testing.T, content string) {
					if !strings.Contains(content, `"Hi, "`) {
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
			common.SnapshotTest(t, "typescript", "text_edit", snapshotName, result)
		})
	}
}

// TestApplyTextEditsLSPRoundTrip verifies the LSP server is properly notified
// after edit_file modifies a file, by checking that diagnostics update.
func TestApplyTextEditsLSPRoundTrip(t *testing.T) {
	suite := internal.GetTestSuite(t)

	ctx, cancel := context.WithTimeout(suite.Context, 30*time.Second)
	defer cancel()

	testFileName := "lsp_roundtrip_test.ts"
	testFilePath := filepath.Join(suite.WorkspaceDir, testFileName)

	// Clean TypeScript code
	cleanContent := `// LSP round-trip test file

function calculate(x: number, y: number): number {
  return x + y;
}

const result: number = calculate(1, 2);
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
	result, err := tools.GetDiagnosticsForFile(ctx, suite.Client, testFilePath, 2, true, -1, tools.DiagnosticFilter{})
	if err != nil {
		t.Fatalf("GetDiagnosticsForFile failed: %v", err)
	}
	if !strings.Contains(result, "No diagnostics found") {
		t.Logf("Note: initial diagnostics: %s", result)
	}

	// Introduce a type error: return a string instead of number
	editResult, err := tools.ApplyTextEdits(ctx, suite.Client, testFilePath, []tools.TextEdit{
		{
			StartLine: 4,
			EndLine:   4,
			NewText:   `  return "not a number";`,
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

	// Check diagnostics — TypeScript should report a type error
	result, err = tools.GetDiagnosticsForFile(ctx, suite.Client, testFilePath, 2, true, -1, tools.DiagnosticFilter{})
	if err != nil {
		t.Fatalf("GetDiagnosticsForFile failed after edit: %v", err)
	}

	if strings.Contains(result, "No diagnostics found") {
		t.Errorf("Expected diagnostics after introducing type error, but got none. LSP may not have been notified.")
	}

	fmt.Printf("LSP round-trip verified: %s\n", result)
	common.SnapshotTest(t, "typescript", "text_edit", "lsp_roundtrip_type_error", result)
}
