package lsp

// KnownLSPCommands maps command basenames to language IDs.
// Used for backward-compat: --lsp gopls → auto-detect as "go".
var KnownLSPCommands = map[string]string{
	"gopls":                      "go",
	"pyright-langserver":         "python",
	"pylsp":                      "python",
	"rust-analyzer":              "rust",
	"clangd":                     "c",
	"typescript-language-server": "typescript",
	"tsserver":                   "typescript",
}

// LSPServerInfo describes a known LSP server for auto-detection.
type LSPServerInfo struct {
	Command     string   // binary name (e.g., "gopls")
	Args        []string // default arguments (e.g., ["--stdio"])
	Priority    int      // lower = preferred when multiple servers exist for a language
	InstallHint string   // shown when binary not found
}

// KnownLSPServers maps language IDs to their known LSP server options.
// Used by auto-detection to find the best available server for each language.
var KnownLSPServers = map[string][]LSPServerInfo{
	"go":   {{Command: "gopls", Priority: 0, InstallHint: "go install golang.org/x/tools/gopls@latest"}},
	"rust": {{Command: "rust-analyzer", Priority: 0, InstallHint: "rustup component add rust-analyzer"}},
	"python": {
		{Command: "pyright-langserver", Args: []string{"--stdio"}, Priority: 0, InstallHint: "npm install -g pyright"},
		{Command: "pylsp", Priority: 1, InstallHint: "pip install python-lsp-server"},
	},
	"c":          {{Command: "clangd", Priority: 0, InstallHint: "apt install clangd / brew install llvm"}},
	"typescript": {{Command: "typescript-language-server", Args: []string{"--stdio"}, Priority: 0, InstallHint: "npm install -g typescript-language-server typescript"}},
}

// ConfigFileLanguages maps config filenames (dot-files) to the language ID
// of the LSP server that should be restarted when the file changes.
// These files are typically cached at server startup (e.g. clangd reads
// .clang-tidy once), so editing them requires a server restart.
var ConfigFileLanguages = map[string]string{
	".clang-tidy":   "c",
	".clangd":       "c",
	".clang-format": "c",
}
