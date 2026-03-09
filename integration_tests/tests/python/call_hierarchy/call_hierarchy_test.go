package call_hierarchy_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/angalato08/mcp-language-server/integration_tests/tests/common"
	"github.com/angalato08/mcp-language-server/integration_tests/tests/python/internal"
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
			// middle_function is defined at line 9 of callchain.py
			// It is called by entry_point and another_caller
			name:         "MiddleFunctionCallers",
			file:         "callchain.py",
			line:         9,
			column:       5,
			expectedText: "Incoming calls to",
			snapshotName: "middle-function-callers",
		},
		{
			// leaf_function is defined at line 4 of callchain.py
			// It is called by middle_function
			name:         "LeafFunctionCallers",
			file:         "callchain.py",
			line:         4,
			column:       5,
			expectedText: "Incoming calls to",
			snapshotName: "leaf-function-callers",
		},
		{
			// entry_point is defined at line 15 of callchain.py
			// It has no callers in this workspace
			name:         "EntryPointNoCallers",
			file:         "callchain.py",
			line:         15,
			column:       5,
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

			result, err := tools.GetIncomingCalls(ctx, suite.Client, filePath, tc.line, tc.column)
			if err != nil {
				t.Fatalf("GetIncomingCalls failed: %v", err)
			}

			if !strings.Contains(result, tc.expectedText) {
				t.Errorf("Expected result to contain %q but got:\n%s", tc.expectedText, result)
			}

			common.SnapshotTest(t, "python", "call_hierarchy", tc.snapshotName, result)
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
			// entry_point calls middle_function and print
			name:         "EntryPointCallees",
			file:         "callchain.py",
			line:         15,
			column:       5,
			expectedText: "Outgoing calls from",
			snapshotName: "entry-point-callees",
		},
		{
			// middle_function calls leaf_function
			name:         "MiddleFunctionCallees",
			file:         "callchain.py",
			line:         9,
			column:       5,
			expectedText: "Outgoing calls from",
			snapshotName: "middle-function-callees",
		},
		{
			// leaf_function has no outgoing calls
			name:         "LeafFunctionNoCallees",
			file:         "callchain.py",
			line:         4,
			column:       5,
			expectedText: "No outgoing calls found",
			snapshotName: "leaf-function-no-callees",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			filePath := filepath.Join(suite.WorkspaceDir, tc.file)
			err := suite.Client.OpenFile(ctx, filePath)
			if err != nil {
				t.Fatalf("Failed to open %s: %v", tc.file, err)
			}

			result, err := tools.GetOutgoingCalls(ctx, suite.Client, filePath, tc.line, tc.column)
			if err != nil {
				t.Fatalf("GetOutgoingCalls failed: %v", err)
			}

			if !strings.Contains(result, tc.expectedText) {
				t.Errorf("Expected result to contain %q but got:\n%s", tc.expectedText, result)
			}

			common.SnapshotTest(t, "python", "call_hierarchy", tc.snapshotName, result)
		})
	}
}
