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

// ConfigFileLanguages maps config filenames (dot-files) to the language ID
// of the LSP server that should be restarted when the file changes.
// These files are typically cached at server startup (e.g. clangd reads
// .clang-tidy once), so editing them requires a server restart.
var ConfigFileLanguages = map[string]string{
	".clang-tidy":   "c",
	".clangd":       "c",
	".clang-format": "c",
}
