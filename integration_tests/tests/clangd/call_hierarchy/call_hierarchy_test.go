package call_hierarchy_test

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
			// middleFunction is defined at line 8 of callchain.cpp
			// Called by entryPoint and anotherCaller
			name:         "MiddleFunctionCallers",
			file:         "src/callchain.cpp",
			line:         8,
			column:       13,
			expectedText: "Incoming calls to",
			snapshotName: "middle-function-callers",
		},
		{
			// leafFunction is defined at line 4 of callchain.cpp
			// Called by middleFunction
			name:         "LeafFunctionCallers",
			file:         "src/callchain.cpp",
			line:         4,
			column:       13,
			expectedText: "Incoming calls to",
			snapshotName: "leaf-function-callers",
		},
		{
			// entryPoint is defined at line 13 of callchain.cpp
			// Has no callers
			name:         "EntryPointNoCallers",
			file:         "src/callchain.cpp",
			line:         13,
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

			result, err := tools.GetIncomingCalls(ctx, suite.Client, filePath, tc.line, tc.column, "full")
			if err != nil {
				t.Fatalf("GetIncomingCalls failed: %v", err)
			}

			if !strings.Contains(result, tc.expectedText) {
				t.Errorf("Expected result to contain %q but got:\n%s", tc.expectedText, result)
			}

			common.SnapshotTest(t, "clangd", "call_hierarchy", tc.snapshotName, result)
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
			// entryPoint calls middleFunction and std::cout
			name:         "EntryPointCallees",
			file:         "src/callchain.cpp",
			line:         13,
			column:       6,
			expectedText: "Outgoing calls from",
			snapshotName: "entry-point-callees",
		},
		{
			// middleFunction calls leafFunction
			name:         "MiddleFunctionCallees",
			file:         "src/callchain.cpp",
			line:         8,
			column:       13,
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

			common.SnapshotTest(t, "clangd", "call_hierarchy", tc.snapshotName, result)
		})
	}
}
