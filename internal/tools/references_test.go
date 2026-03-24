package tools

import (
	"strings"
	"testing"

	"github.com/angalato08/mcp-language-server/internal/protocol"
)

func TestFormatReferencesShort(t *testing.T) {
	// Set workspace root for relative paths
	oldRoot := workspaceRoot
	workspaceRoot = "/workspace"
	defer func() { workspaceRoot = oldRoot }()

	t.Run("single file multiple refs", func(t *testing.T) {
		refs := []protocol.Location{
			{URI: "file:///workspace/src/handler.go", Range: protocol.Range{Start: protocol.Position{Line: 44}}},
			{URI: "file:///workspace/src/handler.go", Range: protocol.Range{Start: protocol.Position{Line: 88}}},
			{URI: "file:///workspace/src/handler.go", Range: protocol.Range{Start: protocol.Position{Line: 155}}},
		}
		result := formatReferencesShort(refs, 3, -1, 0)

		if !strings.Contains(result, "3 references in 1 files:") {
			t.Errorf("Expected header with count, got:\n%s", result)
		}
		if !strings.Contains(result, "src/handler.go: L45, L89, L156 (3 refs)") {
			t.Errorf("Expected line numbers, got:\n%s", result)
		}
		if strings.Contains(result, "Use offset=") {
			t.Errorf("Should not have pagination footer when all refs shown")
		}
	})

	t.Run("multi file", func(t *testing.T) {
		refs := []protocol.Location{
			{URI: "file:///workspace/src/handler.go", Range: protocol.Range{Start: protocol.Position{Line: 44}}},
			{URI: "file:///workspace/src/init.go", Range: protocol.Range{Start: protocol.Position{Line: 66}}},
			{URI: "file:///workspace/tests/handler_test.go", Range: protocol.Range{Start: protocol.Position{Line: 11}}},
			{URI: "file:///workspace/tests/handler_test.go", Range: protocol.Range{Start: protocol.Position{Line: 33}}},
		}
		result := formatReferencesShort(refs, 4, -1, 0)

		if !strings.Contains(result, "4 references in 3 files:") {
			t.Errorf("Expected 4 refs in 3 files, got:\n%s", result)
		}
		if !strings.Contains(result, "src/handler.go: L45 (1 refs)") {
			t.Errorf("Expected handler.go line, got:\n%s", result)
		}
		if !strings.Contains(result, "src/init.go: L67 (1 refs)") {
			t.Errorf("Expected init.go line, got:\n%s", result)
		}
		if !strings.Contains(result, "tests/handler_test.go: L12, L34 (2 refs)") {
			t.Errorf("Expected test file lines, got:\n%s", result)
		}
	})

	t.Run("pagination footer", func(t *testing.T) {
		refs := []protocol.Location{
			{URI: "file:///workspace/a.go", Range: protocol.Range{Start: protocol.Position{Line: 0}}},
			{URI: "file:///workspace/b.go", Range: protocol.Range{Start: protocol.Position{Line: 0}}},
		}
		// totalRefCount=10, limit=2, offset=0 → more to show
		result := formatReferencesShort(refs, 10, 2, 0)

		if !strings.Contains(result, "Use offset=2 to see more.") {
			t.Errorf("Expected pagination footer, got:\n%s", result)
		}
	})

	t.Run("no pagination when all shown", func(t *testing.T) {
		refs := []protocol.Location{
			{URI: "file:///workspace/a.go", Range: protocol.Range{Start: protocol.Position{Line: 0}}},
		}
		result := formatReferencesShort(refs, 1, 30, 0)

		if strings.Contains(result, "Use offset=") {
			t.Errorf("Should not have pagination footer, got:\n%s", result)
		}
	})

	t.Run("empty refs", func(t *testing.T) {
		result := formatReferencesShort(nil, 0, -1, 0)
		if !strings.Contains(result, "0 references in 0 files:") {
			t.Errorf("Expected empty output, got:\n%s", result)
		}
	})
}
