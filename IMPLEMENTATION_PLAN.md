# Implementation Plan: Fork & Improve mcp-language-server

## Setup

```bash
# Fork on GitHub first (via web UI or gh cli)
gh repo fork isaacphi/mcp-language-server --clone --remote
cd mcp-language-server
go build ./...
go test ./...
```

---

## Design Decision: State Management for AI Agent Workflows

### The Problem

LSP is stateful. The server needs to know about file contents via `textDocument/didOpen` and `textDocument/didChange`. But when an AI agent (like Claude Code) edits files, it uses its own `Edit` tool — writing directly to disk, bypassing the MCP server entirely. This means:

- Agent fixes a bug → LSP still reports the old diagnostic
- Agent renames a variable with `Edit` → `references` returns stale locations
- Agent adds a new function → `definition` can't find it

### The Solution: Lazy Open + Lightweight Watcher

The architecture uses two complementary mechanisms:

1. **Lazy `didOpen` on query**: Each tool function (`definition`, `references`, `diagnostics`, etc.) calls `client.OpenFile()` before querying. This reads the file **from disk** and sends `didOpen` to the LSP. So if the agent edited the file between queries, the next query picks up the fresh content.

2. **File watcher for between-query changes**: The `fsnotify`-based watcher detects filesystem changes and sends `didChange` to the LSP. This keeps the LSP's internal index warm even when no tools are being called.

### Implementation Details

**What the watcher should do** (after Bug 3 fix):
- Watch for `fsnotify` events (create, modify, delete, rename)
- On modify: if the file is already open in the LSP, send `didChange` with fresh content from disk
- On create: do NOT send `didOpen` — wait until a tool queries it
- On delete: send `didClose` if it was open
- Debounce rapid changes (already implemented upstream)

**What the watcher should NOT do**:
- Eagerly `didOpen` every file in the workspace (current bug — causes FD exhaustion)
- Try to track in-memory state — disk is the source of truth

**Key insight for `OpenFile()`**: The current implementation skips `didOpen` if the file is already open:
```go
if _, exists := c.openFiles[uri]; exists {
    return nil // Already open
}
```
This is wrong for our use case. If the file is already open but was modified on disk by the agent, we need to send `didChange` instead. Fix:

```go
func (c *Client) OpenFile(ctx context.Context, filepath string) error {
    uri := fmt.Sprintf("file://%s", filepath)

    content, err := os.ReadFile(filepath)
    if err != nil {
        return fmt.Errorf("error reading file: %w", err)
    }

    c.openFilesMu.Lock()
    if _, exists := c.openFiles[uri]; exists {
        c.openFilesMu.Unlock()
        // File already open — send didChange with current disk content
        return c.NotifyChange(ctx, filepath)
    }
    c.openFilesMu.Unlock()

    // First time opening — send didOpen
    params := protocol.DidOpenTextDocumentParams{
        TextDocument: protocol.TextDocumentItem{
            URI:        protocol.DocumentUri(uri),
            LanguageID: DetectLanguageID(uri),
            Version:    1,
            Text:       string(content),
        },
    }
    // ... rest unchanged
}
```

Wait — this would send `didChange` on every tool call even if the file hasn't changed. That's wasteful. Better approach:

```go
func (c *Client) OpenFile(ctx context.Context, filepath string) error {
    uri := fmt.Sprintf("file://%s", filepath)

    content, err := os.ReadFile(filepath)
    if err != nil {
        return fmt.Errorf("error reading file: %w", err)
    }

    c.openFilesMu.RLock()
    _, exists := c.openFiles[uri]
    c.openFilesMu.RUnlock()

    if exists {
        // Already open — the watcher handles didChange for modified files.
        // If watcher missed it, the content may be stale, but this is rare
        // since fsnotify fires reliably for local edits.
        return nil
    }

    // First open — send didOpen with current disk content
    // ... existing code
}
```

The watcher is the right mechanism here. When Claude Code's `Edit` tool writes to disk, `fsnotify` fires, the watcher sends `didChange`, and the LSP updates its index. The next tool call gets fresh results.

**Edge case — watcher race condition**: If a tool call comes immediately after an `Edit` (before `fsnotify` fires), the LSP may have stale data. Mitigation: add a small yield (50ms) in tool functions, or check file mtime against the LSP's version. In practice, this is rarely a problem because the agent's tool calls are serialized and there's always a round-trip delay between `Edit` and the next MCP call.

### Testing State Management

```bash
# 1. Start MCP server on a Go project
# 2. Call definition("FooBar") — should return current definition
# 3. Edit the function signature in the file (via agent's Edit tool or manually)
# 4. Wait 1 second (for watcher to fire)
# 5. Call definition("FooBar") again — should reflect the edit
# 6. Call diagnostics on the file — should show any errors from the edit
```

---

## Design Decision: Dependency Management

### The Problem

The MCP server is useless without the underlying LSP binary (clangd, gopls, pyright, etc.). If the binary is missing, the user gets a cryptic exec error. This is the #1 friction point for first-time users.

### Implementation

#### Better startup error messages

**File**: `internal/lsp/client.go` → `NewClient()`

After `cmd.Start()` fails, detect "executable not found" and provide install instructions:

```go
if err := cmd.Start(); err != nil {
    if errors.Is(err, exec.ErrNotFound) {
        return nil, fmt.Errorf("LSP server '%s' not found.\n%s",
            command, installInstructions(command))
    }
    return nil, fmt.Errorf("failed to start LSP server: %w", err)
}
```

**New function** in `internal/lsp/languages.go`:

```go
var installGuides = map[string]string{
    "clangd": "Install clangd:\n  macOS:   brew install llvm\n  Ubuntu:  sudo apt install clangd\n  Docs:    https://clangd.llvm.org/installation",
    "gopls":  "Install gopls:\n  go install golang.org/x/tools/gopls@latest\n  Docs:    https://pkg.go.dev/golang.org/x/tools/gopls",
    "pyright-langserver": "Install pyright:\n  npm install -g pyright\n  pip install pyright\n  Docs:    https://github.com/microsoft/pyright",
    "pylsp":  "Install python-lsp-server:\n  pip install python-lsp-server\n  Docs:    https://github.com/python-lsp/python-lsp-server",
    "rust-analyzer": "Install rust-analyzer:\n  rustup component add rust-analyzer\n  Docs:    https://rust-analyzer.github.io/manual.html#installation",
    "typescript-language-server": "Install typescript-language-server:\n  npm install -g typescript-language-server typescript\n  Docs:    https://github.com/typescript-language-server/typescript-language-server",
}

func installInstructions(command string) string {
    baseName := filepath.Base(command)
    if guide, ok := installGuides[baseName]; ok {
        return guide
    }
    return fmt.Sprintf("Ensure '%s' is installed and available in your PATH.", command)
}
```

#### `check_health` MCP tool

**File**: `tools.go` — add a new tool registration:

```go
healthTool := mcp.NewTool("check_health",
    mcp.WithDescription("Check the status of configured LSP servers. Returns which servers are running, their versions, and any configuration issues."),
)

s.mcpServer.AddTool(healthTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    var sb strings.Builder
    sb.WriteString("LSP Server Health Check\n\n")

    // For single-language mode:
    client, err := s.lspClient.GetClient()
    if err != nil {
        sb.WriteString(fmt.Sprintf("Status: DOWN (%v)\n", err))
    } else {
        sb.WriteString(fmt.Sprintf("Status: Running\n"))
        sb.WriteString(fmt.Sprintf("Command: %s\n", client.Cmd.Path))
        sb.WriteString(fmt.Sprintf("PID: %d\n", client.Cmd.Process.Pid))
        sb.WriteString(fmt.Sprintf("Open files: %d\n", len(client.openFiles)))
    }

    // For multi-language mode (Phase 4):
    // Iterate over router.clients and report each

    return mcp.NewToolResultText(sb.String()), nil
})
```

### Files Modified/Created

| File | Changes |
|------|---------|
| `internal/lsp/client.go` | Better error message on startup failure |
| `internal/lsp/languages.go` | Add `installGuides` map and `installInstructions()` |
| `tools.go` | Register `check_health` tool |

### Testing

```bash
# Test missing binary:
./mcp-language-server --workspace /tmp/test --lsp nonexistent-lsp
# Should print: "LSP server 'nonexistent-lsp' not found. Ensure 'nonexistent-lsp' is installed..."

# Test with clangd not installed:
./mcp-language-server --workspace /tmp/test --lsp clangd
# Should print install instructions for clangd
```

---

## Phase 1: Bug Fixes

### Bug 1: WorkspaceEdit capabilities not declared (Issue #104)

**File**: `internal/lsp/client.go` → `InitializeLSPClient()`

**Problem**: The `Capabilities` struct in `initParams` doesn't declare `workspace.workspaceEdit`, so LSP servers fall back to single-file rename mode.

**Fix**: Add `WorkspaceEdit` capabilities inside the `Workspace` field of `ClientCapabilities`. Find this block (~line 110):

```go
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
    },
```

**Add** after `DidChangeWatchedFiles`:

```go
        WorkspaceEdit: protocol.WorkspaceEditClientCapabilities{
            DocumentChanges:    true,
            ResourceOperations: []protocol.ResourceOperationKind{
                protocol.Create,
                protocol.Rename,
                protocol.Delete,
            },
            FailureHandling: protocol.Abort,
        },
```

> Check the `protocol` package (`internal/protocol/tsprotocol.go`) to confirm the exact struct/field names. The types are auto-generated from the LSP spec so they should exist — search for `WorkspaceEditClientCapabilities`.

**Verify**: Run `go build ./...`. Then run the rename integration test:
```bash
go test ./integrationtests/tests/go/rename_symbol/... -v -update
```
The rename should now produce changes across multiple files.

---

### Bug 2: Hardcoded 3-second sleep in diagnostics

**File**: `internal/tools/diagnostics.go` → `GetDiagnosticsForFile()`

**Problem**: Line `time.Sleep(time.Second * 3)` blocks unconditionally instead of waiting for `textDocument/publishDiagnostics` notification.

**How it works now**: The `HandleDiagnostics` function in `internal/lsp/server-request-handlers.go` already receives `publishDiagnostics` notifications and stores them in `client.diagnostics`. But `GetDiagnosticsForFile` doesn't wait for it — it just sleeps 3 seconds.

**Fix** (3 parts):

#### Part A: Add a diagnostic notification channel to the Client

**File**: `internal/lsp/client.go`

Add a new field to `Client` struct:
```go
type Client struct {
    // ... existing fields ...

    // Signals when diagnostics are updated for a URI
    diagnosticReady   map[protocol.DocumentUri]chan struct{}
    diagnosticReadyMu sync.Mutex
}
```

Initialize in `NewClient()`:
```go
diagnosticReady: make(map[protocol.DocumentUri]chan struct{}),
```

Add a method:
```go
// WaitForDiagnostics waits for a publishDiagnostics notification for the given URI,
// or returns after timeout.
func (c *Client) WaitForDiagnostics(uri protocol.DocumentUri, timeout time.Duration) {
    c.diagnosticReadyMu.Lock()
    ch, exists := c.diagnosticReady[uri]
    if !exists {
        ch = make(chan struct{}, 1)
        c.diagnosticReady[uri] = ch
    }
    c.diagnosticReadyMu.Unlock()

    select {
    case <-ch:
    case <-time.After(timeout):
    }
}
```

#### Part B: Signal the channel when diagnostics arrive

**File**: `internal/lsp/server-request-handlers.go`

In `HandleDiagnostics()`, after storing diagnostics:
```go
func HandleDiagnostics(client *Client, params json.RawMessage) {
    var diagParams protocol.PublishDiagnosticsParams
    if err := json.Unmarshal(params, &diagParams); err != nil {
        // ...
        return
    }

    client.diagnosticsMu.Lock()
    client.diagnostics[diagParams.URI] = diagParams.Diagnostics
    client.diagnosticsMu.Unlock()

    // Signal waiters
    client.diagnosticReadyMu.Lock()
    if ch, ok := client.diagnosticReady[diagParams.URI]; ok {
        select {
        case ch <- struct{}{}:
        default:
        }
    }
    client.diagnosticReadyMu.Unlock()
}
```

#### Part C: Replace the sleep

**File**: `internal/tools/diagnostics.go`

Replace:
```go
// Wait for diagnostics
// TODO: wait for notification
time.Sleep(time.Second * 3)
```

With:
```go
// Wait for publishDiagnostics notification (with fallback timeout)
uri := protocol.DocumentUri("file://" + filePath)
client.WaitForDiagnostics(uri, 5*time.Second)
```

Move the `uri` variable declaration up (it's currently declared later in the function). Remove the duplicate declaration below.

**Verify**: Run diagnostics integration tests:
```bash
go test ./integrationtests/tests/go/diagnostics/... -v
go test ./integrationtests/tests/python/diagnostics/... -v
```
Should complete in <1s instead of 3s for clean files.

---

### Bug 3: File watcher FD exhaustion (Issue #83)

**File**: `internal/watcher/watcher.go`

**Problem**: The watcher eagerly calls `client.OpenFile()` for every file in the workspace during initial scan, sending `didOpen` with the full file content. On large codebases this exhausts file descriptors.

**Fix strategy**: Lazy `didOpen`. Don't open files during the watcher scan. Only open files when they're actually queried by a tool (definition, references, etc.). The watcher should only track file paths and send `didChangeWatchedFiles` notifications (not `didOpen`).

**Changes**:

1. In the watcher's initial file scan loop, **remove** the call to `client.OpenFile()`. The watcher should only:
   - Register file paths for fsnotify watching
   - Build the file path index
   - NOT send `didOpen` to the LSP server

2. Find the function that walks the workspace directory (look for `filepath.Walk` or `filepath.WalkDir` in `watcher.go`). It likely has something like:
   ```go
   client.OpenFile(ctx, path)
   ```
   Remove or comment out that line.

3. The individual tool functions (`definition.go`, `references.go`, etc.) already call `client.OpenFile()` before querying, so files will be opened on-demand.

4. **Optional LRU eviction**: Add a max open files limit. In `client.go`, after `OpenFile()` succeeds and adds to `openFiles`, check:
   ```go
   const maxOpenFiles = 500

   func (c *Client) maybeEvictFiles(ctx context.Context) {
       c.openFilesMu.Lock()
       if len(c.openFiles) <= maxOpenFiles {
           c.openFilesMu.Unlock()
           return
       }
       // Collect URIs to close (pick arbitrary ones, or track LRU)
       toClose := make([]string, 0)
       for uri := range c.openFiles {
           toClose = append(toClose, uri)
           if len(c.openFiles)-len(toClose) <= maxOpenFiles {
               break
           }
       }
       c.openFilesMu.Unlock()

       for _, uri := range toClose {
           filePath := strings.TrimPrefix(uri, "file://")
           c.CloseFile(ctx, filePath)
       }
   }
   ```
   Call `c.maybeEvictFiles(ctx)` at the end of `OpenFile()`.

**Verify**: Test on a large Python project (10k+ files). Monitor FD count:
```bash
lsof -p $(pgrep mcp-language-server) | wc -l
```

---

### Bug 4: Symbol resolution fragility (Issues #86, #121)

**File**: `internal/tools/lsp-utilities.go` (and `definition.go`, `references.go`)

**Problem**: The current symbol lookup uses `workspace/symbol` with the full symbol name, then filters results by exact name match. This fails for:
- TypeScript classes (returned as qualified names)
- Symbols with container names
- Symbols that the LSP returns with different casing/qualification

**Current matching logic** (in `lsp-utilities.go` or `definition.go`):
```go
// Pseudocode of current approach:
results, _ := client.Symbol(ctx, WorkspaceSymbolParams{Query: symbolName})
for _, sym := range results {
    if sym.Name == symbolName { ... }  // Exact match only
}
```

**Fix** — improve matching in multiple ways:

1. **Try both qualified and unqualified searches**: If `symbolName` contains a dot/`::`, also try searching for just the last component.

2. **Better match scoring**: Replace exact match with a scoring function:
   ```go
   func matchSymbol(sym protocol.SymbolInformation, query string) bool {
       // Exact match
       if sym.Name == query {
           return true
       }

       // Query is qualified (e.g., "MyClass.method")
       parts := strings.Split(query, ".")
       if len(parts) == 2 {
           // Match method name with container
           if sym.Name == parts[1] && sym.ContainerName == parts[0] {
               return true
           }
       }

       // Symbol might be qualified with :: (C++)
       cppParts := strings.Split(query, "::")
       if len(cppParts) == 2 {
           if sym.Name == cppParts[1] && strings.HasSuffix(sym.ContainerName, cppParts[0]) {
               return true
           }
       }

       // Container::Name match
       fullName := sym.ContainerName + "::" + sym.Name
       dotName := sym.ContainerName + "." + sym.Name
       if fullName == query || dotName == query {
           return true
       }

       return false
   }
   ```

3. **Fallback to file-based search**: If `workspace/symbol` returns nothing, and the user provides a file path context (or the symbol name includes a file hint), try `textDocument/documentSymbol` on likely files.

4. **Handle WorkspaceSymbol vs SymbolInformation**: The LSP spec has two return types for `workspace/symbol`. Check if the protocol package handles both (`[]SymbolInformation` and `[]WorkspaceSymbol`). The `Or_Result_workspace_symbol` type likely covers this — inspect it in `internal/protocol/tsprotocol.go`.

**Verify**:
```bash
go test ./integrationtests/tests/typescript/definition/... -v
go test ./integrationtests/tests/clangd/definition/... -v
```

---

## Phase 2: Response Trimming

### 2.1: Add `limit` parameter to `references` tool

**File**: `tools.go` (tool registration) and `internal/tools/references.go`

#### Tool registration change in `tools.go`:

Add a `limit` parameter to `findReferencesTool`:
```go
findReferencesTool := mcp.NewTool("references",
    mcp.WithDescription("Find all usages and references of a symbol throughout the codebase. Returns a list of all files and locations where the symbol appears."),
    mcp.WithString("symbolName",
        mcp.Required(),
        mcp.Description("The name of the symbol to search for"),
    ),
    mcp.WithNumber("limit",
        mcp.Description("Maximum number of references to return. Default 30. Use -1 for all."),
    ),
)
```

In the handler, extract the limit:
```go
limit := 30 // default
if limitArg, ok := request.Params.Arguments["limit"].(float64); ok {
    limit = int(limitArg)
}
```

Pass `limit` to `tools.FindReferences()`.

#### Function signature change in `internal/tools/references.go`:

```go
func FindReferences(ctx context.Context, client *lsp.Client, symbolName string, limit int) (string, error) {
```

After collecting all references, apply the limit:
```go
totalRefs := len(allReferences)
if limit > 0 && totalRefs > limit {
    allReferences = allReferences[:limit]
}

// Add count notice at the top of the result
if limit > 0 && totalRefs > limit {
    result = fmt.Sprintf("Showing %d of %d references\n\n", limit, totalRefs) + result
}
```

### 2.2: Add `limit` parameter to `diagnostics` tool

Same pattern. In `tools.go`, add:
```go
mcp.WithNumber("limit",
    mcp.Description("Maximum number of diagnostics to return. Default 20."),
),
```

In `internal/tools/diagnostics.go`, after getting diagnostics:
```go
totalDiags := len(diagnostics)
if limit > 0 && totalDiags > limit {
    diagnostics = diagnostics[:limit]
    // Append notice
}
```

### 2.3: Convert absolute paths to project-relative paths

**File**: `internal/tools/utilities.go` — add a helper:

```go
var workspaceRoot string // Set during initialization

func RelativePath(absPath string) string {
    if workspaceRoot != "" {
        if rel, err := filepath.Rel(workspaceRoot, absPath); err == nil {
            return rel
        }
    }
    return absPath
}
```

Set `workspaceRoot` during server initialization (pass it through from `main.go`).

Use `RelativePath()` everywhere paths appear in tool output:
- `references.go`: file headers
- `diagnostics.go`: file header and diagnostic locations
- `definition.go`: location output
- `hover.go`: location output
- `rename-symbol.go`: change summary

### 2.4: Drop column numbers from output

In the formatting sections of each tool, change patterns like:
```go
fmt.Sprintf("L%d:C%d", line+1, char+1)
```
to:
```go
fmt.Sprintf("L%d", line+1)
```

### 2.5: Cap total response size

**File**: `internal/tools/utilities.go` — add:

```go
const MaxResponseSize = 8000 // characters

func TrimResponse(response string) string {
    if len(response) <= MaxResponseSize {
        return response
    }
    return response[:MaxResponseSize] + "\n\n... (response truncated, use limit parameter to see specific results)"
}
```

Call `TrimResponse()` in each tool handler in `tools.go` before returning:
```go
text = tools.TrimResponse(text)
return mcp.NewToolResultText(text), nil
```

**Verify**: Test with a symbol that has 100+ references:
```bash
# In the integration test workspace, pick a commonly-used symbol
go test ./integrationtests/tests/go/references/... -v -update
```

---

## Phase 3: LSP Server Resilience

### 3.1: Detect LSP server process exit

**File**: `internal/lsp/client.go`

Add a process monitor goroutine in `NewClient()`:

```go
// Monitor LSP server process
client.dead = make(chan struct{})
go func() {
    err := cmd.Wait()  // This blocks until the process exits
    client.deadErr = err
    close(client.dead)
}()
```

Wait — `cmd.Wait()` is already called in `Close()`. So you need a different approach. Instead, monitor the process:

```go
type Client struct {
    // ... existing fields ...
    dead      chan struct{}
    deadErr   error
    restarting atomic.Bool
}
```

In `NewClient`, after `cmd.Start()`:
```go
client.dead = make(chan struct{})
go func() {
    // Wait for the process to exit (but don't call cmd.Wait since Close() does that)
    // Instead, monitor the PID
    for {
        if client.Cmd.Process == nil {
            return
        }
        // Check if process is still alive
        err := client.Cmd.Process.Signal(syscall.Signal(0))
        if err != nil {
            client.deadErr = err
            close(client.dead)
            return
        }
        time.Sleep(500 * time.Millisecond)
    }
}()
```

Actually, a cleaner approach: wrap `cmd.Wait()` and separate it from `Close()`:

```go
// In NewClient, replace the direct cmd.Wait in Close:
go func() {
    err := cmd.Wait()
    client.deadErr = err
    close(client.dead)
}()
```

Then update `Close()` to just wait on the `dead` channel instead of calling `cmd.Wait()`.

### 3.2: Auto-restart with backoff

**New file**: `internal/lsp/lifecycle.go`

```go
package lsp

import (
    "context"
    "fmt"
    "math"
    "sync"
    "time"
)

type RestartableClient struct {
    mu           sync.RWMutex
    client       *Client
    command      string
    args         []string
    workspaceDir string
    initFunc     func(ctx context.Context, client *Client) error

    // Previously opened files to replay after restart
    previousFiles []string

    restartCount int
    maxBackoff   time.Duration
}

func NewRestartableClient(command string, args []string, workspaceDir string) *RestartableClient {
    return &RestartableClient{
        command:      command,
        args:         args,
        workspaceDir: workspaceDir,
        maxBackoff:   30 * time.Second,
    }
}

func (rc *RestartableClient) Start(ctx context.Context) error {
    return rc.startClient(ctx)
}

func (rc *RestartableClient) startClient(ctx context.Context) error {
    client, err := NewClient(rc.command, rc.args...)
    if err != nil {
        return err
    }

    _, err = client.InitializeLSPClient(ctx, rc.workspaceDir)
    if err != nil {
        client.Close()
        return err
    }

    rc.mu.Lock()
    rc.client = client
    rc.mu.Unlock()

    // Monitor for crashes
    go rc.monitor(ctx)

    return nil
}

func (rc *RestartableClient) monitor(ctx context.Context) {
    rc.mu.RLock()
    dead := rc.client.dead
    rc.mu.RUnlock()

    select {
    case <-dead:
        lspLogger.Warn("LSP server process died, attempting restart")
        rc.restart(ctx)
    case <-ctx.Done():
        return
    }
}

func (rc *RestartableClient) restart(ctx context.Context) {
    rc.restartCount++
    backoff := time.Duration(math.Min(
        float64(time.Second)*math.Pow(2, float64(rc.restartCount-1)),
        float64(rc.maxBackoff),
    ))

    lspLogger.Info("Restarting LSP server in %v (attempt %d)", backoff, rc.restartCount)
    time.Sleep(backoff)

    // Save list of previously opened files
    rc.mu.RLock()
    oldClient := rc.client
    rc.mu.RUnlock()

    var filesToReopen []string
    if oldClient != nil {
        oldClient.openFilesMu.RLock()
        for uri := range oldClient.openFiles {
            filesToReopen = append(filesToReopen, strings.TrimPrefix(uri, "file://"))
        }
        oldClient.openFilesMu.RUnlock()
    }

    if err := rc.startClient(ctx); err != nil {
        lspLogger.Error("Failed to restart LSP server: %v", err)
        // Try again with more backoff
        rc.restart(ctx)
        return
    }

    // Re-open previously opened files
    rc.mu.RLock()
    newClient := rc.client
    rc.mu.RUnlock()

    for _, f := range filesToReopen {
        if err := newClient.OpenFile(ctx, f); err != nil {
            lspLogger.Warn("Failed to reopen file after restart: %s: %v", f, err)
        }
    }

    rc.restartCount = 0 // Reset on successful restart
    lspLogger.Info("LSP server restarted successfully, reopened %d files", len(filesToReopen))
}

// GetClient returns the current client. Returns error if server is restarting.
func (rc *RestartableClient) GetClient() (*Client, error) {
    rc.mu.RLock()
    defer rc.mu.RUnlock()

    if rc.client == nil {
        return nil, fmt.Errorf("LSP server is not running")
    }

    select {
    case <-rc.client.dead:
        return nil, fmt.Errorf("LSP server is restarting, please retry in a few seconds")
    default:
        return rc.client, nil
    }
}
```

### 3.3: Integrate RestartableClient into main.go

**File**: `main.go`

Replace `s.lspClient` (type `*lsp.Client`) with `*lsp.RestartableClient`. Update `mcpServer` struct:

```go
type mcpServer struct {
    ctx        context.Context
    cancel     context.CancelFunc
    lspClient  *lsp.RestartableClient  // was *lsp.Client
    mcpServer  *server.MCPServer
}
```

In `initializeLSP()`, replace:
```go
client, err := lsp.NewClient(command, args...)
// ...
client.InitializeLSPClient(ctx, workspaceDir)
```
With:
```go
rc := lsp.NewRestartableClient(command, args, workspaceDir)
if err := rc.Start(ctx); err != nil {
    return err
}
s.lspClient = rc
```

In `tools.go`, each tool handler should get the client:
```go
client, err := s.lspClient.GetClient()
if err != nil {
    return mcp.NewToolResultError(err.Error()), nil
}
```

**Verify**:
```bash
# Start the MCP server, then:
kill -9 $(pgrep clangd)  # Kill the underlying LSP
# Wait a few seconds, then call a tool — should auto-recover
```

---

## Phase 4: Multi-Language Support

### 4.1: Language router

**New file**: `internal/lsp/router.go`

```go
package lsp

import (
    "context"
    "fmt"
    "path/filepath"
    "strings"
    "sync"
)

// LanguageConfig maps a language ID to its LSP server command
type LanguageConfig struct {
    LangID  string   // e.g., "go", "python", "cpp"
    Command string   // e.g., "gopls"
    Args    []string // e.g., ["-remote=auto"]
}

// Router manages multiple LSP clients, one per language
type Router struct {
    mu           sync.RWMutex
    clients      map[string]*RestartableClient // keyed by langID
    configs      []LanguageConfig
    workspaceDir string
}

func NewRouter(workspaceDir string, configs []LanguageConfig) *Router {
    return &Router{
        clients:      make(map[string]*RestartableClient),
        configs:      configs,
        workspaceDir: workspaceDir,
    }
}

// ClientForFile returns the appropriate LSP client for a file path.
// Starts the LSP server lazily on first access.
func (r *Router) ClientForFile(ctx context.Context, filePath string) (*Client, error) {
    langID := string(DetectLanguageID(filePath))
    if langID == "" {
        return nil, fmt.Errorf("unsupported file type: %s", filepath.Ext(filePath))
    }
    return r.ClientForLang(ctx, langID)
}

// ClientForLang returns the LSP client for a language, starting it lazily.
func (r *Router) ClientForLang(ctx context.Context, langID string) (*Client, error) {
    r.mu.RLock()
    rc, exists := r.clients[langID]
    r.mu.RUnlock()

    if exists {
        return rc.GetClient()
    }

    // Find config for this language
    config, err := r.findConfig(langID)
    if err != nil {
        return nil, err
    }

    // Start the client
    r.mu.Lock()
    // Double-check after acquiring write lock
    if rc, exists := r.clients[langID]; exists {
        r.mu.Unlock()
        return rc.GetClient()
    }

    rc = NewRestartableClient(config.Command, config.Args, r.workspaceDir)
    r.clients[langID] = rc
    r.mu.Unlock()

    if err := rc.Start(ctx); err != nil {
        r.mu.Lock()
        delete(r.clients, langID)
        r.mu.Unlock()
        return nil, fmt.Errorf("failed to start LSP for %s: %w", langID, err)
    }

    return rc.GetClient()
}

func (r *Router) findConfig(langID string) (*LanguageConfig, error) {
    for _, c := range r.configs {
        if c.LangID == langID {
            return &c, nil
        }
    }
    return nil, fmt.Errorf("no LSP server configured for language: %s", langID)
}

// Shutdown stops all LSP clients
func (r *Router) Shutdown(ctx context.Context) {
    r.mu.Lock()
    defer r.mu.Unlock()
    for langID, rc := range r.clients {
        client, err := rc.GetClient()
        if err == nil {
            client.Close()
        }
        delete(r.clients, langID)
    }
}
```

### 4.2: Extension-to-language mapping for config auto-detection

**New file**: `internal/lsp/languages.go`

```go
package lsp

// ExtensionToLangID returns the LSP language ID for common extensions.
// Used for auto-detecting which LSP to route to.
var ExtensionToLangID = map[string]string{
    ".go":   "go",
    ".py":   "python",
    ".rs":   "rust",
    ".ts":   "typescript",
    ".tsx":  "typescriptreact",
    ".js":   "javascript",
    ".jsx":  "javascriptreact",
    ".c":    "c",
    ".cpp":  "cpp",
    ".cxx":  "cpp",
    ".cc":   "cpp",
    ".h":    "c",       // Ambiguous, but c is the common default
    ".hpp":  "cpp",
    ".java": "java",
    ".rb":   "ruby",
    ".php":  "php",
    ".cs":   "csharp",
    ".swift":"swift",
    ".kt":  "kotlin",
    ".lua":  "lua",
    ".zig":  "zig",
}

// KnownLSPCommands maps common command names to their language.
// Used for backward-compatible single --lsp flag auto-detection.
var KnownLSPCommands = map[string]string{
    "gopls":                     "go",
    "pyright-langserver":        "python",
    "pylsp":                     "python",
    "rust-analyzer":             "rust",
    "clangd":                    "cpp",  // handles both c and cpp
    "typescript-language-server": "typescript",
    "tsserver":                  "typescript",
}
```

### 4.3: CLI syntax changes

**File**: `main.go`

Add support for `--lsp lang:command` syntax while keeping backward compatibility:

```go
// New flag parsing
type lspFlag struct {
    lang    string
    command string
    args    []string
}

func parseLSPFlags(flags []string) ([]LanguageConfig, error) {
    var configs []LanguageConfig
    for _, f := range flags {
        if strings.Contains(f, ":") {
            // New syntax: lang:command
            parts := strings.SplitN(f, ":", 2)
            cmdParts := strings.Fields(parts[1])
            configs = append(configs, LanguageConfig{
                LangID:  parts[0],
                Command: cmdParts[0],
                Args:    cmdParts[1:],
            })
        } else {
            // Legacy syntax: bare command, auto-detect language
            cmdParts := strings.Fields(f)
            cmdName := filepath.Base(cmdParts[0])
            langID, ok := KnownLSPCommands[cmdName]
            if !ok {
                langID = "unknown"
            }
            configs = append(configs, LanguageConfig{
                LangID:  langID,
                Command: cmdParts[0],
                Args:    cmdParts[1:],
            })
        }
    }
    return configs, nil
}
```

Support multiple `--lsp` flags. In Go's `flag` package, use a custom flag type:

```go
type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ", ") }
func (s *stringSlice) Set(val string) error {
    *s = append(*s, val)
    return nil
}

var lspFlags stringSlice
flag.Var(&lspFlags, "lsp", "LSP server command. Use lang:command for multi-language (e.g., --lsp go:gopls --lsp py:'pyright-langserver --stdio')")
```

### 4.4: Update mcpServer struct and tool handlers

**File**: `main.go`, `tools.go`

Replace the single `lspClient` with the router:

```go
type mcpServer struct {
    ctx       context.Context
    cancel    context.CancelFunc
    router    *lsp.Router
    mcpServer *server.MCPServer
}
```

In `tools.go`, each tool that takes a `filePath` should use:
```go
client, err := s.router.ClientForFile(ctx, filePath)
```

Tools that take a `symbolName` (like `definition`, `references`) need a way to determine the language. Options:
- Add an optional `filePath` parameter to `definition` and `references` tools for routing
- Or query all configured LSP clients and merge results
- **Recommended**: Add optional `languageId` parameter, and also try all clients if not specified

```go
// In tools.go, for definition/references:
var client *lsp.Client
if langID, ok := request.Params.Arguments["languageId"].(string); ok && langID != "" {
    client, err = s.router.ClientForLang(ctx, langID)
} else {
    // Try all configured clients, return first success
    // (implement a TryAll method on Router)
}
```

**Verify**: Configure with two LSPs:
```bash
./mcp-language-server --workspace /path/to/project \
    --lsp go:gopls \
    --lsp python:"pyright-langserver --stdio"
```
Query a Go definition and a Python definition.

---

## Phase 5: New MCP Tools

### 5.1: `document_symbols` tool

**New file**: `internal/tools/symbols.go`

```go
package tools

import (
    "context"
    "fmt"
    "strings"

    "github.com/isaacphi/mcp-language-server/internal/lsp"
    "github.com/isaacphi/mcp-language-server/internal/protocol"
)

func GetDocumentSymbols(ctx context.Context, client *lsp.Client, filePath string) (string, error) {
    err := client.OpenFile(ctx, filePath)
    if err != nil {
        return "", fmt.Errorf("could not open file: %v", err)
    }

    uri := protocol.DocumentUri("file://" + filePath)
    params := protocol.DocumentSymbolParams{
        TextDocument: protocol.TextDocumentIdentifier{URI: uri},
    }

    result, err := client.DocumentSymbol(ctx, params)
    if err != nil {
        return "", fmt.Errorf("documentSymbol request failed: %v", err)
    }

    // result is Or_Result_textDocument_documentSymbol which can be
    // []DocumentSymbol or []SymbolInformation
    // Check which variant we got and format accordingly

    var sb strings.Builder
    sb.WriteString(fmt.Sprintf("Symbols in %s:\n\n", RelativePath(filePath)))

    // Try DocumentSymbol (hierarchical) first
    // The exact type assertion depends on how the protocol package defines the Or type
    // Inspect internal/protocol/tsprotocol.go for the exact field names

    formatDocumentSymbols(&sb, result, 0)

    return sb.String(), nil
}

func formatDocumentSymbols(sb *strings.Builder, symbols interface{}, indent int) {
    // This needs to handle the Or_Result_textDocument_documentSymbol type
    // which wraps either []DocumentSymbol or []SymbolInformation
    //
    // For DocumentSymbol (hierarchical):
    //   Name, Kind, Range, Children []DocumentSymbol
    //
    // For SymbolInformation (flat):
    //   Name, Kind, Location, ContainerName

    prefix := strings.Repeat("  ", indent)

    // Type-switch based on the protocol package's Or type
    // Pseudo-code — adapt to actual types:
    switch v := symbols.(type) {
    case []protocol.DocumentSymbol:
        for _, sym := range v {
            kind := symbolKindString(sym.Kind)
            line := sym.Range.Start.Line + 1
            sb.WriteString(fmt.Sprintf("%s%s %s (L%d)\n", prefix, kind, sym.Name, line))
            if len(sym.Children) > 0 {
                formatDocumentSymbols(sb, sym.Children, indent+1)
            }
        }
    case []protocol.SymbolInformation:
        for _, sym := range v {
            kind := symbolKindString(sym.Kind)
            line := sym.Location.Range.Start.Line + 1
            container := ""
            if sym.ContainerName != "" {
                container = fmt.Sprintf(" [%s]", sym.ContainerName)
            }
            sb.WriteString(fmt.Sprintf("%s%s %s%s (L%d)\n", prefix, kind, sym.Name, container, line))
        }
    }
}

func symbolKindString(kind protocol.SymbolKind) string {
    switch kind {
    case protocol.File:           return "file"
    case protocol.Module:         return "module"
    case protocol.Namespace:      return "namespace"
    case protocol.Package:        return "package"
    case protocol.Class:          return "class"
    case protocol.Method:         return "method"
    case protocol.Property:       return "property"
    case protocol.Field:          return "field"
    case protocol.Constructor:    return "constructor"
    case protocol.Enum:           return "enum"
    case protocol.Interface:      return "interface"
    case protocol.Function:       return "function"
    case protocol.Variable:       return "variable"
    case protocol.Constant:       return "constant"
    case protocol.String:         return "string"
    case protocol.Number:         return "number"
    case protocol.Boolean:        return "boolean"
    case protocol.Array:          return "array"
    case protocol.Object:         return "object"
    case protocol.Key:            return "key"
    case protocol.Null:           return "null"
    case protocol.EnumMember:     return "enum-member"
    case protocol.Struct:         return "struct"
    case protocol.Event:          return "event"
    case protocol.Operator:       return "operator"
    case protocol.TypeParameter:  return "type-param"
    default:                      return "symbol"
    }
}
```

> **Important**: The exact type handling for `Or_Result_textDocument_documentSymbol` depends on how the protocol package implements union types. Look at how `definition.go` handles `Or_Result_textDocument_definition` for the pattern to follow.

### 5.2: `workspace_symbols` tool

Add to `internal/tools/symbols.go`:

```go
func SearchWorkspaceSymbols(ctx context.Context, client *lsp.Client, query string, limit int) (string, error) {
    params := protocol.WorkspaceSymbolParams{
        Query: query,
    }

    result, err := client.Symbol(ctx, params)
    if err != nil {
        return "", fmt.Errorf("workspace/symbol request failed: %v", err)
    }

    // result is Or_Result_workspace_symbol
    // Extract symbols and format

    var sb strings.Builder
    sb.WriteString(fmt.Sprintf("Workspace symbols matching '%s':\n\n", query))

    // Format results (adapt type handling to match protocol package)
    count := 0
    // Iterate over result symbols, format each as:
    // kind Name [container] (file:line)

    if count == 0 {
        return fmt.Sprintf("No symbols found matching '%s'", query), nil
    }

    return sb.String(), nil
}
```

### 5.3: Register the new tools

**File**: `tools.go`

Add in `registerTools()`:

```go
documentSymbolsTool := mcp.NewTool("document_symbols",
    mcp.WithDescription("List all symbols (functions, classes, variables, etc.) in a file with their hierarchy. Useful for understanding file structure."),
    mcp.WithString("filePath",
        mcp.Required(),
        mcp.Description("Path to the file to list symbols for"),
    ),
)

s.mcpServer.AddTool(documentSymbolsTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    filePath, ok := request.Params.Arguments["filePath"].(string)
    if !ok {
        return mcp.NewToolResultError("filePath must be a string"), nil
    }
    text, err := tools.GetDocumentSymbols(s.ctx, s.lspClient, filePath)
    if err != nil {
        return mcp.NewToolResultError(fmt.Sprintf("failed to get symbols: %v", err)), nil
    }
    return mcp.NewToolResultText(text), nil
})

workspaceSymbolsTool := mcp.NewTool("workspace_symbols",
    mcp.WithDescription("Search for symbols across the entire project by name. Returns matching functions, classes, variables, etc. with their locations."),
    mcp.WithString("query",
        mcp.Required(),
        mcp.Description("Symbol name or partial name to search for"),
    ),
    mcp.WithNumber("limit",
        mcp.Description("Maximum number of results to return. Default 20."),
    ),
)

s.mcpServer.AddTool(workspaceSymbolsTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    query, ok := request.Params.Arguments["query"].(string)
    if !ok {
        return mcp.NewToolResultError("query must be a string"), nil
    }
    limit := 20
    if limitArg, ok := request.Params.Arguments["limit"].(float64); ok {
        limit = int(limitArg)
    }
    text, err := tools.SearchWorkspaceSymbols(s.ctx, s.lspClient, query, limit)
    if err != nil {
        return mcp.NewToolResultError(fmt.Sprintf("failed to search symbols: %v", err)), nil
    }
    return mcp.NewToolResultText(text), nil
})
```

**Verify**:
```bash
go build ./...
# Test manually via Claude Desktop or MCP inspector
```

---

## Summary of All Files Modified/Created

### Modified
| File | Changes |
|------|---------|
| `main.go` | Multi-language CLI flags, Router integration, RestartableClient |
| `tools.go` | New tool registrations (document_symbols, workspace_symbols, check_health), limit params, Router usage |
| `internal/lsp/client.go` | WorkspaceEdit capabilities, diagnostic channels, dead channel, maybeEvictFiles, better startup errors |
| `internal/lsp/server-request-handlers.go` | Signal diagnostic channel on notification |
| `internal/tools/diagnostics.go` | Replace sleep with WaitForDiagnostics, add limit |
| `internal/tools/references.go` | Add limit param, relative paths |
| `internal/tools/definition.go` | Relative paths, drop column numbers |
| `internal/tools/hover.go` | Relative paths |
| `internal/tools/rename-symbol.go` | Relative paths |
| `internal/tools/lsp-utilities.go` | Improved symbol matching |
| `internal/tools/utilities.go` | RelativePath(), TrimResponse() helpers |
| `internal/watcher/watcher.go` | Remove eager didOpen, keep lightweight fsnotify + didChange for open files |

### New Files
| File | Purpose |
|------|---------|
| `internal/lsp/lifecycle.go` | RestartableClient with crash recovery + backoff |
| `internal/lsp/router.go` | Multi-language routing |
| `internal/lsp/languages.go` | Extension → language mappings, known LSP commands, install guides |
| `internal/tools/symbols.go` | document_symbols + workspace_symbols tool implementations |

---

## Testing Checklist

```
[ ] go build ./...                                    — compiles
[ ] go test ./...                                     — unit tests pass
[ ] go test ./integrationtests/tests/go/...           — Go tests pass
[ ] go test ./integrationtests/tests/python/...       — Python tests pass
[ ] go test ./integrationtests/tests/clangd/...       — C++ tests pass
[ ] go test ./integrationtests/tests/rust/...         — Rust tests pass
[ ] go test ./integrationtests/tests/typescript/...   — TS tests pass
[ ] Manual: rename across multiple files works         — Bug 1
[ ] Manual: diagnostics return fast                    — Bug 2
[ ] Manual: large workspace doesn't exhaust FDs        — Bug 3
[ ] Manual: TS class definition resolves               — Bug 4
[ ] Manual: references with limit=5 shows "5 of N"    — Feature 1
[ ] Manual: kill -9 LSP, auto-restarts                 — Feature 2
[ ] Manual: --lsp go:gopls --lsp py:pyright works     — Feature 3
[ ] Manual: document_symbols returns hierarchy         — Feature 4
[ ] Manual: workspace_symbols searches project         — Feature 4
[ ] Manual: edit file externally, next query is fresh  — State mgmt
[ ] Manual: missing LSP binary shows install guide     — Dependency mgmt
[ ] Manual: check_health returns server status         — Dependency mgmt
```
