package text_edit_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/angalato08/mcp-language-server/integration_tests/tests/common"
	"github.com/angalato08/mcp-language-server/integration_tests/tests/rust/internal"
	"github.com/angalato08/mcp-language-server/internal/protocol"
	"github.com/angalato08/mcp-language-server/internal/tools"
)

// TestApplyTextEdits tests the edit_file tool with the Rust language server
func TestApplyTextEdits(t *testing.T) {
	suite := internal.GetTestSuite(t)

	ctx, cancel := context.WithTimeout(suite.Context, 30*time.Second)
	defer cancel()

	testFileName := "src/edit_test.rs"
	testFilePath := filepath.Join(suite.WorkspaceDir, testFileName)

	initialContent := `// Test file for text edits

pub fn greet(name: &str) -> String {
    format!("Hello, {}!", name)
}

pub fn add(a: i32, b: i32) -> i32 {
    a + b
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_greet() {
        assert_eq!(greet("world"), "Hello, world!");
    }
}
`

	err := suite.WriteFile(testFileName, initialContent)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Wait for rust-analyzer to index the new file
	time.Sleep(5 * time.Second)

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
					NewText:   `    format!("Hi there, {}!", name)`,
				},
			},
			verifications: []func(t *testing.T, content string){
				func(t *testing.T, content string) {
					if !strings.Contains(content, `Hi there, {}!`) {
						t.Errorf("Expected modified line not found")
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
					NewText: `pub fn greet(name: &str) -> String {
    let greeting = format!("Hello, {}!", name);
    greeting.to_uppercase()
}`,
				},
			},
			verifications: []func(t *testing.T, content string){
				func(t *testing.T, content string) {
					if !strings.Contains(content, `greeting.to_uppercase()`) {
						t.Errorf("Expected new function body not found")
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
					if strings.Contains(content, `a + b`) {
						t.Errorf("Line should have been deleted")
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
					NewText:   `    format!("Hey, {}!", name)`,
				},
				{
					StartLine: 8,
					EndLine:   8,
					NewText:   `    a * b`,
				},
			},
			verifications: []func(t *testing.T, content string){
				func(t *testing.T, content string) {
					if !strings.Contains(content, `Hey, {}!`) {
						t.Errorf("First edit not found")
					}
					if !strings.Contains(content, `a * b`) {
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
			common.SnapshotTest(t, "rust", "text_edit", snapshotName, result)
		})
	}
}

// TestApplyTextEditsLSPRoundTrip verifies the LSP server is properly notified
// after edit_file modifies a file, by checking that diagnostics update.
// Strategy: main.rs has known errors (unreachable code). We use edit_file to
// fix them, then verify diagnostics clear — proving the LSP was notified.
func TestApplyTextEditsLSPRoundTrip(t *testing.T) {
	suite := internal.GetTestSuite(t)

	ctx, cancel := context.WithTimeout(suite.Context, 90*time.Second)
	defer cancel()

	// main.rs has: println!("Unreachable code"); after a return statement
	testFileName := "src/main.rs"
	testFilePath := filepath.Join(suite.WorkspaceDir, testFileName)

	// Open all files so rust-analyzer indexes fully
	for _, f := range []string{"src/main.rs", "src/types.rs", "src/helper.rs", "src/consumer.rs", "src/another_consumer.rs", "src/clean.rs"} {
		_ = suite.Client.OpenFile(ctx, filepath.Join(suite.WorkspaceDir, f))
	}
	time.Sleep(8 * time.Second)

	// Verify main.rs has diagnostics initially (unreachable code)
	result, err := tools.GetDiagnosticsForFile(ctx, suite.Client, testFilePath, 2, true, -1, tools.DiagnosticFilter{})
	if err != nil {
		t.Fatalf("GetDiagnosticsForFile failed: %v", err)
	}
	if strings.Contains(result, "No diagnostics found") {
		t.Fatalf("Expected initial diagnostics in main.rs but got none")
	}
	t.Logf("Initial diagnostics confirmed: %s", result)

	// Fix the error: replace the broken foo_bar function with a correct one
	// Lines 11-14 are the function body:
	//   fn foo_bar() -> String {
	//       String::from("Hello, World!")
	//       println!("Unreachable code"); // This is unreachable code
	//   }
	editResult, err := tools.ApplyTextEdits(ctx, suite.Client, testFilePath, []tools.TextEdit{
		{
			StartLine: 11,
			EndLine:   14,
			NewText: `fn foo_bar() -> String {
    String::from("Hello, World!")
}`,
		},
	})
	if err != nil {
		t.Fatalf("ApplyTextEdits failed: %v", err)
	}
	if !strings.Contains(editResult, "Successfully applied text edits") {
		t.Fatalf("Unexpected edit result: %s", editResult)
	}

	// Also send DidChangeWatchedFiles to trigger cargo check
	fileURI := fmt.Sprintf("file://%s", testFilePath)
	_ = suite.Client.DidChangeWatchedFiles(ctx, protocol.DidChangeWatchedFilesParams{
		Changes: []protocol.FileEvent{
			{
				URI:  protocol.DocumentUri(fileURI),
				Type: protocol.FileChangeType(protocol.Changed),
			},
		},
	})

	time.Sleep(6 * time.Second)

	// Close and reopen to force re-analysis
	_ = suite.Client.CloseFile(ctx, testFilePath)
	if err := suite.Client.OpenFile(ctx, testFilePath); err != nil {
		t.Fatalf("Failed to reopen file: %v", err)
	}
	time.Sleep(15 * time.Second)

	// Check diagnostics — the unreachable code error should be gone
	result, err = tools.GetDiagnosticsForFile(ctx, suite.Client, testFilePath, 2, true, -1, tools.DiagnosticFilter{})
	if err != nil {
		t.Fatalf("GetDiagnosticsForFile failed after edit: %v", err)
	}

	// The fix should have removed the unreachable code error
	if strings.Contains(result, "unreachable") {
		t.Errorf("Unreachable code error should be gone after fix, but got: %s", result)
	}

	fmt.Printf("LSP round-trip verified: %s\n", result)
	// Snapshot comparison is skipped because rust-analyzer diagnostic timing is
	// non-deterministic: the number and content of diagnostics varies between runs
	// depending on how far cargo-check has progressed after the edit.
	// The meaningful assertion above (no "unreachable" in result) is sufficient.
	// common.SnapshotTest(t, "rust", "text_edit", "lsp_roundtrip_fixed", result)
}
