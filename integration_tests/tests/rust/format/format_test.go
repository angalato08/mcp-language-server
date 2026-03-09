package format_test

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

// TestFormatFile tests the FormatFile tool with the Rust language server
func TestFormatFile(t *testing.T) {
	t.Run("FormatUnformattedFile", func(t *testing.T) {
		suite := internal.GetTestSuite(t)

		ctx, cancel := context.WithTimeout(suite.Context, 15*time.Second)
		defer cancel()

		filePath := filepath.Join(suite.WorkspaceDir, "src/unformatted.rs")
		err := suite.Client.OpenFile(ctx, filePath)
		if err != nil {
			t.Fatalf("Failed to open unformatted.rs: %v", err)
		}

		result, err := tools.FormatFile(ctx, suite.Client, filePath, 4, true)
		if err != nil {
			t.Fatalf("FormatFile failed: %v", err)
		}

		if !strings.Contains(result, "Formatted") {
			t.Errorf("Expected success message but got: %s", result)
		}

		if !strings.Contains(result, "edit(s)") {
			t.Errorf("Expected edits to be applied but got: %s", result)
		}

		common.SnapshotTest(t, "rust", "format", "unformatted", result)

		// Verify the file was actually formatted
		content, err := suite.ReadFile("src/unformatted.rs")
		if err != nil {
			t.Fatalf("Failed to read formatted file: %v", err)
		}

		// After rustfmt, excessive spacing should be removed
		if strings.Contains(content, "println!(  \"hello\"  )") {
			t.Errorf("Expected formatted file to remove excessive spaces")
		}

		common.SnapshotTest(t, "rust", "format", "unformatted-content", content)
	})

	t.Run("FormatAlreadyFormattedFile", func(t *testing.T) {
		suite := internal.GetTestSuite(t)

		ctx, cancel := context.WithTimeout(suite.Context, 15*time.Second)
		defer cancel()

		filePath := filepath.Join(suite.WorkspaceDir, "src/clean.rs")
		err := suite.Client.OpenFile(ctx, filePath)
		if err != nil {
			t.Fatalf("Failed to open clean.rs: %v", err)
		}

		result, err := tools.FormatFile(ctx, suite.Client, filePath, 4, true)
		if err != nil {
			t.Fatalf("FormatFile failed: %v", err)
		}

		if !strings.Contains(result, "already formatted") {
			t.Errorf("Expected 'already formatted' message but got: %s", result)
		}

		common.SnapshotTest(t, "rust", "format", "already-formatted", result)
	})
}
