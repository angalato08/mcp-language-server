package lsp

import (
	"context"
	"fmt"
	"sync"

	"github.com/angalato08/mcp-language-server/internal/protocol"
)

// languageGroups maps language IDs to their "primary" language config key.
// e.g., clangd handles both "c" and "cpp" — register config as "c",
// and this maps "cpp" → "c" so both route to the same client.
var languageGroups = map[string]string{
	"cpp":             "c",
	"typescriptreact": "typescript",
	"javascriptreact": "javascript",
}

// LanguageConfig describes how to start an LSP server for a language.
type LanguageConfig struct {
	LangID  string
	Command string
	Args    []string
}

// Router manages multiple RestartableClient instances — one per language —
// with lazy startup and file-extension-based routing.
type Router struct {
	mu           sync.RWMutex
	clients      map[string]*RestartableClient // keyed by langID
	configs      []LanguageConfig
	workspaceDir string
	ctx          context.Context
}

// NewRouter creates a new Router.
func NewRouter(ctx context.Context, workspaceDir string, configs []LanguageConfig) *Router {
	return &Router{
		clients:      make(map[string]*RestartableClient),
		configs:      configs,
		workspaceDir: workspaceDir,
		ctx:          ctx,
	}
}

// findConfig returns the LanguageConfig for the given langID, checking
// exact match first, then language groups.
func (r *Router) findConfig(langID string) (*LanguageConfig, bool) {
	for i := range r.configs {
		if r.configs[i].LangID == langID {
			return &r.configs[i], true
		}
	}
	// Check language groups (e.g., "cpp" → "c")
	if primary, ok := languageGroups[langID]; ok {
		for i := range r.configs {
			if r.configs[i].LangID == primary {
				return &r.configs[i], true
			}
		}
	}
	return nil, false
}

// getOrStartClient returns the RestartableClient for the given langID,
// lazily starting it if needed.
func (r *Router) getOrStartClient(ctx context.Context, langID string) (*RestartableClient, error) {
	// Fast path: RLock check
	r.mu.RLock()
	if rc, ok := r.clients[langID]; ok {
		r.mu.RUnlock()
		return rc, nil
	}
	r.mu.RUnlock()

	// Find config
	cfg, ok := r.findConfig(langID)
	if !ok {
		return nil, fmt.Errorf("no LSP server configured for language %q", langID)
	}

	// Slow path: WLock double-check
	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check after acquiring write lock
	if rc, ok := r.clients[langID]; ok {
		return rc, nil
	}

	// Also check if the primary language already has a client
	// (e.g., "cpp" should reuse the "c" client)
	if primary, ok := languageGroups[langID]; ok {
		if rc, ok := r.clients[primary]; ok {
			// Store under both keys so future lookups are fast
			r.clients[langID] = rc
			return rc, nil
		}
	}

	rc := NewRestartableClient(cfg.Command, r.workspaceDir, cfg.Args...)
	if err := rc.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start LSP server for %q: %w", langID, err)
	}

	// Store under the config's langID and the requested langID
	r.clients[cfg.LangID] = rc
	if langID != cfg.LangID {
		r.clients[langID] = rc
	}

	lspLogger.Info("Lazily started LSP server for language %q (command: %s)", langID, cfg.Command)
	return rc, nil
}

// ClientForFile returns the inner *Client for the given file path,
// lazily starting the appropriate LSP server if needed.
func (r *Router) ClientForFile(ctx context.Context, filePath string) (*Client, error) {
	rc, err := r.RestartableClientForFile(ctx, filePath)
	if err != nil {
		return nil, err
	}
	return rc.GetClient()
}

// RestartableClientForFile returns the *RestartableClient for the given file path.
func (r *Router) RestartableClientForFile(ctx context.Context, filePath string) (*RestartableClient, error) {
	langID := string(DetectLanguageID(filePath))
	if langID == "" {
		return nil, fmt.Errorf("unknown language for file %q", filePath)
	}
	return r.getOrStartClient(ctx, langID)
}

// ClientForLang returns the inner *Client for the given language ID,
// lazily starting the LSP server if needed.
func (r *Router) ClientForLang(ctx context.Context, langID string) (*Client, error) {
	rc, err := r.getOrStartClient(ctx, langID)
	if err != nil {
		return nil, err
	}
	return rc.GetClient()
}

// ActiveClients returns all currently-started *Client instances.
// Does not lazily start any servers.
func (r *Router) ActiveClients() []*Client {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Deduplicate clients (language groups may cause aliases)
	seen := make(map[*RestartableClient]bool)
	var clients []*Client
	for _, rc := range r.clients {
		if seen[rc] {
			continue
		}
		seen[rc] = true
		if c, err := rc.GetClient(); err == nil {
			clients = append(clients, c)
		}
	}
	return clients
}

// Close shuts down all active LSP clients gracefully.
func (r *Router) Close(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Deduplicate clients before closing
	seen := make(map[*RestartableClient]bool)
	for _, rc := range r.clients {
		if seen[rc] {
			continue
		}
		seen[rc] = true

		rc.CloseAllFiles(ctx)

		// Best-effort shutdown/exit
		if err := rc.Shutdown(ctx); err != nil {
			lspLogger.Error("Shutdown failed: %v", err)
		}
		if err := rc.Exit(ctx); err != nil {
			lspLogger.Error("Exit failed: %v", err)
		}
		if err := rc.Close(); err != nil {
			lspLogger.Error("Close failed: %v", err)
		}
	}

	r.clients = make(map[string]*RestartableClient)
}

// watcher.LSPClient interface implementation

// IsFileOpen checks if a file is open in any active client.
func (r *Router) IsFileOpen(path string) bool {
	rc, err := r.RestartableClientForFile(r.ctx, path)
	if err != nil {
		return false
	}
	return rc.IsFileOpen(path)
}

// OpenFile opens a file in the appropriate LSP client.
func (r *Router) OpenFile(ctx context.Context, path string) error {
	rc, err := r.RestartableClientForFile(ctx, path)
	if err != nil {
		// Not an error for the watcher — many file types won't have configs
		return nil
	}
	return rc.OpenFile(ctx, path)
}

// NotifyChange notifies the appropriate LSP client of a file change.
func (r *Router) NotifyChange(ctx context.Context, path string) error {
	rc, err := r.RestartableClientForFile(ctx, path)
	if err != nil {
		return nil
	}
	return rc.NotifyChange(ctx, path)
}

// DidChangeWatchedFiles broadcasts watched file events to ALL active clients.
func (r *Router) DidChangeWatchedFiles(ctx context.Context, params protocol.DidChangeWatchedFilesParams) error {
	r.mu.RLock()
	seen := make(map[*RestartableClient]bool)
	var rcs []*RestartableClient
	for _, rc := range r.clients {
		if !seen[rc] {
			seen[rc] = true
			rcs = append(rcs, rc)
		}
	}
	r.mu.RUnlock()

	var lastErr error
	for _, rc := range rcs {
		if err := rc.DidChangeWatchedFiles(ctx, params); err != nil {
			lastErr = err
		}
	}
	return lastErr
}
