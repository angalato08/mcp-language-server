package lsp

import (
	"fmt"
	"io/fs"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/angalato08/mcp-language-server/internal/watcher/common"
)

// lookPath is a variable so tests can mock it.
var lookPath = exec.LookPath

// ScanWorkspaceLanguages walks the workspace and returns the set of language IDs
// that have at least one source file present. It skips excluded directories,
// binary files, and respects .gitignore.
func ScanWorkspaceLanguages(workspaceDir string) map[string]bool {
	cfg := common.DefaultWatcherConfig()
	gitMatcher, _ := common.NewGitignoreMatcher(workspaceDir)

	// We only care about languages that have a known LSP server.
	// Build the set of language IDs we're looking for.
	targetLangs := make(map[string]bool)
	for langID := range KnownLSPServers {
		targetLangs[langID] = true
	}
	// Also include group aliases (e.g., "cpp" resolves to "c" which is a target).
	for alias, primary := range languageGroups {
		if targetLangs[primary] {
			targetLangs[alias] = true
		}
	}

	found := make(map[string]bool)
	totalTargets := len(KnownLSPServers) // number of distinct server configs

	_ = filepath.WalkDir(workspaceDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip errors
		}

		if d.IsDir() {
			name := d.Name()
			if cfg.ExcludedDirs[name] {
				return filepath.SkipDir
			}
			if gitMatcher != nil && gitMatcher.IsIgnored(path) {
				return filepath.SkipDir
			}
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if cfg.ExcludedFileExtensions[ext] || cfg.LargeBinaryExtensions[ext] {
			return nil
		}

		if gitMatcher != nil && gitMatcher.IsIgnored(path) {
			return nil
		}

		langID := string(DetectLanguageID(path))
		if langID == "" {
			return nil
		}

		// Normalize to primary language (e.g., "cpp" → "c")
		normalized := normalizeLangID(langID)
		if _, hasServer := KnownLSPServers[normalized]; !hasServer {
			return nil // language detected but no known server
		}

		if !found[normalized] {
			found[normalized] = true
			if len(found) >= totalTargets {
				return filepath.SkipAll // found all possible languages
			}
		}

		return nil
	})

	return found
}

// normalizeLangID maps language group aliases to their primary language ID.
// e.g., "cpp" → "c", "typescriptreact" → "typescript", "javascriptreact" → "javascript".
// If the langID already has a KnownLSPServers entry, it's returned as-is.
func normalizeLangID(langID string) string {
	if _, ok := KnownLSPServers[langID]; ok {
		return langID
	}
	if primary, ok := languageGroups[langID]; ok {
		return primary
	}
	return langID
}

// DetectLSPConfigs scans the workspace for source files and returns LSP configs
// for each detected language that has an available server on PATH.
func DetectLSPConfigs(workspaceDir string) ([]LanguageConfig, error) {
	detected := ScanWorkspaceLanguages(workspaceDir)
	if len(detected) == 0 {
		return nil, fmt.Errorf("no supported source files found in workspace: %s", workspaceDir)
	}

	var configs []LanguageConfig
	var missingHints []string

	// Deduplicate: track which commands we've already added to avoid
	// starting the same server twice (e.g., JS + TS both use typescript-language-server).
	usedCommands := make(map[string]bool)

	// Sort language IDs for deterministic output
	langs := make([]string, 0, len(detected))
	for langID := range detected {
		langs = append(langs, langID)
	}
	sort.Strings(langs)

	for _, langID := range langs {
		servers, ok := KnownLSPServers[langID]
		if !ok {
			continue
		}

		// Try servers in priority order
		found := false
		for _, srv := range servers {
			if usedCommands[srv.Command] {
				found = true // already configured via another language
				break
			}
			if _, err := lookPath(srv.Command); err == nil {
				configs = append(configs, LanguageConfig{
					LangID:  langID,
					Command: srv.Command,
					Args:    srv.Args,
				})
				usedCommands[srv.Command] = true
				found = true
				lspLogger.Info("Auto-detected: %s -> %s %v", langID, srv.Command, srv.Args)
				break
			}
		}

		if !found {
			// Collect install hints for the preferred server
			hint := servers[0].InstallHint
			missingHints = append(missingHints, fmt.Sprintf("  %s: %s", langID, hint))
			lspLogger.Warn("Detected %s files but no LSP server found. Install with: %s", langID, hint)
		}
	}

	if len(configs) == 0 {
		return nil, fmt.Errorf("detected languages %v but no LSP servers are installed.\nInstall suggestions:\n%s",
			langs, strings.Join(missingHints, "\n"))
	}

	return configs, nil
}
