package lsp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/isaacphi/mcp-language-server/internal/watcher/common"
)

// initializeTypescriptLanguageServer initializes the TypeScript language server
// with specific configurations and opens all TypeScript files in the workspace.
func initializeTypescriptLanguageServer(ctx context.Context, c *Client, workspaceDir string) error {
	lspLogger.Info("Initializing TypeScript language server with workspace: %s", workspaceDir)

	// Get default watcher config for exclusions
	config := common.DefaultWatcherConfig()

	// Initialize gitignore matcher if .gitignore exists
	var ignoreMatcher *common.GitignoreMatcher
	gitignorePath := filepath.Join(workspaceDir, ".gitignore")
	if _, err := os.Stat(gitignorePath); err == nil {
		ignoreMatcher, err = common.NewGitignoreMatcher(workspaceDir)
		if err != nil {
			lspLogger.Warn("Failed to initialize gitignore matcher: %v", err)
		} else {
			lspLogger.Info("Initialized gitignore matcher for %s", workspaceDir)
		}
	}

	lspLogger.Info("Opening all TypeScript files in workspace: %s", workspaceDir)
	fileCount := 0
	err := filepath.Walk(workspaceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Check if it's a directory
		if info.IsDir() {
			dirName := filepath.Base(path)
			// Skip excluded directories (like .git, node_modules)
			if config.ExcludedDirs[dirName] {
				return filepath.SkipDir
			}
			// Skip if ignored by gitignore
			if ignoreMatcher != nil && ignoreMatcher.IsIgnored(path) {
				return filepath.SkipDir
			}
			return nil
		}

		// Check if it's a TypeScript file
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".ts" || ext == ".tsx" {
			// Skip if ignored by gitignore
			if ignoreMatcher != nil && ignoreMatcher.IsIgnored(path) {
				return nil
			}

			// Skip large files
			if info.Size() > config.MaxFileSize {
				return nil
			}

			err := c.OpenFile(ctx, path)
			if err != nil {
				lspLogger.Error("Failed to open TypeScript file %s: %v", path, err)
				// Continue opening other files even if one fails
			}
			fileCount++
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("error walking workspace directory: %w", err)
	}

	lspLogger.Info("Opened %d TypeScript files", fileCount)
	return nil
}
