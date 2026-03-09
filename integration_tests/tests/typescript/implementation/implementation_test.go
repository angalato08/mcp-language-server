package implementation_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/angalato08/mcp-language-server/integration_tests/tests/common"
	"github.com/angalato08/mcp-language-server/integration_tests/tests/typescript/internal"
	"github.com/angalato08/mcp-language-server/internal/tools"
)

// TestFindImplementation tests the FindImplementation tool with TypeScript interfaces
func TestFindImplementation(t *testing.T) {
	suite := internal.GetTestSuite(t)

	ctx, cancel := context.WithTimeout(suite.Context, 10*time.Second)
	defer cancel()

	tests := []struct {
		name         string
		symbolName   string
		expectedText string
		snapshotName string
	}{
		{
			name:         "SharedInterfaceByName",
			symbolName:   "SharedInterface",
			expectedText: "Implementation of",
			snapshotName: "shared-interface",
		},
		{
			name:         "NotFound",
			symbolName:   "NonExistentInterface",
			expectedText: "No implementations found",
			snapshotName: "not-found",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := tools.FindImplementation(ctx, suite.Client, tc.symbolName)
			if err != nil {
				t.Fatalf("FindImplementation failed: %v", err)
			}

			if !strings.Contains(result, tc.expectedText) {
				t.Errorf("Expected result to contain %q but got:\n%s", tc.expectedText, result)
			}

			common.SnapshotTest(t, "typescript", "implementation", tc.snapshotName, result)
		})
	}
}

// TestFindImplementationAtPosition tests implementation lookup at a specific position
func TestFindImplementationAtPosition(t *testing.T) {
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
			// SharedInterface is defined at line 9 of helper.ts
			name:         "InterfaceAtPosition",
			file:         "helper.ts",
			line:         9,
			column:       18,
			expectedText: "File:",
			snapshotName: "interface-at-position",
		},
		{
			// TestInterface is defined at line 8 of main.ts
			name:         "TestInterfaceAtPosition",
			file:         "main.ts",
			line:         8,
			column:       18,
			expectedText: "File:",
			snapshotName: "test-interface-at-position",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			filePath := filepath.Join(suite.WorkspaceDir, tc.file)
			err := suite.Client.OpenFile(ctx, filePath)
			if err != nil {
				t.Fatalf("Failed to open %s: %v", tc.file, err)
			}

			result, err := tools.FindImplementationAtPosition(ctx, suite.Client, filePath, tc.line, tc.column)
			if err != nil {
				t.Fatalf("FindImplementationAtPosition failed: %v", err)
			}

			if !strings.Contains(result, tc.expectedText) {
				t.Errorf("Expected result to contain %q but got:\n%s", tc.expectedText, result)
			}

			common.SnapshotTest(t, "typescript", "implementation", tc.snapshotName, result)
		})
	}
}
