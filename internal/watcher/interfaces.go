package watcher

import (
	"context"

	"github.com/angalato08/mcp-language-server/internal/protocol"
	"github.com/angalato08/mcp-language-server/internal/watcher/common"
)

// LSPClient defines the minimal interface needed by the watcher
type LSPClient interface {
	// IsFileOpen checks if a file is already open in the editor
	IsFileOpen(path string) bool

	// OpenFile opens a file in the editor
	OpenFile(ctx context.Context, path string) error

	// NotifyChange notifies the server of a file change
	NotifyChange(ctx context.Context, path string) error

	// DidChangeWatchedFiles sends watched file events to the server
	DidChangeWatchedFiles(ctx context.Context, params protocol.DidChangeWatchedFilesParams) error
}

// Re-export from common for compatibility
type WatcherConfig = common.WatcherConfig

// Re-export from common for compatibility
func DefaultWatcherConfig() *WatcherConfig {
	return common.DefaultWatcherConfig()
}
