package call_hierarchy_test

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

// TestIncomingCalls tests finding all callers of a function
func TestIncomingCalls(t *testing.T) {
	suite := internal.GetTestSuite(t)

	ctx, cancel := context.WithTimeout(suite.Context, 10*time.Second)
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
			// MiddleFunction is defined at line 11 of callchain.go
			// It is called by EntryPoint and AnotherCaller
			name:         "MiddleFunctionCallers",
			file:         "callchain.go",
			line:         11,
			column:       6,
			expectedText: "Incoming calls to",
			snapshotName: "middle-function-callers",
		},
		{
			// LeafFunction is defined at line 6 of callchain.go
			// It is called by MiddleFunction
			name:         "LeafFunctionCallers",
			file:         "callchain.go",
			line:         6,
			column:       6,
			expectedText: "Incoming calls to",
			snapshotName: "leaf-function-callers",
		},
		{
			// EntryPoint is defined at line 17 of callchain.go
			// It has no callers in this workspace
			name:         "EntryPointNoCallers",
			file:         "callchain.go",
			line:         17,
			column:       6,
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

			common.SnapshotTest(t, "go", "call_hierarchy", tc.snapshotName, result)
		})
	}
}

// TestOutgoingCalls tests finding all functions called by a function
func TestOutgoingCalls(t *testing.T) {
	suite := internal.GetTestSuite(t)

	ctx, cancel := context.WithTimeout(suite.Context, 10*time.Second)
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
			// MiddleFunction calls LeafFunction and fmt.Sprintf
			name:         "MiddleFunctionCallees",
			file:         "callchain.go",
			line:         11,
			column:       6,
			expectedText: "Outgoing calls from",
			snapshotName: "middle-function-callees",
		},
		{
			// EntryPoint calls MiddleFunction and fmt.Println
			name:         "EntryPointCallees",
			file:         "callchain.go",
			line:         17,
			column:       6,
			expectedText: "Outgoing calls from",
			snapshotName: "entry-point-callees",
		},
		{
			// LeafFunction calls nothing interesting (just returns)
			name:         "LeafFunctionNoCallees",
			file:         "callchain.go",
			line:         6,
			column:       6,
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

			common.SnapshotTest(t, "go", "call_hierarchy", tc.snapshotName, result)
		})
	}
}
