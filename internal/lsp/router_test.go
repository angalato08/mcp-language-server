package lsp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRouterFindConfig(t *testing.T) {
	t.Parallel()

	configs := []LanguageConfig{
		{LangID: "go", Command: "gopls"},
		{LangID: "c", Command: "clangd"},
		{LangID: "typescript", Command: "typescript-language-server", Args: []string{"--stdio"}},
		{LangID: "python", Command: "pyright-langserver", Args: []string{"--stdio"}},
	}

	r := NewRouter(context.Background(), "/workspace", configs)

	tests := []struct {
		name      string
		langID    string
		wantCmd   string
		wantFound bool
	}{
		{"exact match go", "go", "gopls", true},
		{"exact match c", "c", "clangd", true},
		{"exact match typescript", "typescript", "typescript-language-server", true},
		{"language group cpp -> c", "cpp", "clangd", true},
		{"language group typescriptreact -> typescript", "typescriptreact", "typescript-language-server", true},
		{"language group javascriptreact (no javascript config)", "javascriptreact", "", false},
		{"unknown language", "ruby", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg, found := r.findConfig(tc.langID)
			assert.Equal(t, tc.wantFound, found)
			if found {
				require.NotNil(t, cfg)
				assert.Equal(t, tc.wantCmd, cfg.Command)
			}
		})
	}
}

func TestLanguageGroups(t *testing.T) {
	t.Parallel()

	// Verify the language group mappings
	assert.Equal(t, "c", languageGroups["cpp"])
	assert.Equal(t, "typescript", languageGroups["typescriptreact"])
	assert.Equal(t, "javascript", languageGroups["javascriptreact"])

	// Unknown language has no group
	_, ok := languageGroups["go"]
	assert.False(t, ok)
}

func TestRouterClientForFileUnknownLanguage(t *testing.T) {
	t.Parallel()

	r := NewRouter(context.Background(), "/workspace", nil)

	_, err := r.ClientForFile(context.Background(), "/workspace/data.xyz")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown language")
}

func TestRouterClientForFileNoConfig(t *testing.T) {
	t.Parallel()

	// Router with configs that don't match the file
	configs := []LanguageConfig{
		{LangID: "go", Command: "gopls"},
	}
	r := NewRouter(context.Background(), "/workspace", configs)

	_, err := r.ClientForFile(context.Background(), "/workspace/main.py")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no LSP server configured")
}

func TestRouterActiveClientsEmpty(t *testing.T) {
	t.Parallel()

	r := NewRouter(context.Background(), "/workspace", nil)
	clients := r.ActiveClients()
	assert.Empty(t, clients)
}

func TestNewRouter(t *testing.T) {
	t.Parallel()

	configs := []LanguageConfig{
		{LangID: "go", Command: "gopls"},
	}
	ctx := context.Background()
	r := NewRouter(ctx, "/workspace", configs)

	assert.NotNil(t, r.clients)
	assert.Equal(t, "/workspace", r.workspaceDir)
	assert.Len(t, r.configs, 1)
}
