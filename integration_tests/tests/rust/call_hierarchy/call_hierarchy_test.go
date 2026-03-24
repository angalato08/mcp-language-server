package call_hierarchy_test

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

// TestIncomingCalls tests finding all callers of a function
func TestIncomingCalls(t *testing.T) {
	suite := internal.GetTestSuite(t)

	ctx, cancel := context.WithTimeout(suite.Context, 15*time.Second)
	defer cancel()

	tests := []struct {
		name         string
		file         string
		line         int
		column       int
		expectedText string
		snapshotName string
	}{
		{
			// middle_function is defined at line 7 of callchain.rs
			// Called by entry_point and another_caller
			name:         "MiddleFunctionCallers",
			file:         "src/callchain.rs",
			line:         7,
			column:       8,
			expectedText: "Incoming calls to",
			snapshotName: "middle-function-callers",
		},
		{
			// leaf_function is defined at line 3 of callchain.rs
			// Called by middle_function
			name:         "LeafFunctionCallers",
			file:         "src/callchain.rs",
			line:         3,
			column:       8,
			expectedText: "Incoming calls to",
			snapshotName: "leaf-function-callers",
		},
		{
			// entry_point is defined at line 12 of callchain.rs
			// Has no callers in this workspace
			name:         "EntryPointNoCallers",
			file:         "src/callchain.rs",
			line:         12,
			column:       8,
			expectedText: "No callers found",
			snapshotName: "entry-point-no-callers",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			filePath := filepath.Join(suite.WorkspaceDir, tc.file)
			err := suite.Client.OpenFile(ctx, filePath)
			if err != nil {
				t.Fatalf("Failed to open %s: %v", tc.file, err)
			}

			result, err := tools.GetIncomingCalls(ctx, suite.Client, filePath, tc.line, tc.column, "full")
			if err != nil {
				t.Fatalf("GetIncomingCalls failed: %v", err)
			}

			if !strings.Contains(result, tc.expectedText) {
				t.Errorf("Expected result to contain %q but got:\n%s", tc.expectedText, result)
			}

			common.SnapshotTest(t, "rust", "call_hierarchy", tc.snapshotName, result)
		})
	}
}

// TestOutgoingCalls tests finding all functions called by a function
func TestOutgoingCalls(t *testing.T) {
	suite := internal.GetTestSuite(t)

	ctx, cancel := context.WithTimeout(suite.Context, 15*time.Second)
	defer cancel()

	tests := []struct {
		name         string
		file         string
		line         int
		column       int
		expectedText string
		snapshotName string
	}{
		{
			// entry_point calls middle_function and println!
			name:         "EntryPointCallees",
			file:         "src/callchain.rs",
			line:         12,
			column:       8,
			expectedText: "Outgoing calls from",
			snapshotName: "entry-point-callees",
		},
		{
			// middle_function calls leaf_function and format!
			name:         "MiddleFunctionCallees",
			file:         "src/callchain.rs",
			line:         7,
			column:       8,
			expectedText: "Outgoing calls from",
			snapshotName: "middle-function-callees",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			filePath := filepath.Join(suite.WorkspaceDir, tc.file)
			err := suite.Client.OpenFile(ctx, filePath)
			if err != nil {
				t.Fatalf("Failed to open %s: %v", tc.file, err)
			}

			result, err := tools.GetOutgoingCalls(ctx, suite.Client, filePath, tc.line, tc.column, "full")
			if err != nil {
				t.Fatalf("GetOutgoingCalls failed: %v", err)
			}

			if !strings.Contains(result, tc.expectedText) {
				t.Errorf("Expected result to contain %q but got:\n%s", tc.expectedText, result)
			}

			common.SnapshotTest(t, "rust", "call_hierarchy", tc.snapshotName, result)
		})
	}
}
