package lsp

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/angalato08/mcp-language-server/internal/protocol"
)

// RestartableClient wraps a Client with automatic restart on LSP server death.
type RestartableClient struct {
	mu     sync.RWMutex
	client *Client

	command      string
	args         []string
	workspaceDir string

	// Backoff state
	restartCount int
	restarting   atomic.Bool

	// Used to stop the health monitor on Close
	cancel context.CancelFunc
	ctx    context.Context
}

// NewRestartableClient creates a new RestartableClient.
func NewRestartableClient(command, workspaceDir string, args ...string) *RestartableClient {
	return &RestartableClient{
		command:      command,
		args:         args,
		workspaceDir: workspaceDir,
	}
}

// Start creates the inner client, initializes the LSP, and starts the health monitor.
func (rc *RestartableClient) Start(ctx context.Context) error {
	rc.ctx, rc.cancel = context.WithCancel(ctx)

	client, err := NewClient(rc.command, rc.args...)
	if err != nil {
		return fmt.Errorf("failed to create LSP client: %w", err)
	}

	rc.mu.Lock()
	rc.client = client
	rc.mu.Unlock()

	if _, err := client.InitializeLSPClient(rc.ctx, rc.workspaceDir); err != nil {
		client.Close()
		return fmt.Errorf("initialize failed: %w", err)
	}

	go rc.monitorHealth()
	return nil
}

// GetClient returns the current inner client. Returns ErrServerRestarting if a restart is in progress.
func (rc *RestartableClient) GetClient() (*Client, error) {
	if rc.restarting.Load() {
		return nil, ErrServerRestarting
	}
	rc.mu.RLock()
	c := rc.client
	rc.mu.RUnlock()
	if c == nil {
		return nil, ErrServerRestarting
	}
	return c, nil
}

// Close gracefully shuts down the restartable client.
func (rc *RestartableClient) Close() error {
	rc.cancel()
	rc.mu.RLock()
	c := rc.client
	rc.mu.RUnlock()
	if c != nil {
		return c.Close()
	}
	return nil
}

// Shutdown sends the LSP shutdown request.
func (rc *RestartableClient) Shutdown(ctx context.Context) error {
	rc.mu.RLock()
	c := rc.client
	rc.mu.RUnlock()
	if c == nil {
		return ErrServerRestarting
	}
	return c.Shutdown(ctx)
}

// Exit sends the LSP exit notification.
func (rc *RestartableClient) Exit(ctx context.Context) error {
	rc.mu.RLock()
	c := rc.client
	rc.mu.RUnlock()
	if c == nil {
		return ErrServerRestarting
	}
	return c.Exit(ctx)
}

// CloseAllFiles closes all open files on the inner client.
func (rc *RestartableClient) CloseAllFiles(ctx context.Context) {
	rc.mu.RLock()
	c := rc.client
	rc.mu.RUnlock()
	if c != nil {
		c.CloseAllFiles(ctx)
	}
}

// WaitForServerReady waits for the inner LSP server to be ready.
func (rc *RestartableClient) WaitForServerReady(ctx context.Context) error {
	rc.mu.RLock()
	c := rc.client
	rc.mu.RUnlock()
	if c == nil {
		return ErrServerRestarting
	}
	return c.WaitForServerReady(ctx)
}

// Delegation methods for watcher.LSPClient interface

func (rc *RestartableClient) IsFileOpen(path string) bool {
	rc.mu.RLock()
	c := rc.client
	rc.mu.RUnlock()
	if c == nil {
		return false
	}
	return c.IsFileOpen(path)
}

func (rc *RestartableClient) OpenFile(ctx context.Context, path string) error {
	rc.mu.RLock()
	c := rc.client
	rc.mu.RUnlock()
	if c == nil {
		return ErrServerRestarting
	}
	return c.OpenFile(ctx, path)
}

func (rc *RestartableClient) NotifyChange(ctx context.Context, path string) error {
	rc.mu.RLock()
	c := rc.client
	rc.mu.RUnlock()
	if c == nil {
		return ErrServerRestarting
	}
	return c.NotifyChange(ctx, path)
}

func (rc *RestartableClient) DidChangeWatchedFiles(ctx context.Context, params protocol.DidChangeWatchedFilesParams) error {
	rc.mu.RLock()
	c := rc.client
	rc.mu.RUnlock()
	if c == nil {
		return ErrServerRestarting
	}
	return c.DidChangeWatchedFiles(ctx, params)
}

func (rc *RestartableClient) monitorHealth() {
	rc.mu.RLock()
	c := rc.client
	rc.mu.RUnlock()

	select {
	case <-c.done:
		lspLogger.Warn("LSP server process died, attempting restart")
		rc.restart()
	case <-rc.ctx.Done():
		return
	}
}

func (rc *RestartableClient) restart() {
	if !rc.restarting.CompareAndSwap(false, true) {
		return // already restarting
	}
	defer rc.restarting.Store(false)

	// Backoff: 1s, 2s, 4s, 8s, 16s, 30s max
	backoffs := []time.Duration{
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		30 * time.Second,
	}

	for {
		select {
		case <-rc.ctx.Done():
			return
		default:
		}

		idx := rc.restartCount
		if idx >= len(backoffs) {
			idx = len(backoffs) - 1
		}
		delay := backoffs[idx]
		rc.restartCount++

		lspLogger.Info("Restarting LSP server (attempt %d, backoff %v)", rc.restartCount, delay)

		select {
		case <-time.After(delay):
		case <-rc.ctx.Done():
			return
		}

		// Close old client (best-effort)
		rc.mu.Lock()
		if rc.client != nil {
			rc.client.Close()
		}
		rc.mu.Unlock()

		client, err := NewClient(rc.command, rc.args...)
		if err != nil {
			lspLogger.Error("Failed to create LSP client during restart: %v", err)
			continue
		}

		if _, err := client.InitializeLSPClient(rc.ctx, rc.workspaceDir); err != nil {
			lspLogger.Error("Failed to initialize LSP during restart: %v", err)
			client.Close()
			continue
		}

		rc.mu.Lock()
		rc.client = client
		rc.mu.Unlock()

		rc.restartCount = 0
		lspLogger.Info("LSP server restarted successfully")

		// Start monitoring the new client
		go rc.monitorHealth()
		return
	}
}
