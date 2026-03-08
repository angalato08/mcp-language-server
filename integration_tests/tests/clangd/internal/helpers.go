// Package internal contains shared helpers for Clangd tests
package internal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/angalato08/mcp-language-server/integration_tests/tests/common"
	"github.com/angalato08/mcp-language-server/internal/tools"
)

// GetTestSuite returns a test suite for Clangd language server tests
func GetTestSuite(t *testing.T) *common.TestSuite {
	// Configure Clangd LSP
	repoRoot, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatalf("Failed to get repo root: %v", err)
	}

	workspaceTemplate := filepath.Join(repoRoot, "integration_tests/workspaces/clangd")

	// Pre-calculate workspace path to use in clangd args
	testName := strings.ReplaceAll(strings.ReplaceAll(t.Name(), "/", "_"), " ", "_")
	testOutputDir := filepath.Join(repoRoot, "integration_tests", "test-output")
	workspaceDir := filepath.Join(testOutputDir, "clangd", testName, "workspace")

	config := common.LSPTestConfig{
		Name:             "clangd",
		Command:          "clangd",
		Args:             []string{"--compile-commands-dir=" + workspaceDir},
		WorkspaceDir:     workspaceTemplate,
		InitializeTimeMs: 2000,
	}

	// Create a test suite
	suite := common.NewTestSuite(t, config)

	// Set up the suite (copies workspace and starts LSP)
	if err := suite.Setup(); err != nil {
		t.Fatalf("Failed to set up test suite: %v", err)
	}

	// Generate compile_commands.json from template in the temporary workspace
	templatePath := filepath.Join(suite.WorkspaceDir, "compile_commands.json.template")
	content, err := os.ReadFile(templatePath)
	if err == nil {
		// Replace WORKDIR placeholder with actual workspace path
		newContent := strings.ReplaceAll(string(content), "WORKDIR", suite.WorkspaceDir)
		targetPath := filepath.Join(suite.WorkspaceDir, "compile_commands.json")
		if err := os.WriteFile(targetPath, []byte(newContent), 0644); err != nil {
			t.Logf("Warning: Failed to write compile_commands.json: %v", err)
		} else {
			t.Logf("Generated compile_commands.json at %s", targetPath)
		}
	}

	// Wait a bit for clangd to see the new file and index
	time.Sleep(5 * time.Second)

	// Register cleanup
	t.Cleanup(func() {
		suite.Cleanup()
	})

	// Set workspace root for tools (important for relative paths in references)
	tools.SetWorkspaceRoot(suite.WorkspaceDir)

	return suite
}
