package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/angalato08/mcp-language-server/internal/lsp"
	"github.com/angalato08/mcp-language-server/internal/protocol"
	"github.com/angalato08/mcp-language-server/internal/utilities"
)

// ApplyFix applies a quick-fix for the diagnostic at the given position.
// line and column are 1-indexed.
func ApplyFix(ctx context.Context, client *lsp.Client, filePath string, line, column int, diagnosticCode string) (string, error) {
	uri, diagnostics, err := syncAndFetchDiagnostics(ctx, client, filePath)
	if err != nil {
		return "", err
	}

	matched := matchDiagnosticsAtPosition(diagnostics, line, column, diagnosticCode)
	if len(matched) == 0 {
		return "", fmt.Errorf("no diagnostic found at line %d, column %d", line, column)
	}

	actions, err := fetchQuickFixActions(ctx, client, uri, matched)
	if err != nil {
		return "", fmt.Errorf("failed to fetch quick-fix actions: %v", err)
	}

	if len(actions) == 0 {
		return "", fmt.Errorf("no quick-fix available for: %s", matched[0].Message)
	}

	best, alternatives := selectBestAction(actions)

	// Resolve if needed: Edit is nil but Data is present
	if best.Edit == nil && best.Data != nil {
		resolved, err := client.ResolveCodeAction(ctx, best)
		if err != nil {
			return "", fmt.Errorf("failed to resolve code action: %v", err)
		}
		best = resolved
	}

	if best.Edit == nil {
		if best.Command != nil {
			return "", fmt.Errorf("fix requires a command execution, not a workspace edit (command: %s) — not supported", best.Command.Command)
		}
		return "", fmt.Errorf("code action '%s' has no edit to apply", best.Title)
	}

	if err := utilities.ApplyWorkspaceEdit(*best.Edit); err != nil {
		return "", fmt.Errorf("failed to apply fix: %v", err)
	}

	notifyChangedFiles(ctx, client, best.Edit)

	// Build summary
	var result strings.Builder
	result.WriteString(fmt.Sprintf("Applied fix: %s\n", best.Title))

	filesChanged := countChangedFiles(best.Edit)
	result.WriteString(fmt.Sprintf("Files changed: %d\n", filesChanged))

	if len(alternatives) > 0 {
		result.WriteString(fmt.Sprintf("Other available fixes: %s\n", strings.Join(alternatives, ", ")))
	}

	return result.String(), nil
}

// ApplyAllFixes applies all preferred quick-fixes for the file sequentially.
func ApplyAllFixes(ctx context.Context, client *lsp.Client, filePath string) (string, error) {
	uri, diagnostics, err := syncAndFetchDiagnostics(ctx, client, filePath)
	if err != nil {
		return "", err
	}

	if len(diagnostics) == 0 {
		return "No diagnostics found. Nothing to fix.", nil
	}

	var appliedFixes []string
	totalFilesChanged := 0

	for iteration := 0; iteration < 50; iteration++ {
		actions, err := fetchQuickFixActions(ctx, client, uri, diagnostics)
		if err != nil {
			break
		}

		// Filter to preferred actions only
		var preferred []protocol.CodeAction
		for _, a := range actions {
			if a.IsPreferred {
				preferred = append(preferred, a)
			}
		}

		if len(preferred) == 0 {
			break
		}

		// Apply the first preferred action
		action := preferred[0]

		if action.Edit == nil && action.Data != nil {
			resolved, err := client.ResolveCodeAction(ctx, action)
			if err != nil {
				toolsLogger.Warn("Failed to resolve code action '%s': %v", action.Title, err)
				break
			}
			action = resolved
		}

		if action.Edit == nil {
			break
		}

		if err := utilities.ApplyWorkspaceEdit(*action.Edit); err != nil {
			toolsLogger.Warn("Failed to apply fix '%s': %v", action.Title, err)
			break
		}

		notifyChangedFiles(ctx, client, action.Edit)
		appliedFixes = append(appliedFixes, action.Title)
		totalFilesChanged += countChangedFiles(action.Edit)

		// Re-sync and fetch diagnostics (line numbers may have shifted)
		uri, diagnostics, err = syncAndFetchDiagnostics(ctx, client, filePath)
		if err != nil {
			break
		}

		if len(diagnostics) == 0 {
			break
		}
	}

	if len(appliedFixes) == 0 {
		return "No preferred quick-fixes available.", nil
	}

	var result strings.Builder
	result.WriteString(fmt.Sprintf("Applied %d fix(es):\n", len(appliedFixes)))
	for _, title := range appliedFixes {
		result.WriteString(fmt.Sprintf("  - %s\n", title))
	}
	result.WriteString(fmt.Sprintf("Total files changed: %d\n", totalFilesChanged))

	return result.String(), nil
}

// matchDiagnosticsAtPosition finds diagnostics at/near the given 1-indexed position.
// Uses tiered matching: exact → line → range containment.
// If diagnosticCode is non-empty, further filters by code.
func matchDiagnosticsAtPosition(diagnostics []protocol.Diagnostic, line, column int, diagnosticCode string) []protocol.Diagnostic {
	// Convert 1-indexed to 0-indexed
	zeroLine := uint32(line - 1)
	zeroCol := uint32(column - 1)

	// Tier 1: exact match
	var exact []protocol.Diagnostic
	for _, d := range diagnostics {
		if d.Range.Start.Line == zeroLine && d.Range.Start.Character == zeroCol {
			exact = append(exact, d)
		}
	}
	if len(exact) > 0 {
		return filterByCode(exact, diagnosticCode)
	}

	// Tier 2: line match (any column)
	var lineMatch []protocol.Diagnostic
	for _, d := range diagnostics {
		if d.Range.Start.Line == zeroLine {
			lineMatch = append(lineMatch, d)
		}
	}
	if len(lineMatch) > 0 {
		return filterByCode(lineMatch, diagnosticCode)
	}

	// Tier 3: range containment
	var rangeMatch []protocol.Diagnostic
	for _, d := range diagnostics {
		if positionInRange(zeroLine, zeroCol, d.Range) {
			rangeMatch = append(rangeMatch, d)
		}
	}
	return filterByCode(rangeMatch, diagnosticCode)
}

// filterByCode filters diagnostics by code substring if diagnosticCode is non-empty.
func filterByCode(diagnostics []protocol.Diagnostic, diagnosticCode string) []protocol.Diagnostic {
	if diagnosticCode == "" {
		return diagnostics
	}
	var filtered []protocol.Diagnostic
	for _, d := range diagnostics {
		if d.Code != nil {
			codeStr := fmt.Sprintf("%v", d.Code)
			if strings.Contains(codeStr, diagnosticCode) {
				filtered = append(filtered, d)
			}
		}
	}
	return filtered
}

// positionInRange checks if a 0-indexed position is within a protocol.Range.
func positionInRange(line, col uint32, r protocol.Range) bool {
	if line < r.Start.Line || line > r.End.Line {
		return false
	}
	if line == r.Start.Line && col < r.Start.Character {
		return false
	}
	if line == r.End.Line && col > r.End.Character {
		return false
	}
	return true
}

// fetchQuickFixActions queries the LSP for quick-fix CodeActions for the given diagnostics.
func fetchQuickFixActions(ctx context.Context, client *lsp.Client, uri protocol.DocumentUri, diagnostics []protocol.Diagnostic) ([]protocol.CodeAction, error) {
	// Compute a range spanning all provided diagnostics
	r := spanningRange(diagnostics)

	params := protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Range:        r,
		Context: protocol.CodeActionContext{
			Diagnostics: diagnostics,
			Only:        []protocol.CodeActionKind{protocol.QuickFix},
		},
	}

	actionItems, err := client.CodeAction(ctx, params)
	if err != nil {
		return nil, err
	}

	var actions []protocol.CodeAction
	for _, item := range actionItems {
		ca, ok := item.Value.(protocol.CodeAction)
		if !ok {
			continue
		}
		if ca.Disabled != nil {
			continue
		}
		actions = append(actions, ca)
	}

	return actions, nil
}

// selectBestAction picks the preferred action and lists alternatives.
// Returns the best action and a list of alternative action titles.
func selectBestAction(actions []protocol.CodeAction) (protocol.CodeAction, []string) {
	// Sort alphabetically by title for deterministic ordering
	sort.Slice(actions, func(i, j int) bool {
		return actions[i].Title < actions[j].Title
	})

	// Find preferred action
	bestIdx := -1
	for i, a := range actions {
		if a.IsPreferred {
			bestIdx = i
			break
		}
	}

	// If none preferred, pick first alphabetically
	if bestIdx == -1 {
		bestIdx = 0
	}

	best := actions[bestIdx]
	var alternatives []string
	for i, a := range actions {
		if i != bestIdx {
			alternatives = append(alternatives, a.Title)
		}
	}

	return best, alternatives
}

// notifyChangedFiles sends NotifyChange for each file URI in a WorkspaceEdit.
func notifyChangedFiles(ctx context.Context, client *lsp.Client, edit *protocol.WorkspaceEdit) {
	seen := make(map[string]bool)

	for uri := range edit.Changes {
		path := strings.TrimPrefix(string(uri), "file://")
		if !seen[path] {
			seen[path] = true
			if err := client.NotifyChange(ctx, path); err != nil {
				toolsLogger.Warn("Failed to notify LSP of change to %s: %v", path, err)
			}
		}
	}

	for _, change := range edit.DocumentChanges {
		if change.TextDocumentEdit != nil {
			path := strings.TrimPrefix(string(change.TextDocumentEdit.TextDocument.URI), "file://")
			if !seen[path] {
				seen[path] = true
				if err := client.NotifyChange(ctx, path); err != nil {
					toolsLogger.Warn("Failed to notify LSP of change to %s: %v", path, err)
				}
			}
		}
	}
}

// spanningRange returns a Range that covers all the given diagnostics.
func spanningRange(diagnostics []protocol.Diagnostic) protocol.Range {
	if len(diagnostics) == 0 {
		return protocol.Range{}
	}
	r := protocol.Range{
		Start: diagnostics[0].Range.Start,
		End:   diagnostics[0].Range.End,
	}
	for _, d := range diagnostics[1:] {
		if d.Range.Start.Line < r.Start.Line || (d.Range.Start.Line == r.Start.Line && d.Range.Start.Character < r.Start.Character) {
			r.Start = d.Range.Start
		}
		if d.Range.End.Line > r.End.Line || (d.Range.End.Line == r.End.Line && d.Range.End.Character > r.End.Character) {
			r.End = d.Range.End
		}
	}
	return r
}

// countChangedFiles counts the number of unique files affected by a WorkspaceEdit.
func countChangedFiles(edit *protocol.WorkspaceEdit) int {
	seen := make(map[string]bool)
	for uri := range edit.Changes {
		seen[string(uri)] = true
	}
	for _, change := range edit.DocumentChanges {
		if change.TextDocumentEdit != nil {
			seen[string(change.TextDocumentEdit.TextDocument.URI)] = true
		}
	}
	return len(seen)
}
