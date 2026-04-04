package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/angalato08/mcp-language-server/internal/logging"
	"github.com/angalato08/mcp-language-server/internal/lsp"
	"github.com/angalato08/mcp-language-server/internal/tools"
	"github.com/angalato08/mcp-language-server/internal/watcher"
	"github.com/mark3labs/mcp-go/server"
)

// Set at build time via: go build -ldflags "-X main.version=..."
// Falls back to "dev" when built without ldflags.
var version = "dev"

// Create a logger for the core component
var coreLogger = logging.NewLogger(logging.Core)

const serverInstructions = `MCP Language Server — Tool Usage Guide

This server provides IDE-quality code intelligence via Language Server Protocol (LSP). Use these tools as your primary means of understanding and navigating code — they are faster and more accurate than reading raw files.

## Recommended Workflows

### Understanding a file's API
Use api_overview for a complete view of all symbols with signatures and docs in a single call:
  api_overview(filePath) → signatures + docs for every symbol

This replaces the pattern of document_symbols → hover → hover → hover...

Only fall back to document_symbols + individual hover calls if you need hover info for just 1-2 specific symbols.

### Understanding a specific symbol
1. hover(filePath, line, column) → type signature + documentation
2. get_definition(filePath, line, column) → jump to source code
3. get_references(filePath, line, column) → find all usages

### Finding symbols across the project
1. workspace_symbols(query) → search by name
2. definition(symbolName) → read source of a known symbol
3. references(symbolName) → find all usages of a known symbol

### Understanding call flow
1. incoming_calls → who calls this function?
2. outgoing_calls → what does this function call?
3. dependency_graph → visualize multi-level call chains

### Batch operations
Use batch_hover when you need hover info for multiple positions in the same file — it runs concurrently and is much more efficient than sequential hover calls.

### After editing code
Always call diagnostics(filePath) after modifying a file to catch errors immediately.

## Key Principles
- Prefer position-based tools (get_definition, get_references, hover) when you already have file + line + column
- Prefer name-based tools (definition, references, workspace_symbols) when you only know the symbol name
- Use api_overview as the first step when exploring an unfamiliar file
- Use short output format for references/calls when you only need locations, not code context`

// stringSlice implements flag.Value for multi-value --lsp flags.
type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ", ") }
func (s *stringSlice) Set(val string) error {
	*s = append(*s, val)
	return nil
}

type config struct {
	workspaceDir string
	lspConfigs   []lsp.LanguageConfig
}

type mcpServer struct {
	config           config
	router           *lsp.Router
	mcpServer        *server.MCPServer
	ctx              context.Context
	cancelFunc       context.CancelFunc
	workspaceWatcher *watcher.WorkspaceWatcher
}

// parseLSPFlags converts raw --lsp flag values and trailing args into LanguageConfigs.
// Supported syntaxes:
//   - "go:gopls"  → explicit lang:command
//   - "gopls"     → legacy, auto-detect via KnownLSPCommands
func parseLSPFlags(flags []string, trailingArgs []string) ([]lsp.LanguageConfig, error) {
	if len(flags) == 0 {
		return nil, fmt.Errorf("at least one --lsp flag is required")
	}

	var configs []lsp.LanguageConfig
	for _, f := range flags {
		var langID, command string
		if idx := strings.Index(f, ":"); idx > 0 {
			// Explicit syntax: "go:gopls" or "python:pyright-langserver --stdio"
			langID = f[:idx]
			command = f[idx+1:]
		} else {
			// Legacy syntax: just the command name
			command = f
			base := filepath.Base(command)
			if detected, ok := lsp.KnownLSPCommands[base]; ok {
				langID = detected
			} else {
				langID = "unknown"
			}
		}

		configs = append(configs, lsp.LanguageConfig{
			LangID:  langID,
			Command: command,
		})
	}

	// Trailing args (after --) apply to the last/only LSP config (backward compat)
	if len(trailingArgs) > 0 && len(configs) > 0 {
		configs[len(configs)-1].Args = trailingArgs
	}

	// Validate commands exist
	for _, cfg := range configs {
		if _, err := exec.LookPath(cfg.Command); err != nil {
			return nil, fmt.Errorf("LSP command not found: %s", cfg.Command)
		}
	}

	return configs, nil
}

func parseConfig() (*config, error) {
	cfg := &config{}
	var lspFlags stringSlice
	showVersion := flag.Bool("version", false, "Print version and exit")
	flag.StringVar(&cfg.workspaceDir, "workspace", ".", "Path to workspace directory (defaults to current directory)")
	flag.Var(&lspFlags, "lsp", "LSP server to use (optional — auto-detected if omitted). Format: lang:command or just command. Can be specified multiple times.")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		os.Exit(0)
	}

	workspaceDir, err := filepath.Abs(cfg.workspaceDir)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path for workspace: %v", err)
	}
	cfg.workspaceDir = workspaceDir

	if _, err := os.Stat(cfg.workspaceDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("workspace directory does not exist: %s", cfg.workspaceDir)
	}

	// Configure LSP servers: explicit flags or auto-detect
	if len(lspFlags) > 0 {
		lspConfigs, err := parseLSPFlags(lspFlags, flag.Args())
		if err != nil {
			return nil, err
		}
		cfg.lspConfigs = lspConfigs
	} else {
		lspConfigs, err := lsp.DetectLSPConfigs(cfg.workspaceDir)
		if err != nil {
			return nil, fmt.Errorf("auto-detection failed: %v\nHint: use --lsp to manually specify a language server", err)
		}
		cfg.lspConfigs = lspConfigs
	}

	return cfg, nil
}

func newServer(config *config) (*mcpServer, error) {
	ctx, cancel := context.WithCancel(context.Background())
	return &mcpServer{
		config:     *config,
		ctx:        ctx,
		cancelFunc: cancel,
	}, nil
}

func (s *mcpServer) initializeLSP() error {
	if err := os.Chdir(s.config.workspaceDir); err != nil {
		return fmt.Errorf("failed to change to workspace directory: %v", err)
	}

	tools.SetWorkspaceRoot(s.config.workspaceDir)

	router := lsp.NewRouter(s.ctx, s.config.workspaceDir, s.config.lspConfigs)
	s.router = router
	s.workspaceWatcher = watcher.NewWorkspaceWatcher(router)

	go s.workspaceWatcher.WatchWorkspace(s.ctx, s.config.workspaceDir)
	// No WaitForServerReady — clients start lazily on first tool call
	return nil
}

func (s *mcpServer) start() error {
	if err := s.initializeLSP(); err != nil {
		return err
	}

	s.mcpServer = server.NewMCPServer(
		"MCP Language Server",
		version,
		server.WithLogging(),
		server.WithRecovery(),
		server.WithInstructions(serverInstructions),
		server.WithToolCapabilities(true),
	)

	err := s.registerTools()
	if err != nil {
		return fmt.Errorf("tool registration failed: %v", err)
	}

	return server.ServeStdio(s.mcpServer)
}

func main() {
	coreLogger.Info("MCP Language Server %s starting", version)

	done := make(chan struct{})
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	config, err := parseConfig()
	if err != nil {
		coreLogger.Fatal("%v", err)
	}

	server, err := newServer(config)
	if err != nil {
		coreLogger.Fatal("%v", err)
	}

	// Parent process monitoring channel
	parentDeath := make(chan struct{})

	// Monitor parent process termination
	// Claude desktop does not properly kill child processes for MCP servers
	go func() {
		ppid := os.Getppid()
		coreLogger.Debug("Monitoring parent process: %d", ppid)

		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				currentPpid := os.Getppid()
				if currentPpid != ppid && (currentPpid == 1 || ppid == 1) {
					coreLogger.Info("Parent process %d terminated (current ppid: %d), initiating shutdown", ppid, currentPpid)
					close(parentDeath)
					return
				}
			case <-done:
				return
			}
		}
	}()

	// Handle shutdown triggers
	go func() {
		select {
		case sig := <-sigChan:
			coreLogger.Info("Received signal %v in PID: %d", sig, os.Getpid())
			cleanup(server, done)
		case <-parentDeath:
			coreLogger.Info("Parent death detected, initiating shutdown")
			cleanup(server, done)
		}
	}()

	if err := server.start(); err != nil {
		coreLogger.Error("Server error: %v", err)
		cleanup(server, done)
		os.Exit(1)
	}

	<-done
	coreLogger.Info("Server shutdown complete for PID: %d", os.Getpid())
	os.Exit(0)
}

func cleanup(s *mcpServer, done chan struct{}) {
	coreLogger.Info("Cleanup initiated for PID: %d", os.Getpid())

	// Create a context with timeout for shutdown operations
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if s.router != nil {
		coreLogger.Info("Closing all LSP clients via router")
		s.router.Close(ctx)
	}

	// Send signal to the done channel
	select {
	case <-done: // Channel already closed
	default:
		close(done)
	}

	coreLogger.Info("Cleanup completed for PID: %d", os.Getpid())
}
