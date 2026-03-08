package common

import (
	"os"
	"path/filepath"
	"time"

	gitignore "github.com/sabhiram/go-gitignore"
)

// WatcherConfig holds basic configuration for the watcher
type WatcherConfig struct {
	// DebounceTime is the duration to wait before sending file change events
	DebounceTime time.Duration

	// ExcludedDirs are directory names that should be excluded from watching
	ExcludedDirs map[string]bool

	// ExcludedFileExtensions are file extensions that should be excluded from watching
	ExcludedFileExtensions map[string]bool

	// LargeBinaryExtensions are file extensions for large binary files that shouldn't be opened
	LargeBinaryExtensions map[string]bool

	// MaxFileSize is the maximum size of a file to open
	MaxFileSize int64
}

// DefaultWatcherConfig returns a configuration with sensible defaults
func DefaultWatcherConfig() *WatcherConfig {
	return &WatcherConfig{
		DebounceTime: 300 * time.Millisecond,
		ExcludedDirs: map[string]bool{
			".git":         true,
			"node_modules": true,
			"dist":         true,
			"build":        true,
			"out":          true,
			"bin":          true,
			".idea":        true,
			".vscode":      true,
			".cache":       true,
			"coverage":     true,
			"target":       true, // Rust build output
			"vendor":       true, // Go vendor directory
		},
		ExcludedFileExtensions: map[string]bool{
			".swp":   true,
			".swo":   true,
			".tmp":   true,
			".temp":  true,
			".bak":   true,
			".log":   true,
			".o":     true, // Object files
			".so":    true, // Shared libraries
			".dylib": true, // macOS shared libraries
			".dll":   true, // Windows shared libraries
			".a":     true, // Static libraries
			".exe":   true, // Windows executables
			".lock":  true, // Lock files
		},
		LargeBinaryExtensions: map[string]bool{
			".png":  true,
			".jpg":  true,
			".jpeg": true,
			".gif":  true,
			".bmp":  true,
			".ico":  true,
			".zip":  true,
			".tar":  true,
			".gz":   true,
			".rar":  true,
			".7z":   true,
			".pdf":  true,
			".mp3":  true,
			".mp4":  true,
			".mov":  true,
			".wav":  true,
			".wasm": true,
		},
		MaxFileSize: 5 * 1024 * 1024, // 5MB
	}
}

// GitignoreMatcher provides a simple wrapper around the go-gitignore package
type GitignoreMatcher struct {
	Gitignore *gitignore.GitIgnore
	BasePath  string
}

// NewGitignoreMatcher creates a new gitignore matcher for a workspace
func NewGitignoreMatcher(workspacePath string) (*GitignoreMatcher, error) {
	gitignorePath := filepath.Join(workspacePath, ".gitignore")

	// Check if .gitignore exists
	_, err := os.Stat(gitignorePath)
	if os.IsNotExist(err) {
		// No .gitignore file, return a matcher with no patterns
		emptyIgnore := gitignore.CompileIgnoreLines([]string{}...)
		return &GitignoreMatcher{
			Gitignore: emptyIgnore,
			BasePath:  workspacePath,
		}, nil
	} else if err != nil {
		return nil, err
	}

	// Parse .gitignore file using the go-gitignore library
	ignore, err := gitignore.CompileIgnoreFile(gitignorePath)
	if err != nil {
		return nil, err
	}

	return &GitignoreMatcher{
		Gitignore: ignore,
		BasePath:  workspacePath,
	}, nil
}

// IsIgnored checks if a path should be ignored (matches GitignoreMatcher.ShouldIgnore logic)
func (g *GitignoreMatcher) IsIgnored(path string) bool {
	// Make path relative to workspace root
	relPath, err := filepath.Rel(g.BasePath, path)
	if err != nil {
		return false
	}

	// Use the go-gitignore Match function to check if the path should be ignored
	return g.Gitignore.MatchesPath(relPath)
}

// ShouldIgnore is an alias for IsIgnored for compatibility
func (g *GitignoreMatcher) ShouldIgnore(path string, isDir bool) bool {
	return g.IsIgnored(path)
}
