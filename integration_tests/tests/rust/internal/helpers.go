// Package internal contains shared helpers for Rust tests
package internal

import (
	"path/filepath"
	"testing"

	"github.com/angalato08/mcp-language-server/integration_tests/tests/common"
	"github.com/angalato08/mcp-language-server/internal/tools"
)

)

// GetTestSuite returns a test suite for Rust language server tests
func GetTestSuite(t *testing.T) *common.TestSuite {
	// Configure Rust LSP (rust-analyzer)
	repoRoot, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatalf("Failed to get repo root: %v", err)
	}

	config := common.LSPTestConfig{
		Name:             "rust",
		Command:          "rust-analyzer",
		Args:             []string{},
		WorkspaceDir:     filepath.Join(repoRoot, "integration_tests/workspaces/rust"),
		InitializeTimeMs: 3000,
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

