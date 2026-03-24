package lsp

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/angalato08/mcp-language-server/internal/protocol"
)

type Client struct {
	Cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr io.ReadCloser

	// Request ID counter
	nextID atomic.Int32

	// Response handlers
	handlers   map[string]chan *Message
	handlersMu sync.RWMutex

	// Server request handlers
	serverRequestHandlers map[string]ServerRequestHandler
	serverHandlersMu      sync.RWMutex

	// Notification handlers
	notificationHandlers map[string]NotificationHandler
	notificationMu       sync.RWMutex

	// Diagnostic cache
	diagnostics         map[protocol.DocumentUri][]protocol.Diagnostic
	previousDiagnostics map[protocol.DocumentUri][]protocol.Diagnostic
	diagnosticsMu       sync.RWMutex

	// Signals when diagnostics are updated for a URI
	diagnosticReady   map[protocol.DocumentUri]chan struct{}
	diagnosticReadyMu sync.Mutex

	// Files are currently opened by the LSP
	openFiles   map[string]*OpenFileInfo
	openFilesMu sync.RWMutex

	// Progress tracking (e.g. indexing)
	activeProgress map[string]*ProgressInfo // token → info
	progressMu     sync.RWMutex

	// Semantic token legend from server capabilities
	semanticTokenLegend *protocol.SemanticTokensLegend

	// Closed when handleMessages exits (LSP process died)
	done     chan struct{}
	doneOnce sync.Once
}

// SemanticTokenLegend returns the semantic token legend from the server's initialize result.
func (c *Client) SemanticTokenLegend() *protocol.SemanticTokensLegend {
	return c.semanticTokenLegend
}

// ProgressInfo tracks a single work-done progress operation.
type ProgressInfo struct {
	Token      string
	Title      string
	Message    string
	Percentage uint32
	Done       bool
}

func NewClient(command string, args ...string) (*Client, error) {
	cmd := exec.Command(command, args...)
	// Copy env
	cmd.Env = os.Environ()

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	client := &Client{
		Cmd:                   cmd,
		stdin:                 stdin,
		stdout:                bufio.NewReader(stdout),
		stderr:                stderr,
		handlers:              make(map[string]chan *Message),
		notificationHandlers:  make(map[string]NotificationHandler),
		serverRequestHandlers: make(map[string]ServerRequestHandler),
		diagnostics:           make(map[protocol.DocumentUri][]protocol.Diagnostic),
		previousDiagnostics:   make(map[protocol.DocumentUri][]protocol.Diagnostic),
		diagnosticReady:       make(map[protocol.DocumentUri]chan struct{}),
		openFiles:             make(map[string]*OpenFileInfo),
		activeProgress:        make(map[string]*ProgressInfo),
		done:                  make(chan struct{}),
	}

	// Start the LSP server process
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start LSP server: %w", err)
	}

	// Handle stderr in a separate goroutine with proper logging
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			processLogger.Info("%s", line)
		}
		if err := scanner.Err(); err != nil {
			lspLogger.Error("Error reading LSP server stderr: %v", err)
		}
	}()

	// Start message handling loop
	go client.handleMessages()

	return client, nil
}

func (c *Client) RegisterNotificationHandler(method string, handler NotificationHandler) {
	c.notificationMu.Lock()
	defer c.notificationMu.Unlock()
	c.notificationHandlers[method] = handler
}

func (c *Client) RegisterServerRequestHandler(method string, handler ServerRequestHandler) {
	c.serverHandlersMu.Lock()
	defer c.serverHandlersMu.Unlock()
	c.serverRequestHandlers[method] = handler
}

func (c *Client) InitializeLSPClient(ctx context.Context, workspaceDir string) (*protocol.InitializeResult, error) {
	initParams := &protocol.InitializeParams{
		WorkspaceFoldersInitializeParams: protocol.WorkspaceFoldersInitializeParams{
			WorkspaceFolders: []protocol.WorkspaceFolder{
				{
					URI:  protocol.URI("file://" + workspaceDir),
					Name: workspaceDir,
				},
			},
		},

		XInitializeParams: protocol.XInitializeParams{
			ProcessID: int32(os.Getpid()),
			ClientInfo: &protocol.ClientInfo{
				Name:    "mcp-language-server",
				Version: "0.1.0",
			},
			RootPath: workspaceDir,
			RootURI:  protocol.DocumentUri("file://" + workspaceDir),
			Capabilities: protocol.ClientCapabilities{
				Workspace: protocol.WorkspaceClientCapabilities{
					Configuration: true,
					DidChangeConfiguration: protocol.DidChangeConfigurationClientCapabilities{
						DynamicRegistration: true,
					},
					DidChangeWatchedFiles: protocol.DidChangeWatchedFilesClientCapabilities{
						DynamicRegistration:    true,
						RelativePatternSupport: true,
					},
					WorkspaceEdit: &protocol.WorkspaceEditClientCapabilities{
						DocumentChanges: true,
						ResourceOperations: []protocol.ResourceOperationKind{
							protocol.Create,
							protocol.Rename,
							protocol.Delete,
						},
						FailureHandling: ptrTo(protocol.Abort),
					},
				},
				TextDocument: protocol.TextDocumentClientCapabilities{
					Synchronization: &protocol.TextDocumentSyncClientCapabilities{
						DynamicRegistration: true,
						DidSave:             true,
					},
					Completion: protocol.CompletionClientCapabilities{
						CompletionItem: protocol.ClientCompletionItemOptions{},
					},
					CodeLens: &protocol.CodeLensClientCapabilities{
						DynamicRegistration: true,
					},
					DocumentSymbol: protocol.DocumentSymbolClientCapabilities{
						HierarchicalDocumentSymbolSupport: true,
					},
					Hover: &protocol.HoverClientCapabilities{
						ContentFormat: []protocol.MarkupKind{protocol.Markdown, protocol.PlainText},
					},
					Definition: &protocol.DefinitionClientCapabilities{
						DynamicRegistration: true,
						LinkSupport:         true,
					},
					TypeDefinition: &protocol.TypeDefinitionClientCapabilities{
						DynamicRegistration: true,
						LinkSupport:         true,
					},
					Implementation: &protocol.ImplementationClientCapabilities{
						DynamicRegistration: true,
						LinkSupport:         true,
					},
					References: &protocol.ReferenceClientCapabilities{
						DynamicRegistration: true,
					},
					CodeAction: protocol.CodeActionClientCapabilities{
						CodeActionLiteralSupport: protocol.ClientCodeActionLiteralOptions{
							CodeActionKind: protocol.ClientCodeActionKindOptions{
								ValueSet: []protocol.CodeActionKind{},
							},
						},
					},
					PublishDiagnostics: protocol.PublishDiagnosticsClientCapabilities{
						VersionSupport: true,
					},
					CallHierarchy: &protocol.CallHierarchyClientCapabilities{
						DynamicRegistration: true,
					},
					SemanticTokens: protocol.SemanticTokensClientCapabilities{
						Requests: protocol.ClientSemanticTokensRequestOptions{
							Range: &protocol.Or_ClientSemanticTokensRequestOptions_range{},
							Full:  &protocol.Or_ClientSemanticTokensRequestOptions_full{},
						},
						TokenTypes:     []string{},
						TokenModifiers: []string{},
						Formats:        []protocol.TokenFormat{},
					},
				},
				Window: protocol.WindowClientCapabilities{
					WorkDoneProgress: true,
				},
			},
			InitializationOptions: map[string]any{
				"codelenses": map[string]bool{
					"generate":           true,
					"regenerate_cgo":     true,
					"test":               true,
					"tidy":               true,
					"upgrade_dependency": true,
					"vendor":             true,
					"vulncheck":          false,
				},
			},
		},
	}

	var result protocol.InitializeResult
	if err := c.Call(ctx, "initialize", initParams, &result); err != nil {
		return nil, fmt.Errorf("initialize failed: %w", err)
	}

	// Extract semantic token legend from server capabilities
	if result.Capabilities.SemanticTokensProvider != nil {
		if raw, err := json.Marshal(result.Capabilities.SemanticTokensProvider); err == nil {
			var opts protocol.SemanticTokensOptions
			if err := json.Unmarshal(raw, &opts); err == nil {
				c.semanticTokenLegend = &opts.Legend
			}
		}
	}

	if err := c.Notify(ctx, "initialized", struct{}{}); err != nil {
		return nil, fmt.Errorf("initialized notification failed: %w", err)
	}

	// Register handlers
	c.RegisterServerRequestHandler("workspace/applyEdit", HandleApplyEdit)
	c.RegisterServerRequestHandler("workspace/configuration", HandleWorkspaceConfiguration)
	c.RegisterServerRequestHandler("client/registerCapability", HandleRegisterCapability)
	c.RegisterNotificationHandler("window/showMessage", HandleServerMessage)
	c.RegisterNotificationHandler("textDocument/publishDiagnostics",
		func(params json.RawMessage) { HandleDiagnostics(c, params) })
	c.RegisterServerRequestHandler("window/workDoneProgress/create",
		func(params json.RawMessage) (any, error) { return HandleWorkDoneProgressCreate(c, params) })
	c.RegisterNotificationHandler("$/progress",
		func(params json.RawMessage) { HandleProgress(c, params) })

	// Notify the LSP server
	err := c.Initialized(ctx, protocol.InitializedParams{})
	if err != nil {
		return nil, fmt.Errorf("initialization failed: %w", err)
	}

	// LSP sepecific Initialization
	path := strings.ToLower(c.Cmd.Path)
	switch {
	case strings.Contains(path, "typescript-language-server"):
		err := initializeTypescriptLanguageServer(ctx, c, workspaceDir)
		if err != nil {
			return nil, err
		}
	}

	return &result, nil
}

func (c *Client) Close() error {
	// Try to close all open files first
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Attempt to close files but continue shutdown regardless
	c.CloseAllFiles(ctx)

	// Force kill the LSP process if it doesn't exit within timeout
	forcedKill := make(chan struct{})
	go func() {
		select {
		case <-time.After(2 * time.Second):
			lspLogger.Warn("LSP process did not exit within timeout, forcing kill")
			if c.Cmd.Process != nil {
				if err := c.Cmd.Process.Kill(); err != nil {
					lspLogger.Error("Failed to kill process: %v", err)
				} else {
					lspLogger.Info("Process killed successfully")
				}
			}
			close(forcedKill)
		case <-forcedKill:
			// Channel closed from completion path
			return
		}
	}()

	// Close stdin to signal the server
	if err := c.stdin.Close(); err != nil {
		lspLogger.Error("Failed to close stdin: %v", err)
	}

	// Wait for process to exit
	err := c.Cmd.Wait()
	close(forcedKill) // Stop the force kill goroutine

	return err
}

type ServerState int

const (
	StateStarting ServerState = iota
	StateReady
	StateError
)

func (c *Client) WaitForServerReady(ctx context.Context) error {
	// TODO: wait for specific messages or poll workspace/symbol
	time.Sleep(time.Second * 1)
	return nil
}

type OpenFileInfo struct {
	Version     int32
	URI         protocol.DocumentUri
	ContentHash [sha256.Size]byte // hash of content last sent to the server
}

func (c *Client) OpenFile(ctx context.Context, filepath string) error {
	uri := fmt.Sprintf("file://%s", filepath)

	c.openFilesMu.Lock()
	if _, exists := c.openFiles[uri]; exists {
		c.openFilesMu.Unlock()
		return nil // Already open
	}
	c.openFilesMu.Unlock()

	// Skip files that do not exist or cannot be read
	content, err := os.ReadFile(filepath)
	if err != nil {
		return fmt.Errorf("error reading file: %w", err)
	}

	params := protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:        protocol.DocumentUri(uri),
			LanguageID: DetectLanguageID(uri),
			Version:    1,
			Text:       string(content),
		},
	}

	if err := c.Notify(ctx, "textDocument/didOpen", params); err != nil {
		return err
	}

	c.openFilesMu.Lock()
	c.openFiles[uri] = &OpenFileInfo{
		Version:     1,
		URI:         protocol.DocumentUri(uri),
		ContentHash: sha256.Sum256(content),
	}
	c.openFilesMu.Unlock()

	lspLogger.Debug("Opened file: %s", filepath)

	return nil
}

// SyncFileFromDisk ensures the LSP server has the latest on-disk content.
// If the file is not yet open, it opens it. If it is open but disk content
// has changed since the last sync, it sends a didChange notification.
// Returns true if a sync (open or change) was performed.
func (c *Client) SyncFileFromDisk(ctx context.Context, filepath string) (bool, error) {
	uri := fmt.Sprintf("file://%s", filepath)

	c.openFilesMu.RLock()
	fileInfo, isOpen := c.openFiles[uri]
	c.openFilesMu.RUnlock()

	if !isOpen {
		return true, c.OpenFile(ctx, filepath)
	}

	// File is open — check if disk content differs
	content, err := os.ReadFile(filepath)
	if err != nil {
		return false, fmt.Errorf("error reading file: %w", err)
	}

	diskHash := sha256.Sum256(content)
	if diskHash == fileInfo.ContentHash {
		return false, nil // content unchanged
	}

	// Content changed on disk — send didChange
	return true, c.NotifyChange(ctx, filepath)
}

func (c *Client) NotifyChange(ctx context.Context, filepath string) error {
	uri := fmt.Sprintf("file://%s", filepath)

	content, err := os.ReadFile(filepath)
	if err != nil {
		return fmt.Errorf("error reading file: %w", err)
	}

	c.openFilesMu.Lock()
	fileInfo, isOpen := c.openFiles[uri]
	if !isOpen {
		c.openFilesMu.Unlock()
		return fmt.Errorf("cannot notify change for unopened file: %s", filepath)
	}

	// Increment version and update content hash
	fileInfo.Version++
	fileInfo.ContentHash = sha256.Sum256(content)
	version := fileInfo.Version
	c.openFilesMu.Unlock()

	params := protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{
				URI: protocol.DocumentUri(uri),
			},
			Version: version,
		},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{
			{
				Value: protocol.TextDocumentContentChangeWholeDocument{
					Text: string(content),
				},
			},
		},
	}

	// Reset the signal channel BEFORE sending didChange so that the next
	// WaitForDiagnostics blocks until the LSP sends fresh publishDiagnostics.
	// We intentionally do NOT clear the diagnostics cache here because the
	// file watcher may trigger a second NotifyChange for the same edit, and
	// clearing the cache would discard diagnostics that clangd already sent
	// in response to the first didChange.
	docURI := protocol.DocumentUri(uri)
	c.diagnosticReadyMu.Lock()
	c.diagnosticReady[docURI] = make(chan struct{}, 1)
	c.diagnosticReadyMu.Unlock()

	return c.Notify(ctx, "textDocument/didChange", params)
}

func (c *Client) CloseFile(ctx context.Context, filepath string) error {
	uri := fmt.Sprintf("file://%s", filepath)

	c.openFilesMu.Lock()
	if _, exists := c.openFiles[uri]; !exists {
		c.openFilesMu.Unlock()
		return nil // Already closed
	}
	c.openFilesMu.Unlock()

	params := protocol.DidCloseTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: protocol.DocumentUri(uri),
		},
	}
	lspLogger.Debug("Closing file: %s", params.TextDocument.URI.Dir())
	if err := c.Notify(ctx, "textDocument/didClose", params); err != nil {
		return err
	}

	c.openFilesMu.Lock()
	delete(c.openFiles, uri)
	c.openFilesMu.Unlock()

	return nil
}

// GetOpenFiles returns a list of currently open file paths.
func (c *Client) GetOpenFiles() []string {
	c.openFilesMu.RLock()
	defer c.openFilesMu.RUnlock()

	paths := make([]string, 0, len(c.openFiles))
	for uri := range c.openFiles {
		paths = append(paths, strings.TrimPrefix(uri, "file://"))
	}
	return paths
}

func (c *Client) IsFileOpen(filepath string) bool {
	uri := fmt.Sprintf("file://%s", filepath)
	c.openFilesMu.RLock()
	defer c.openFilesMu.RUnlock()
	_, exists := c.openFiles[uri]
	return exists
}

// RestartServer is a no-op on a bare Client. It exists to satisfy the
// watcher.LSPClient interface when a Client is used directly (e.g. tests).
// Real restart logic lives on Router.RestartServer / RestartableClient.Restart.
func (c *Client) RestartServer(ctx context.Context, langID string) error {
	return nil
}

// CloseAllFiles closes all currently open files
func (c *Client) CloseAllFiles(ctx context.Context) {
	c.openFilesMu.Lock()
	filesToClose := make([]string, 0, len(c.openFiles))

	// First collect all URIs that need to be closed
	for uri := range c.openFiles {
		// Convert URI back to file path by trimming "file://" prefix
		filePath := strings.TrimPrefix(uri, "file://")
		filesToClose = append(filesToClose, filePath)
	}
	c.openFilesMu.Unlock()

	// Then close them all
	for _, filePath := range filesToClose {
		err := c.CloseFile(ctx, filePath)
		if err != nil {
			lspLogger.Error("Error closing file %s: %v", filePath, err)
		}
	}

	lspLogger.Debug("Closed %d files", len(filesToClose))
}

// WaitForDiagnostics waits for a publishDiagnostics notification for the given URI,
// or returns after timeout.
func (c *Client) WaitForDiagnostics(uri protocol.DocumentUri, timeout time.Duration) {
	c.diagnosticReadyMu.Lock()
	ch, exists := c.diagnosticReady[uri]
	if !exists {
		ch = make(chan struct{}, 1)
		c.diagnosticReady[uri] = ch
	}
	chOnce := ch
	c.diagnosticReadyMu.Unlock()

	select {
	case <-chOnce:
	case <-time.After(timeout):
	}
}

// SignalDiagnostics signals that diagnostics have been received for a URI.
func (c *Client) SignalDiagnostics(uri protocol.DocumentUri) {
	c.diagnosticReadyMu.Lock()
	defer c.diagnosticReadyMu.Unlock()

	ch, exists := c.diagnosticReady[uri]
	if !exists {
		ch = make(chan struct{}, 1)
		c.diagnosticReady[uri] = ch
	}
	// Non-blocking send
	select {
	case ch <- struct{}{}:
	default:
	}
}

// GetActiveProgress returns a snapshot of all active (non-done) progress operations.
func (c *Client) GetActiveProgress() []ProgressInfo {
	c.progressMu.RLock()
	defer c.progressMu.RUnlock()

	var result []ProgressInfo
	for _, p := range c.activeProgress {
		if !p.Done {
			result = append(result, *p)
		}
	}
	return result
}

// IsIndexing returns true if any active progress operation looks like indexing.
func (c *Client) IsIndexing() bool {
	c.progressMu.RLock()
	defer c.progressMu.RUnlock()

	for _, p := range c.activeProgress {
		if !p.Done {
			return true
		}
	}
	return false
}

// GetFileDiagnostics returns diagnostics for a single URI
func (c *Client) GetFileDiagnostics(uri protocol.DocumentUri) []protocol.Diagnostic {
	c.diagnosticsMu.RLock()
	defer c.diagnosticsMu.RUnlock()

	return c.diagnostics[uri]
}

// GetAllDiagnostics returns a copy of all current diagnostics
func (c *Client) GetAllDiagnostics() map[protocol.DocumentUri][]protocol.Diagnostic {
	c.diagnosticsMu.RLock()
	defer c.diagnosticsMu.RUnlock()

	// Return a copy to avoid concurrency issues
	result := make(map[protocol.DocumentUri][]protocol.Diagnostic)
	for k, v := range c.diagnostics {
		if len(v) > 0 {
			result[k] = v
		}
	}
	return result
}

// DiagnosticDiff holds new and resolved diagnostics for a single URI.
type DiagnosticDiff struct {
	New      []protocol.Diagnostic
	Resolved []protocol.Diagnostic
}

// GetDiagnosticDiff returns the new and resolved diagnostics for a single URI
// by comparing previousDiagnostics against current diagnostics.
func (c *Client) GetDiagnosticDiff(uri protocol.DocumentUri) (newDiags, resolved []protocol.Diagnostic) {
	c.diagnosticsMu.RLock()
	defer c.diagnosticsMu.RUnlock()

	prev := c.previousDiagnostics[uri]
	curr := c.diagnostics[uri]

	return ComputeDiagnosticDiff(prev, curr)
}

// GetAllDiagnosticDiffs returns diagnostic diffs for all URIs that have changes.
func (c *Client) GetAllDiagnosticDiffs() map[protocol.DocumentUri]*DiagnosticDiff {
	c.diagnosticsMu.RLock()
	defer c.diagnosticsMu.RUnlock()

	result := make(map[protocol.DocumentUri]*DiagnosticDiff)

	// Check all URIs that appear in either previous or current
	seen := make(map[protocol.DocumentUri]bool)
	for uri := range c.diagnostics {
		seen[uri] = true
	}
	for uri := range c.previousDiagnostics {
		seen[uri] = true
	}

	for uri := range seen {
		prev := c.previousDiagnostics[uri]
		curr := c.diagnostics[uri]
		newDiags, resolved := ComputeDiagnosticDiff(prev, curr)
		if len(newDiags) > 0 || len(resolved) > 0 {
			result[uri] = &DiagnosticDiff{New: newDiags, Resolved: resolved}
		}
	}

	return result
}


// ComputeDiagnosticDiff is a pure function that computes the diff between two diagnostic slices.
// New diagnostics are those in current but not in previous; resolved are in previous but not in current.
// Key: (Range.Start.Line, Range.Start.Character, Message, Code, Source)
func ComputeDiagnosticDiff(previous, current []protocol.Diagnostic) (newDiags, resolved []protocol.Diagnostic) {
	type diagKey struct {
		Line    uint32
		Char    uint32
		Message string
		Code    string
		Source  string
	}

	makeKey := func(d protocol.Diagnostic) diagKey {
		codeStr := ""
		if d.Code != nil {
			codeStr = fmt.Sprintf("%v", d.Code)
		}
		return diagKey{
			Line:    d.Range.Start.Line,
			Char:    d.Range.Start.Character,
			Message: d.Message,
			Code:    codeStr,
			Source:  d.Source,
		}
	}

	prevSet := make(map[diagKey]int)
	for _, d := range previous {
		prevSet[makeKey(d)]++
	}

	currSet := make(map[diagKey]int)
	for _, d := range current {
		currSet[makeKey(d)]++
	}

	// New diagnostics: in current but not in previous
	for _, d := range current {
		k := makeKey(d)
		if prevSet[k] > 0 {
			prevSet[k]--
		} else {
			newDiags = append(newDiags, d)
		}
	}

	// Resolved diagnostics: in previous but not in current
	for _, d := range previous {
		k := makeKey(d)
		if currSet[k] > 0 {
			currSet[k]--
		} else {
			resolved = append(resolved, d)
		}
	}

	return newDiags, resolved
}

func ptrTo[T any](v T) *T {
	return &v
}
