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
