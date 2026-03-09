package format_test

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

// TestFormatFile tests the FormatFile tool with clangd
func TestFormatFile(t *testing.T) {
	t.Run("FormatUnformattedFile", func(t *testing.T) {
		suite := internal.GetTestSuite(t)

		ctx, cancel := context.WithTimeout(suite.Context, 15*time.Second)
		defer cancel()

		filePath := filepath.Join(suite.WorkspaceDir, "src/unformatted.cpp")
		err := suite.Client.OpenFile(ctx, filePath)
		if err != nil {
			t.Fatalf("Failed to open unformatted.cpp: %v", err)
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

		common.SnapshotTest(t, "clangd", "format", "unformatted", result)

		// Verify the file was actually formatted
		content, err := suite.ReadFile("src/unformatted.cpp")
		if err != nil {
			t.Fatalf("Failed to read formatted file: %v", err)
		}

		// After clang-format, excessive spacing should be removed
		if strings.Contains(content, "std::cout<<\"hello\"<<std::endl") {
			t.Errorf("Expected formatted file to add proper spacing around operators")
		}

		common.SnapshotTest(t, "clangd", "format", "unformatted-content", content)
	})

	t.Run("FormatAlreadyFormattedFile", func(t *testing.T) {
		suite := internal.GetTestSuite(t)

		ctx, cancel := context.WithTimeout(suite.Context, 15*time.Second)
		defer cancel()

		// helper.cpp is already properly formatted
		filePath := filepath.Join(suite.WorkspaceDir, "src/helper.cpp")
		err := suite.Client.OpenFile(ctx, filePath)
		if err != nil {
			t.Fatalf("Failed to open helper.cpp: %v", err)
		}

		result, err := tools.FormatFile(ctx, suite.Client, filePath, 4, true)
		if err != nil {
			t.Fatalf("FormatFile failed: %v", err)
		}

		// Note: clangd may still apply formatting changes depending on the
		// clang-format config, so we just verify it doesn't error
		if strings.Contains(result, "failed") {
			t.Errorf("Format should not fail on a clean file: %s", result)
		}

		common.SnapshotTest(t, "clangd", "format", "already-formatted", result)
	})
}
