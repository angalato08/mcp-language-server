package watcher

import (
	"github.com/angalato08/mcp-language-server/internal/watcher/common"
)

// GitignoreMatcher provides a simple wrapper around the go-gitignore package
type GitignoreMatcher = common.GitignoreMatcher

// NewGitignoreMatcher creates a new gitignore matcher for a workspace
func NewGitignoreMatcher(workspacePath string) (*GitignoreMatcher, error) {
	return common.NewGitignoreMatcher(workspacePath)
}
