package format_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/angalato08/mcp-language-server/integration_tests/tests/common"
	"github.com/angalato08/mcp-language-server/integration_tests/tests/go/internal"
	"github.com/angalato08/mcp-language-server/internal/tools"
)

// TestFormatFile tests the FormatFile tool with the Go language server
func TestFormatFile(t *testing.T) {
	t.Run("FormatUnformattedFile", func(t *testing.T) {
		suite := internal.GetTestSuite(t)

		ctx, cancel := context.WithTimeout(suite.Context, 10*time.Second)
		defer cancel()

		filePath := filepath.Join(suite.WorkspaceDir, "unformatted.go")
		err := suite.Client.OpenFile(ctx, filePath)
		if err != nil {
			t.Fatalf("Failed to open unformatted.go: %v", err)
		}

		// Format the file using LSP (gopls wraps gofmt)
		result, err := tools.FormatFile(ctx, suite.Client, filePath, 4, false)
		if err != nil {
			t.Fatalf("FormatFile failed: %v", err)
		}

		if !strings.Contains(result, "Formatted") {
			t.Errorf("Expected success message but got: %s", result)
		}

		if !strings.Contains(result, "edit(s)") {
			t.Errorf("Expected edits to be applied but got: %s", result)
		}

		common.SnapshotTest(t, "go", "format", "unformatted", result)

		// Verify the file was actually formatted by reading it back
		content, err := suite.ReadFile("unformatted.go")
		if err != nil {
			t.Fatalf("Failed to read formatted file: %v", err)
		}

		// After gofmt, the file should have proper indentation with tabs
		if !strings.Contains(content, "\tfmt.Println") {
			t.Errorf("Expected formatted file to use tab indentation, got:\n%s", content)
		}

		common.SnapshotTest(t, "go", "format", "unformatted-content", content)
	})

	t.Run("FormatAlreadyFormattedFile", func(t *testing.T) {
		suite := internal.GetTestSuite(t)

		ctx, cancel := context.WithTimeout(suite.Context, 10*time.Second)
		defer cancel()

		filePath := filepath.Join(suite.WorkspaceDir, "clean.go")
		err := suite.Client.OpenFile(ctx, filePath)
		if err != nil {
			t.Fatalf("Failed to open clean.go: %v", err)
		}

		result, err := tools.FormatFile(ctx, suite.Client, filePath, 4, false)
		if err != nil {
			t.Fatalf("FormatFile failed: %v", err)
		}

		if !strings.Contains(result, "already formatted") {
			t.Errorf("Expected 'already formatted' message but got: %s", result)
		}

		common.SnapshotTest(t, "go", "format", "already-formatted", result)
	})
}
