// Package internal contains shared helpers for Python tests
package internal

import (
	"path/filepath"
	"testing"

	"github.com/angalato08/mcp-language-server/integration_tests/tests/common"
	"github.com/angalato08/mcp-language-server/internal/tools"
)

)

// GetTestSuite returns a test suite for Python language server tests
func GetTestSuite(t *testing.T) *common.TestSuite {
	// Configure Python LSP (pyright)
	repoRoot, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatalf("Failed to get repo root: %v", err)
	}

	config := common.LSPTestConfig{
		Name:             "python",
		Command:          "pyright-langserver",
		Args:             []string{"--stdio"},
		WorkspaceDir:     filepath.Join(repoRoot, "integration_tests/workspaces/python"),
		InitializeTimeMs: 2000, // 2 seconds
	}

	// Create a test suite
	suite := common.NewTestSuite(t, config)

	// Set up the suite
	err = suite.Setup()
	if err != nil {
		t.Fatalf("Failed to set up test suite: %v", err)
	}

	// Register cleanup
	t.Cleanup(func() {
		suite.Cleanup()
	})

	return suite
	// Register cleanup
	t.Cleanup(func() {
		suite.Cleanup()
	})

	// Set workspace root for tools (important for relative paths in references)
	tools.SetWorkspaceRoot(suite.WorkspaceDir)

	return suite
}

