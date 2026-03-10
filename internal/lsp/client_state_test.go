package lsp

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/angalato08/mcp-language-server/internal/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestClient creates a minimal Client for testing state management
// (diagnostics, open files) without spawning a real process.
func newTestClient() *Client {
	return &Client{
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
}

func TestDiagnosticCaching(t *testing.T) {
	t.Parallel()
	c := newTestClient()

	uri := protocol.DocumentUri("file:///test.go")

	// Initially empty
	diags := c.GetFileDiagnostics(uri)
	assert.Empty(t, diags)

	all := c.GetAllDiagnostics()
	assert.Empty(t, all)

	// Store some diagnostics
	c.diagnosticsMu.Lock()
	c.diagnostics[uri] = []protocol.Diagnostic{
		{Message: "unused variable"},
		{Message: "missing return"},
	}
	c.diagnosticsMu.Unlock()

	// Retrieve
	diags = c.GetFileDiagnostics(uri)
	assert.Len(t, diags, 2)
	assert.Equal(t, "unused variable", diags[0].Message)

	all = c.GetAllDiagnostics()
	assert.Len(t, all, 1)
	assert.Len(t, all[uri], 2)

	// GetAllDiagnostics returns a copy
	all[uri] = nil
	assert.Len(t, c.GetFileDiagnostics(uri), 2, "modifying returned map should not affect cache")
}

func TestDiagnosticCaching_EmptyDiagnosticsNotReturned(t *testing.T) {
	t.Parallel()
	c := newTestClient()

	uri := protocol.DocumentUri("file:///test.go")

	// Store empty diagnostics (e.g., after errors are fixed)
	c.diagnosticsMu.Lock()
	c.diagnostics[uri] = []protocol.Diagnostic{}
	c.diagnosticsMu.Unlock()

	// GetAllDiagnostics should not include URIs with empty slices
	all := c.GetAllDiagnostics()
	assert.Empty(t, all)
}

func TestSignalAndWaitForDiagnostics(t *testing.T) {
	t.Parallel()
	c := newTestClient()

	uri := protocol.DocumentUri("file:///test.go")

	// Signal before anyone is waiting (should not block)
	c.SignalDiagnostics(uri)

	// WaitForDiagnostics should return immediately since signal is buffered
	done := make(chan struct{})
	go func() {
		c.WaitForDiagnostics(uri, 2*time.Second)
		close(done)
	}()

	select {
	case <-done:
		// Good, returned promptly
	case <-time.After(1 * time.Second):
		t.Fatal("WaitForDiagnostics did not return after signal")
	}
}

func TestWaitForDiagnosticsTimeout(t *testing.T) {
	t.Parallel()
	c := newTestClient()

	uri := protocol.DocumentUri("file:///never-signaled.go")

	start := time.Now()
	c.WaitForDiagnostics(uri, 100*time.Millisecond)
	elapsed := time.Since(start)

	assert.GreaterOrEqual(t, elapsed.Milliseconds(), int64(90), "should wait at least ~100ms")
	assert.Less(t, elapsed.Milliseconds(), int64(500), "should not wait too long")
}

func TestSignalDiagnosticsConcurrent(t *testing.T) {
	t.Parallel()
	c := newTestClient()

	uri := protocol.DocumentUri("file:///concurrent.go")

	// Signal from multiple goroutines — should not panic or deadlock
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.SignalDiagnostics(uri)
		}()
	}
	wg.Wait()
}

func TestOpenFileTracking(t *testing.T) {
	t.Parallel()
	c := newTestClient()

	// IsFileOpen returns false for unknown files
	assert.False(t, c.IsFileOpen("/test.go"))

	// Simulate opening a file
	uri := "file:///test.go"
	c.openFilesMu.Lock()
	c.openFiles[uri] = &OpenFileInfo{
		Version: 1,
		URI:     protocol.DocumentUri(uri),
	}
	c.openFilesMu.Unlock()

	assert.True(t, c.IsFileOpen("/test.go"))

	// Simulate closing
	c.openFilesMu.Lock()
	delete(c.openFiles, uri)
	c.openFilesMu.Unlock()

	assert.False(t, c.IsFileOpen("/test.go"))
}

func TestOpenFileVersionTracking(t *testing.T) {
	t.Parallel()
	c := newTestClient()

	uri := "file:///versioned.go"
	info := &OpenFileInfo{Version: 1, URI: protocol.DocumentUri(uri)}

	c.openFilesMu.Lock()
	c.openFiles[uri] = info
	c.openFilesMu.Unlock()

	// Simulate version increments
	c.openFilesMu.Lock()
	info.Version++
	c.openFilesMu.Unlock()

	c.openFilesMu.RLock()
	assert.Equal(t, int32(2), c.openFiles[uri].Version)
	c.openFilesMu.RUnlock()
}

func TestRegisterNotificationHandler(t *testing.T) {
	t.Parallel()
	c := newTestClient()

	called := false
	c.RegisterNotificationHandler("test/method", func(params json.RawMessage) {
		called = true
	})

	c.notificationMu.RLock()
	handler, ok := c.notificationHandlers["test/method"]
	c.notificationMu.RUnlock()

	require.True(t, ok)
	handler(nil)
	assert.True(t, called)
}

func TestRegisterServerRequestHandler(t *testing.T) {
	t.Parallel()
	c := newTestClient()

	c.RegisterServerRequestHandler("workspace/configuration", func(params json.RawMessage) (any, error) {
		return []map[string]any{{}}, nil
	})

	c.serverHandlersMu.RLock()
	handler, ok := c.serverRequestHandlers["workspace/configuration"]
	c.serverHandlersMu.RUnlock()

	require.True(t, ok)
	result, err := handler(nil)
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestHandleDiagnosticsRotatesPreviousSnapshot(t *testing.T) {
	t.Parallel()
	c := newTestClient()

	uri := protocol.DocumentUri("file:///rotate.go")

	// First notification: 2 diagnostics
	first := protocol.PublishDiagnosticsParams{
		URI: uri,
		Diagnostics: []protocol.Diagnostic{
			{Message: "error A", Range: protocol.Range{Start: protocol.Position{Line: 1}}},
			{Message: "error B", Range: protocol.Range{Start: protocol.Position{Line: 5}}},
		},
	}
	firstJSON, _ := json.Marshal(first)
	HandleDiagnostics(c, firstJSON)

	// After first notification: previous should be nil/empty, current should have 2
	c.diagnosticsMu.RLock()
	assert.Len(t, c.diagnostics[uri], 2)
	assert.Len(t, c.previousDiagnostics[uri], 0, "previous should be empty after first notification")
	c.diagnosticsMu.RUnlock()

	// Second notification: 1 diagnostic (B resolved, C added)
	second := protocol.PublishDiagnosticsParams{
		URI: uri,
		Diagnostics: []protocol.Diagnostic{
			{Message: "error A", Range: protocol.Range{Start: protocol.Position{Line: 1}}},
			{Message: "error C", Range: protocol.Range{Start: protocol.Position{Line: 10}}},
		},
	}
	secondJSON, _ := json.Marshal(second)
	HandleDiagnostics(c, secondJSON)

	// After second notification: previous should have the first set, current the second
	c.diagnosticsMu.RLock()
	assert.Len(t, c.previousDiagnostics[uri], 2, "previous should have first notification's diagnostics")
	assert.Equal(t, "error A", c.previousDiagnostics[uri][0].Message)
	assert.Equal(t, "error B", c.previousDiagnostics[uri][1].Message)
	assert.Len(t, c.diagnostics[uri], 2)
	assert.Equal(t, "error C", c.diagnostics[uri][1].Message)
	c.diagnosticsMu.RUnlock()

	// Verify diff: B resolved, C new
	newDiags, resolved := c.GetDiagnosticDiff(uri)
	assert.Len(t, newDiags, 1, "should have 1 new diagnostic")
	assert.Equal(t, "error C", newDiags[0].Message)
	assert.Len(t, resolved, 1, "should have 1 resolved diagnostic")
	assert.Equal(t, "error B", resolved[0].Message)
}

func TestProgressTracking(t *testing.T) {
	t.Parallel()
	c := newTestClient()

	// Initially no progress
	assert.False(t, c.IsIndexing())
	assert.Empty(t, c.GetActiveProgress())

	// Simulate workDoneProgress/create
	createParams := `{"token":"idx-1"}`
	_, err := HandleWorkDoneProgressCreate(c, json.RawMessage(createParams))
	require.NoError(t, err)

	// Token created but no begin yet — not active
	assert.False(t, c.IsIndexing(), "token created without begin should not count as indexing")

	// Simulate $/progress begin
	beginParams := `{"token":"idx-1","value":{"kind":"begin","title":"Indexing","message":"0/10 files","percentage":0}}`
	HandleProgress(c, json.RawMessage(beginParams))

	assert.True(t, c.IsIndexing())
	active := c.GetActiveProgress()
	require.Len(t, active, 1)
	assert.Equal(t, "Indexing", active[0].Title)
	assert.Equal(t, "0/10 files", active[0].Message)
	assert.Equal(t, uint32(0), active[0].Percentage)

	// Simulate $/progress report
	reportParams := `{"token":"idx-1","value":{"kind":"report","message":"5/10 files","percentage":50}}`
	HandleProgress(c, json.RawMessage(reportParams))

	active = c.GetActiveProgress()
	require.Len(t, active, 1)
	assert.Equal(t, "5/10 files", active[0].Message)
	assert.Equal(t, uint32(50), active[0].Percentage)

	// Simulate $/progress end
	endParams := `{"token":"idx-1","value":{"kind":"end","message":"Indexing complete"}}`
	HandleProgress(c, json.RawMessage(endParams))

	assert.False(t, c.IsIndexing())
	assert.Empty(t, c.GetActiveProgress())
}

func TestProgressTrackingMultipleTokens(t *testing.T) {
	t.Parallel()
	c := newTestClient()

	// Begin two operations
	HandleProgress(c, json.RawMessage(`{"token":"idx","value":{"kind":"begin","title":"Indexing"}}`))
	HandleProgress(c, json.RawMessage(`{"token":"lint","value":{"kind":"begin","title":"Linting"}}`))

	assert.True(t, c.IsIndexing())
	assert.Len(t, c.GetActiveProgress(), 2)

	// End one
	HandleProgress(c, json.RawMessage(`{"token":"idx","value":{"kind":"end"}}`))

	assert.True(t, c.IsIndexing(), "still indexing — lint is active")
	assert.Len(t, c.GetActiveProgress(), 1)
	assert.Equal(t, "Linting", c.GetActiveProgress()[0].Title)

	// End the other
	HandleProgress(c, json.RawMessage(`{"token":"lint","value":{"kind":"end"}}`))

	assert.False(t, c.IsIndexing())
	assert.Empty(t, c.GetActiveProgress())
}

func TestProgressTokenNumeric(t *testing.T) {
	t.Parallel()
	c := newTestClient()

	// Some servers use numeric tokens
	HandleProgress(c, json.RawMessage(`{"token":42,"value":{"kind":"begin","title":"Building"}}`))

	assert.True(t, c.IsIndexing())
	active := c.GetActiveProgress()
	require.Len(t, active, 1)
	assert.Equal(t, "Building", active[0].Title)
}
