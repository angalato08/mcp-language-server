package lsp

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScanWorkspaceLanguages(t *testing.T) {
	t.Run("detects Go files", func(t *testing.T) {
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)

		langs := ScanWorkspaceLanguages(dir)
		assert.True(t, langs["go"])
	})

	t.Run("detects multiple languages", func(t *testing.T) {
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)
		_ = os.WriteFile(filepath.Join(dir, "lib.rs"), []byte("fn main() {}"), 0644)
		_ = os.WriteFile(filepath.Join(dir, "app.py"), []byte("print('hi')"), 0644)

		langs := ScanWorkspaceLanguages(dir)
		assert.True(t, langs["go"])
		assert.True(t, langs["rust"])
		assert.True(t, langs["python"])
	})

	t.Run("normalizes cpp to c", func(t *testing.T) {
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "main.cpp"), []byte("int main() {}"), 0644)

		langs := ScanWorkspaceLanguages(dir)
		assert.True(t, langs["c"])
		assert.False(t, langs["cpp"])
	})

	t.Run("normalizes typescriptreact to typescript", func(t *testing.T) {
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "App.tsx"), []byte("export default {}"), 0644)

		langs := ScanWorkspaceLanguages(dir)
		assert.True(t, langs["typescript"])
	})

	t.Run("skips excluded dirs", func(t *testing.T) {
		dir := t.TempDir()
		nodeModules := filepath.Join(dir, "node_modules")
		_ = os.MkdirAll(nodeModules, 0755)
		_ = os.WriteFile(filepath.Join(nodeModules, "dep.ts"), []byte(""), 0644)

		langs := ScanWorkspaceLanguages(dir)
		assert.Empty(t, langs)
	})

	t.Run("skips binary extensions", func(t *testing.T) {
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "image.png"), []byte{}, 0644)
		_ = os.WriteFile(filepath.Join(dir, "lib.so"), []byte{}, 0644)

		langs := ScanWorkspaceLanguages(dir)
		assert.Empty(t, langs)
	})

	t.Run("empty workspace", func(t *testing.T) {
		dir := t.TempDir()
		langs := ScanWorkspaceLanguages(dir)
		assert.Empty(t, langs)
	})

	t.Run("detects files in subdirectories", func(t *testing.T) {
		dir := t.TempDir()
		subDir := filepath.Join(dir, "src", "pkg")
		_ = os.MkdirAll(subDir, 0755)
		_ = os.WriteFile(filepath.Join(subDir, "main.go"), []byte("package pkg"), 0644)

		langs := ScanWorkspaceLanguages(dir)
		assert.True(t, langs["go"])
	})

	t.Run("ignores unsupported languages", func(t *testing.T) {
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "style.css"), []byte("body {}"), 0644)
		_ = os.WriteFile(filepath.Join(dir, "data.json"), []byte("{}"), 0644)

		langs := ScanWorkspaceLanguages(dir)
		assert.Empty(t, langs) // css and json have no known LSP servers in our registry
	})
}

func TestNormalizeLangID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"go", "go"},
		{"rust", "rust"},
		{"cpp", "c"},
		{"typescriptreact", "typescript"},
		{"javascriptreact", "javascript"},
		{"python", "python"},
		{"unknown", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, normalizeLangID(tt.input))
		})
	}
}

func TestDetectLSPConfigs(t *testing.T) {
	// Save and restore original lookPath
	origLookPath := lookPath
	defer func() { lookPath = origLookPath }()

	t.Run("finds available server", func(t *testing.T) {
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)

		lookPath = func(file string) (string, error) {
			if file == "gopls" {
				return "/usr/bin/gopls", nil
			}
			return "", exec.ErrNotFound
		}

		configs, err := DetectLSPConfigs(dir)
		require.NoError(t, err)
		require.Len(t, configs, 1)
		assert.Equal(t, "go", configs[0].LangID)
		assert.Equal(t, "gopls", configs[0].Command)
	})

	t.Run("picks higher priority server", func(t *testing.T) {
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "app.py"), []byte("print()"), 0644)

		lookPath = func(file string) (string, error) {
			// Both available — should pick pyright (priority 0)
			if file == "pyright-langserver" || file == "pylsp" {
				return "/usr/bin/" + file, nil
			}
			return "", exec.ErrNotFound
		}

		configs, err := DetectLSPConfigs(dir)
		require.NoError(t, err)
		require.Len(t, configs, 1)
		assert.Equal(t, "pyright-langserver", configs[0].Command)
		assert.Equal(t, []string{"--stdio"}, configs[0].Args)
	})

	t.Run("falls back to lower priority", func(t *testing.T) {
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "app.py"), []byte("print()"), 0644)

		lookPath = func(file string) (string, error) {
			if file == "pylsp" {
				return "/usr/bin/pylsp", nil
			}
			return "", exec.ErrNotFound
		}

		configs, err := DetectLSPConfigs(dir)
		require.NoError(t, err)
		require.Len(t, configs, 1)
		assert.Equal(t, "pylsp", configs[0].Command)
	})

	t.Run("deduplicates same server for JS and TS", func(t *testing.T) {
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "app.ts"), []byte(""), 0644)
		_ = os.WriteFile(filepath.Join(dir, "util.js"), []byte(""), 0644)

		lookPath = func(file string) (string, error) {
			if file == "typescript-language-server" {
				return "/usr/bin/typescript-language-server", nil
			}
			return "", exec.ErrNotFound
		}

		configs, err := DetectLSPConfigs(dir)
		require.NoError(t, err)
		// Should produce only one config even though both JS and TS were detected
		assert.Len(t, configs, 1)
		assert.Equal(t, "typescript-language-server", configs[0].Command)
	})

	t.Run("multiple languages multiple servers", func(t *testing.T) {
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)
		_ = os.WriteFile(filepath.Join(dir, "lib.rs"), []byte("fn main() {}"), 0644)

		lookPath = func(file string) (string, error) {
			if file == "gopls" || file == "rust-analyzer" {
				return "/usr/bin/" + file, nil
			}
			return "", exec.ErrNotFound
		}

		configs, err := DetectLSPConfigs(dir)
		require.NoError(t, err)
		assert.Len(t, configs, 2)

		// Configs should be sorted by language ID
		langIDs := []string{configs[0].LangID, configs[1].LangID}
		assert.Contains(t, langIDs, "go")
		assert.Contains(t, langIDs, "rust")
	})

	t.Run("error when no source files", func(t *testing.T) {
		dir := t.TempDir()
		_, err := DetectLSPConfigs(dir)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no supported source files")
	})

	t.Run("error when no servers installed", func(t *testing.T) {
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)

		lookPath = func(file string) (string, error) {
			return "", exec.ErrNotFound
		}

		_, err := DetectLSPConfigs(dir)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no LSP servers are installed")
	})
}
