package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/angalato08/mcp-language-server/internal/lsp"
	"github.com/angalato08/mcp-language-server/internal/protocol"
	"github.com/angalato08/mcp-language-server/internal/watcher/common"
)

// JSON output structs

// JSONDiagnostic represents a single diagnostic in structured JSON output.
type JSONDiagnostic struct {
	File      string   `json:"file"`
	Line      int      `json:"line"`
	Column    int      `json:"col"`
	EndLine   int      `json:"endLine"`
	EndColumn int      `json:"endCol"`
	Severity  string   `json:"severity"`
	Message   string   `json:"message"`
	Source    string   `json:"source,omitempty"`
	Code      string   `json:"code,omitempty"`
	CodeHref  string   `json:"codeHref,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	Fixable   bool     `json:"fixable"`
}

// JSONFileDiagnostics represents diagnostics for a single file in JSON output.
type JSONFileDiagnostics struct {
	File        string           `json:"file"`
	Diagnostics []JSONDiagnostic `json:"diagnostics"`
	Count       int              `json:"count"`
	Truncated   bool             `json:"truncated"`
	Error       string           `json:"error,omitempty"`
}

// JSONDiagnosticsResult is the top-level JSON result for non-diff mode.
type JSONDiagnosticsResult struct {
	Files    []JSONFileDiagnostics `json:"files"`
	Total    int                   `json:"total"`
	Indexing bool                  `json:"indexing,omitempty"`
}

// JSONFileDiagnosticDiff represents diagnostic changes for a single file in JSON output.
type JSONFileDiagnosticDiff struct {
	File     string           `json:"file"`
	New      []JSONDiagnostic `json:"new"`
	Resolved []JSONDiagnostic `json:"resolved"`
	Error    string           `json:"error,omitempty"`
}

// JSONDiagnosticDiffResult is the top-level JSON result for diff mode.
type JSONDiagnosticDiffResult struct {
	Files    []JSONFileDiagnosticDiff `json:"files"`
	Indexing bool                     `json:"indexing,omitempty"`
}

// DiagnosticFilter controls which diagnostics are included in results.
type DiagnosticFilter struct {
	Exclude     []string                    // Substrings matched against the Code field
	MinSeverity protocol.DiagnosticSeverity // 0=no filter, 1=error only, 2=warning+error, etc.
}

// FilterDiagnostics returns only the diagnostics that pass the filter criteria.
func FilterDiagnostics(diagnostics []protocol.Diagnostic, filter DiagnosticFilter) []protocol.Diagnostic {
	if len(filter.Exclude) == 0 && filter.MinSeverity == 0 {
		return diagnostics
	}

	result := make([]protocol.Diagnostic, 0, len(diagnostics))
	for _, diag := range diagnostics {
		if filter.MinSeverity > 0 && diag.Severity > filter.MinSeverity {
			continue
		}

		if len(filter.Exclude) > 0 && diag.Code != nil {
			codeStr := fmt.Sprintf("%v", diag.Code)
			excluded := false
			for _, substr := range filter.Exclude {
				if strings.Contains(codeStr, substr) {
					excluded = true
					break
				}
			}
			if excluded {
				continue
			}
		}

		result = append(result, diag)
	}
	return result
}

// anyClientIndexing returns true if any of the given clients is still indexing.
func anyClientIndexing(clients []*lsp.Client) bool {
	for _, c := range clients {
		if c.IsIndexing() {
			return true
		}
	}
	return false
}

// syncAndFetchDiagnostics syncs a file to the LSP server, waits for diagnostics,
// and returns the URI and cached diagnostics.
func syncAndFetchDiagnostics(ctx context.Context, client *lsp.Client, filePath string) (protocol.DocumentUri, []protocol.Diagnostic, error) {
	synced, err := client.SyncFileFromDisk(ctx, filePath)
	if err != nil {
		return "", nil, fmt.Errorf("could not sync file: %v", err)
	}

	uri := protocol.DocumentUri("file://" + filePath)
	if synced {
		client.WaitForDiagnostics(uri, 3*time.Second)
	} else {
		client.WaitForDiagnostics(uri, 1*time.Second)
	}

	diagParams := protocol.DocumentDiagnosticParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	}
	_, err = client.Diagnostic(ctx, diagParams)
	if err != nil {
		toolsLogger.Warn("Failed to request fresh diagnostics (may not be supported by server): %v", err)
	}

	diagnostics := client.GetFileDiagnostics(uri)
	return uri, diagnostics, nil
}

// getSeverityStringLower returns the lowercase severity string for a DiagnosticSeverity.
func getSeverityStringLower(severity protocol.DiagnosticSeverity) string {
	switch severity {
	case protocol.SeverityError:
		return "error"
	case protocol.SeverityWarning:
		return "warning"
	case protocol.SeverityInformation:
		return "info"
	case protocol.SeverityHint:
		return "hint"
	default:
		return "unknown"
	}
}

// diagnosticKey returns a composite key for identifying a diagnostic in fixable lookups.
func diagnosticKey(diag protocol.Diagnostic) string {
	return fmt.Sprintf("%d:%d:%s", diag.Range.Start.Line, diag.Range.Start.Character, diag.Message)
}

// convertDiagnostic converts a protocol.Diagnostic to a JSONDiagnostic.
func convertDiagnostic(diag protocol.Diagnostic, filePath string, fixableSet map[string]bool) JSONDiagnostic {
	jd := JSONDiagnostic{
		File:      RelativePath(filePath),
		Line:      int(diag.Range.Start.Line) + 1,
		Column:    int(diag.Range.Start.Character) + 1,
		EndLine:   int(diag.Range.End.Line) + 1,
		EndColumn: int(diag.Range.End.Character) + 1,
		Severity:  getSeverityStringLower(diag.Severity),
		Message:   diag.Message,
		Source:    diag.Source,
		Fixable:   fixableSet[diagnosticKey(diag)],
	}

	if diag.Code != nil {
		jd.Code = fmt.Sprintf("%v", diag.Code)
	}

	if diag.CodeDescription != nil {
		jd.CodeHref = string(diag.CodeDescription.Href)
	}

	for _, tag := range diag.Tags {
		switch tag {
		case protocol.Unnecessary:
			jd.Tags = append(jd.Tags, "unnecessary")
		case protocol.Deprecated:
			jd.Tags = append(jd.Tags, "deprecated")
		}
	}

	return jd
}

// getFixableDiagnostics sends a textDocument/codeAction request for the given file
// and returns a set of diagnostic keys that have associated quick-fix actions.
func getFixableDiagnostics(ctx context.Context, client *lsp.Client, uri protocol.DocumentUri, diagnostics []protocol.Diagnostic) map[string]bool {
	fixable := make(map[string]bool)
	if len(diagnostics) == 0 {
		return fixable
	}

	// Compute a range spanning all provided diagnostics rather than using a
	// sentinel value — some servers (e.g. gopls) reject out-of-range lines.
	r := spanningRange(diagnostics)

	params := protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Range:        r,
		Context: protocol.CodeActionContext{
			Diagnostics: diagnostics,
			Only:        []protocol.CodeActionKind{protocol.QuickFix},
		},
	}

	actions, err := client.CodeAction(ctx, params)
	if err != nil {
		return fixable
	}

	for _, actionItem := range actions {
		ca, ok := actionItem.Value.(protocol.CodeAction)
		if !ok {
			continue
		}
		for _, d := range ca.Diagnostics {
			fixable[diagnosticKey(d)] = true
		}
	}

	return fixable
}

// GetDiagnosticsForFile retrieves diagnostics for a specific file from the language server
func GetDiagnosticsForFile(ctx context.Context, client *lsp.Client, filePath string, contextLines int, showLineNumbers bool, limit int, filter DiagnosticFilter) (string, error) {
	// Override with environment variable if specified
	if envLines := os.Getenv("LSP_CONTEXT_LINES"); envLines != "" {
		if val, err := strconv.Atoi(envLines); err == nil && val >= 0 {
			contextLines = val
		}
	}

	uri, diagnostics, err := syncAndFetchDiagnostics(ctx, client, filePath)
	if err != nil {
		return "", err
	}

	// Apply diagnostic filter
	diagnostics = FilterDiagnostics(diagnostics, filter)

	if len(diagnostics) == 0 {
		return "No diagnostics found for " + RelativePath(filePath), nil
	}

	totalCount := len(diagnostics)

	// Apply limit if specified
	if limit > 0 && len(diagnostics) > limit {
		diagnostics = diagnostics[:limit]
	}

	displayPath := RelativePath(filePath)

	// Format file header
	fileInfo := fmt.Sprintf("%s\nDiagnostics in File: %d\n",
		displayPath,
		totalCount,
	)

	if client.IsIndexing() {
		fileInfo += "WARNING: Language server is still indexing. Diagnostics may be incomplete or stale.\n"
	}

	// Prepend limit notice if results were truncated
	if limit > 0 && totalCount > limit {
		fileInfo += fmt.Sprintf("Showing %d of %d diagnostics\n", limit, totalCount)
	}

	// Create a summary of all the diagnostics
	var diagSummaries []string
	var diagLocations []protocol.Location

	for _, diag := range diagnostics {
		severity := getSeverityString(diag.Severity)
		location := fmt.Sprintf("L%d:C%d",
			diag.Range.Start.Line+1,
			diag.Range.Start.Character+1)

		summary := fmt.Sprintf("%s at %s: %s",
			severity,
			location,
			diag.Message)

		// Add source and code if available
		if diag.Source != "" {
			summary += fmt.Sprintf(" (Source: %s", diag.Source)
			if diag.Code != nil {
				summary += fmt.Sprintf(", Code: %v", diag.Code)
			}
			summary += ")"
		} else if diag.Code != nil {
			summary += fmt.Sprintf(" (Code: %v)", diag.Code)
		}

		diagSummaries = append(diagSummaries, summary)

		// Create a location for this diagnostic to use with line ranges
		diagLocations = append(diagLocations, protocol.Location{
			URI:   uri,
			Range: diag.Range,
		})
	}

	// Format content with context
	fileContent, err := os.ReadFile(filePath)
	if err != nil {
		// Include diagnostic summaries even when file can't be read for context
		result := fileInfo
		if len(diagSummaries) > 0 {
			result += strings.Join(diagSummaries, "\n") + "\n"
		}
		return result, nil
	}

	lines := strings.Split(string(fileContent), "\n")

	// Collect lines to display
	var linesToShow map[int]bool
	if contextLines > 0 {
		// Use GetLineRangesToDisplay for context
		linesToShow, err = GetLineRangesToDisplay(ctx, client, diagLocations, len(lines), contextLines)
		if err != nil {
			// If error, just show the diagnostic lines
			linesToShow = make(map[int]bool)
			for _, diag := range diagnostics {
				linesToShow[int(diag.Range.Start.Line)] = true
			}
		}
	} else {
		// Just show the diagnostic lines
		linesToShow = make(map[int]bool)
		for _, diag := range diagnostics {
			linesToShow[int(diag.Range.Start.Line)] = true
		}
	}

	// Convert to line ranges
	lineRanges := ConvertLinesToRanges(linesToShow, len(lines))

	// Format with diagnostics summary in header
	result := fileInfo
	if len(diagSummaries) > 0 {
		result += strings.Join(diagSummaries, "\n") + "\n"
	}

	// Format the content with ranges
	if showLineNumbers {
		result += "\n" + FormatLinesWithRanges(lines, lineRanges)
	}

	return result, nil
}

func getSeverityString(severity protocol.DiagnosticSeverity) string {
	switch severity {
	case protocol.SeverityError:
		return "ERROR"
	case protocol.SeverityWarning:
		return "WARNING"
	case protocol.SeverityInformation:
		return "INFO"
	case protocol.SeverityHint:
		return "HINT"
	default:
		return "UNKNOWN"
	}
}

// GetDiagnosticDiffForFile retrieves the diagnostic diff for a specific file.
func GetDiagnosticDiffForFile(ctx context.Context, client *lsp.Client, filePath string, contextLines int, showLineNumbers bool, limit int, filter DiagnosticFilter) (string, error) {
	// Override with environment variable if specified
	if envLines := os.Getenv("LSP_CONTEXT_LINES"); envLines != "" {
		if val, err := strconv.Atoi(envLines); err == nil && val >= 0 {
			contextLines = val
		}
	}

	uri, _, err := syncAndFetchDiagnostics(ctx, client, filePath)
	if err != nil {
		return "", err
	}

	// Get diff from client
	newDiags, resolved := client.GetDiagnosticDiff(uri)

	// Apply filter to both slices
	newDiags = FilterDiagnostics(newDiags, filter)
	resolved = FilterDiagnostics(resolved, filter)

	displayPath := RelativePath(filePath)

	if len(newDiags) == 0 && len(resolved) == 0 {
		return fmt.Sprintf("No diagnostic changes for %s", displayPath), nil
	}

	// Apply limit across both slices
	totalCount := len(newDiags) + len(resolved)
	if limit > 0 && totalCount > limit {
		// Prioritize new diagnostics over resolved
		if len(newDiags) >= limit {
			newDiags = newDiags[:limit]
			resolved = nil
		} else {
			remaining := limit - len(newDiags)
			if len(resolved) > remaining {
				resolved = resolved[:remaining]
			}
		}
	}

	// Format header
	result := fmt.Sprintf("%s\nNew Diagnostics: %d, Resolved Diagnostics: %d\n",
		displayPath, len(newDiags), len(resolved))

	if client.IsIndexing() {
		result += "WARNING: Language server is still indexing. Diagnostics may be incomplete or stale.\n"
	}

	if limit > 0 && totalCount > limit {
		result += fmt.Sprintf("Showing %d of %d diagnostic changes\n", limit, totalCount)
	}

	// Format new diagnostics with code context
	for _, diag := range newDiags {
		result += "\n" + formatDiffDiagnostic("[NEW]", diag)
	}

	// Format resolved diagnostics (no code context — source may have changed)
	for _, diag := range resolved {
		result += "\n" + formatDiffDiagnostic("[RESOLVED]", diag)
	}

	// Show code context only for NEW diagnostics
	if showLineNumbers && len(newDiags) > 0 && contextLines > 0 {
		fileContent, err := os.ReadFile(filePath)
		if err == nil {
			lines := strings.Split(string(fileContent), "\n")
			var diagLocations []protocol.Location
			for _, diag := range newDiags {
				diagLocations = append(diagLocations, protocol.Location{
					URI:   uri,
					Range: diag.Range,
				})
			}
			linesToShow, err := GetLineRangesToDisplay(ctx, client, diagLocations, len(lines), contextLines)
			if err != nil {
				linesToShow = make(map[int]bool)
				for _, diag := range newDiags {
					linesToShow[int(diag.Range.Start.Line)] = true
				}
			}
			lineRanges := ConvertLinesToRanges(linesToShow, len(lines))
			result += "\n" + FormatLinesWithRanges(lines, lineRanges)
		}
	}

	return result, nil
}

// formatDiffDiagnostic formats a single diagnostic with a [NEW] or [RESOLVED] prefix.
func formatDiffDiagnostic(prefix string, diag protocol.Diagnostic) string {
	severity := getSeverityString(diag.Severity)
	location := fmt.Sprintf("L%d:C%d",
		diag.Range.Start.Line+1,
		diag.Range.Start.Character+1)

	summary := fmt.Sprintf("%s %s at %s: %s",
		prefix, severity, location, diag.Message)

	if diag.Source != "" {
		summary += fmt.Sprintf(" (Source: %s", diag.Source)
		if diag.Code != nil {
			summary += fmt.Sprintf(", Code: %v", diag.Code)
		}
		summary += ")"
	} else if diag.Code != nil {
		summary += fmt.Sprintf(" (Code: %v)", diag.Code)
	}

	return summary
}

// GetAllDiagnosticDiffs retrieves diagnostic diffs for all active clients.
func GetAllDiagnosticDiffs(ctx context.Context, clients []*lsp.Client, contextLines int, showLineNumbers bool, limit int, filter DiagnosticFilter) (string, error) {
	var allDiffs []string

	for _, client := range clients {
		diffs := client.GetAllDiagnosticDiffs()

		for uri, diff := range diffs {
			newDiags := FilterDiagnostics(diff.New, filter)
			resolved := FilterDiagnostics(diff.Resolved, filter)

			if len(newDiags) == 0 && len(resolved) == 0 {
				continue
			}

			filePath := strings.TrimPrefix(string(uri), "file://")
			displayPath := RelativePath(filePath)

			// Apply limit across both slices (prioritize new over resolved)
			totalCount := len(newDiags) + len(resolved)
			if limit > 0 && totalCount > limit {
				if len(newDiags) >= limit {
					newDiags = newDiags[:limit]
					resolved = nil
				} else {
					remaining := limit - len(newDiags)
					if len(resolved) > remaining {
						resolved = resolved[:remaining]
					}
				}
			}

			fileInfo := fmt.Sprintf("---\n\n%s\nNew Diagnostics: %d, Resolved Diagnostics: %d\n",
				displayPath, len(newDiags), len(resolved))

			if limit > 0 && totalCount > limit {
				fileInfo += fmt.Sprintf("Showing %d of %d diagnostic changes\n", limit, totalCount)
			}

			for _, diag := range newDiags {
				fileInfo += formatDiffDiagnostic("[NEW]", diag) + "\n"
			}
			for _, diag := range resolved {
				fileInfo += formatDiffDiagnostic("[RESOLVED]", diag) + "\n"
			}

			// Show code context only for NEW diagnostics
			if contextLines > 0 && len(newDiags) > 0 {
				fileContent, err := os.ReadFile(filePath)
				if err == nil {
					lines := strings.Split(string(fileContent), "\n")
					var diagLocations []protocol.Location
					for _, diag := range newDiags {
						diagLocations = append(diagLocations, protocol.Location{
							URI:   uri,
							Range: diag.Range,
						})
					}
					linesToShow, err := GetLineRangesToDisplay(ctx, client, diagLocations, len(lines), contextLines)
					if err != nil {
						linesToShow = make(map[int]bool)
						for _, diag := range newDiags {
							linesToShow[int(diag.Range.Start.Line)] = true
						}
					}
					lineRanges := ConvertLinesToRanges(linesToShow, len(lines))
					if showLineNumbers {
						fileInfo += "\n" + FormatLinesWithRanges(lines, lineRanges)
					}
				}
			}

			allDiffs = append(allDiffs, fileInfo)
		}
	}

	if len(allDiffs) == 0 {
		return "No diagnostic changes in workspace", nil
	}

	result := strings.Join(allDiffs, "")
	if anyClientIndexing(clients) {
		result = "WARNING: Language server is still indexing. Diagnostics may be incomplete or stale.\n" + result
	}
	return result, nil
}

// GetAllDiagnostics retrieves diagnostics for all active clients
func GetAllDiagnostics(ctx context.Context, clients []*lsp.Client, contextLines int, showLineNumbers bool, limit int, filter DiagnosticFilter) (string, error) {
	var allDiagnostics []string

	for _, client := range clients {
		// Get all files that have diagnostics in this client
		diagnosticsMap := client.GetAllDiagnostics()

		// Sort URIs for deterministic output
		uris := make([]string, 0, len(diagnosticsMap))
		for uri := range diagnosticsMap {
			uris = append(uris, string(uri))
		}

		for _, uriStr := range uris {
			uri := protocol.DocumentUri(uriStr)
			diagnostics := FilterDiagnostics(diagnosticsMap[uri], filter)

			if len(diagnostics) == 0 {
				continue
			}

			filePath := strings.TrimPrefix(string(uri), "file://")
			displayPath := RelativePath(filePath)

			totalCount := len(diagnostics)

			// Apply limit if specified
			if limit > 0 && len(diagnostics) > limit {
				diagnostics = diagnostics[:limit]
			}

			fileInfo := fmt.Sprintf("---\n\n%s\nDiagnostics in File: %d\n",
				displayPath,
				totalCount,
			)

			if limit > 0 && totalCount > limit {
				fileInfo += fmt.Sprintf("Showing %d of %d diagnostics\n", limit, totalCount)
			}

			var diagSummaries []string
			var diagLocations []protocol.Location
			for _, diag := range diagnostics {
				severity := getSeverityString(diag.Severity)
				location := fmt.Sprintf("L%d:C%d",
					diag.Range.Start.Line+1,
					diag.Range.Start.Character+1)

				summary := fmt.Sprintf("%s at %s: %s",
					severity,
					location,
					diag.Message)

				if diag.Source != "" {
					summary += fmt.Sprintf(" (Source: %s", diag.Source)
					if diag.Code != nil {
						summary += fmt.Sprintf(", Code: %v", diag.Code)
					}
					summary += ")"
				}
				diagSummaries = append(diagSummaries, summary)

				diagLocations = append(diagLocations, protocol.Location{
					URI:   uri,
					Range: diag.Range,
				})
			}

			fileInfo += strings.Join(diagSummaries, "\n") + "\n"

			// Add code context if requested
			if contextLines > 0 {
				fileContent, err := os.ReadFile(filePath)
				if err == nil {
					lines := strings.Split(string(fileContent), "\n")
					linesToShow, err := GetLineRangesToDisplay(ctx, client, diagLocations, len(lines), contextLines)
					if err != nil {
						linesToShow = make(map[int]bool)
						for _, diag := range diagnostics {
							linesToShow[int(diag.Range.Start.Line)] = true
						}
					}
					lineRanges := ConvertLinesToRanges(linesToShow, len(lines))
					if showLineNumbers {
						fileInfo += "\n" + FormatLinesWithRanges(lines, lineRanges)
					}
				}
			}

			allDiagnostics = append(allDiagnostics, fileInfo)
		}
	}

	if len(allDiagnostics) == 0 {
		return "No diagnostics found in workspace", nil
	}

	result := strings.Join(allDiagnostics, "")
	if anyClientIndexing(clients) {
		result = "WARNING: Language server is still indexing. Diagnostics may be incomplete or stale.\n" + result
	}
	return result, nil
}

// GetDiagnosticsForFiles retrieves diagnostics for multiple files, routing each
// to the correct LSP client via the router. Per-file errors are recorded inline
// and do not abort the batch.
func GetDiagnosticsForFiles(ctx context.Context, router *lsp.Router, filePaths []string,
	contextLines int, showLineNumbers bool, limit int, showDiff bool, filter DiagnosticFilter) (string, error) {

	var sections []string
	for _, fp := range filePaths {
		client, err := router.ClientForFile(ctx, fp)
		if err != nil {
			sections = append(sections, fmt.Sprintf("---\n\n%s\nError: %v", RelativePath(fp), err))
			continue
		}

		var text string
		if showDiff {
			text, err = GetDiagnosticDiffForFile(ctx, client, fp, contextLines, showLineNumbers, limit, filter)
		} else {
			text, err = GetDiagnosticsForFile(ctx, client, fp, contextLines, showLineNumbers, limit, filter)
		}
		if err != nil {
			sections = append(sections, fmt.Sprintf("---\n\n%s\nError: %v", RelativePath(fp), err))
			continue
		}
		sections = append(sections, "---\n\n"+text)
	}

	if len(sections) == 0 {
		return fmt.Sprintf("No diagnostics found in %d files", len(filePaths)), nil
	}
	return strings.Join(sections, "\n"), nil
}

// --- JSON output functions ---

// GetDiagnosticsForFileJSON returns structured JSON diagnostics for a single file.
func GetDiagnosticsForFileJSON(ctx context.Context, client *lsp.Client, filePath string, limit int, filter DiagnosticFilter) (string, error) {
	uri, diagnostics, err := syncAndFetchDiagnostics(ctx, client, filePath)
	if err != nil {
		result := JSONDiagnosticsResult{
			Files: []JSONFileDiagnostics{{
				File:        RelativePath(filePath),
				Diagnostics: []JSONDiagnostic{},
				Error:       err.Error(),
			}},
		}
		b, _ := json.Marshal(result)
		return string(b), nil
	}

	diagnostics = FilterDiagnostics(diagnostics, filter)
	totalCount := len(diagnostics)
	truncated := false

	if limit > 0 && len(diagnostics) > limit {
		diagnostics = diagnostics[:limit]
		truncated = true
	}

	fixableSet := getFixableDiagnostics(ctx, client, uri, diagnostics)

	jsonDiags := make([]JSONDiagnostic, 0, len(diagnostics))
	for _, d := range diagnostics {
		jsonDiags = append(jsonDiags, convertDiagnostic(d, filePath, fixableSet))
	}

	result := JSONDiagnosticsResult{
		Files: []JSONFileDiagnostics{{
			File:        RelativePath(filePath),
			Diagnostics: jsonDiags,
			Count:       totalCount,
			Truncated:   truncated,
		}},
		Total:    totalCount,
		Indexing: client.IsIndexing(),
	}

	b, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal JSON: %v", err)
	}
	return string(b), nil
}

// GetDiagnosticDiffForFileJSON returns structured JSON diagnostic diff for a single file.
func GetDiagnosticDiffForFileJSON(ctx context.Context, client *lsp.Client, filePath string, limit int, filter DiagnosticFilter) (string, error) {
	uri, _, err := syncAndFetchDiagnostics(ctx, client, filePath)
	if err != nil {
		result := JSONDiagnosticDiffResult{
			Files: []JSONFileDiagnosticDiff{{
				File:     RelativePath(filePath),
				New:      []JSONDiagnostic{},
				Resolved: []JSONDiagnostic{},
				Error:    err.Error(),
			}},
		}
		b, _ := json.Marshal(result)
		return string(b), nil
	}

	newDiags, resolved := client.GetDiagnosticDiff(uri)
	newDiags = FilterDiagnostics(newDiags, filter)
	resolved = FilterDiagnostics(resolved, filter)

	// Apply limit across both slices (prioritize new over resolved)
	totalCount := len(newDiags) + len(resolved)
	if limit > 0 && totalCount > limit {
		if len(newDiags) >= limit {
			newDiags = newDiags[:limit]
			resolved = nil
		} else {
			remaining := limit - len(newDiags)
			if len(resolved) > remaining {
				resolved = resolved[:remaining]
			}
		}
	}

	// Get fixable info for new diagnostics
	allDiags := append(newDiags, resolved...)
	fixableSet := getFixableDiagnostics(ctx, client, uri, allDiags)

	jsonNew := make([]JSONDiagnostic, 0, len(newDiags))
	for _, d := range newDiags {
		jsonNew = append(jsonNew, convertDiagnostic(d, filePath, fixableSet))
	}
	jsonResolved := make([]JSONDiagnostic, 0, len(resolved))
	for _, d := range resolved {
		jsonResolved = append(jsonResolved, convertDiagnostic(d, filePath, fixableSet))
	}

	result := JSONDiagnosticDiffResult{
		Files: []JSONFileDiagnosticDiff{{
			File:     RelativePath(filePath),
			New:      jsonNew,
			Resolved: jsonResolved,
		}},
		Indexing: client.IsIndexing(),
	}

	b, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal JSON: %v", err)
	}
	return string(b), nil
}

// GetAllDiagnosticsJSON returns structured JSON diagnostics for all active clients.
func GetAllDiagnosticsJSON(ctx context.Context, clients []*lsp.Client, limit int, filter DiagnosticFilter) (string, error) {
	var allFiles []JSONFileDiagnostics
	total := 0

	for _, client := range clients {
		diagnosticsMap := client.GetAllDiagnostics()

		uris := make([]string, 0, len(diagnosticsMap))
		for uri := range diagnosticsMap {
			uris = append(uris, string(uri))
		}
		sort.Strings(uris)

		for _, uriStr := range uris {
			uri := protocol.DocumentUri(uriStr)
			diagnostics := FilterDiagnostics(diagnosticsMap[uri], filter)
			if len(diagnostics) == 0 {
				continue
			}

			filePath := strings.TrimPrefix(string(uri), "file://")
			totalCount := len(diagnostics)
			truncated := false

			if limit > 0 && len(diagnostics) > limit {
				diagnostics = diagnostics[:limit]
				truncated = true
			}

			fixableSet := getFixableDiagnostics(ctx, client, uri, diagnostics)

			jsonDiags := make([]JSONDiagnostic, 0, len(diagnostics))
			for _, d := range diagnostics {
				jsonDiags = append(jsonDiags, convertDiagnostic(d, filePath, fixableSet))
			}

			allFiles = append(allFiles, JSONFileDiagnostics{
				File:        RelativePath(filePath),
				Diagnostics: jsonDiags,
				Count:       totalCount,
				Truncated:   truncated,
			})
			total += totalCount
		}
	}

	if allFiles == nil {
		allFiles = []JSONFileDiagnostics{}
	}

	result := JSONDiagnosticsResult{
		Files:    allFiles,
		Total:    total,
		Indexing: anyClientIndexing(clients),
	}

	b, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal JSON: %v", err)
	}
	return string(b), nil
}

// GetAllDiagnosticDiffsJSON returns structured JSON diagnostic diffs for all active clients.
func GetAllDiagnosticDiffsJSON(ctx context.Context, clients []*lsp.Client, limit int, filter DiagnosticFilter) (string, error) {
	var allFiles []JSONFileDiagnosticDiff

	for _, client := range clients {
		diffs := client.GetAllDiagnosticDiffs()

		for uri, diff := range diffs {
			newDiags := FilterDiagnostics(diff.New, filter)
			resolved := FilterDiagnostics(diff.Resolved, filter)

			if len(newDiags) == 0 && len(resolved) == 0 {
				continue
			}

			filePath := strings.TrimPrefix(string(uri), "file://")

			// Apply limit across both slices (prioritize new over resolved)
			totalCount := len(newDiags) + len(resolved)
			if limit > 0 && totalCount > limit {
				if len(newDiags) >= limit {
					newDiags = newDiags[:limit]
					resolved = nil
				} else {
					remaining := limit - len(newDiags)
					if len(resolved) > remaining {
						resolved = resolved[:remaining]
					}
				}
			}

			allDiags := append(newDiags, resolved...)
			fixableSet := getFixableDiagnostics(ctx, client, uri, allDiags)

			jsonNew := make([]JSONDiagnostic, 0, len(newDiags))
			for _, d := range newDiags {
				jsonNew = append(jsonNew, convertDiagnostic(d, filePath, fixableSet))
			}
			jsonResolved := make([]JSONDiagnostic, 0, len(resolved))
			for _, d := range resolved {
				jsonResolved = append(jsonResolved, convertDiagnostic(d, filePath, fixableSet))
			}

			allFiles = append(allFiles, JSONFileDiagnosticDiff{
				File:     RelativePath(filePath),
				New:      jsonNew,
				Resolved: jsonResolved,
			})
		}
	}

	if allFiles == nil {
		allFiles = []JSONFileDiagnosticDiff{}
	}

	result := JSONDiagnosticDiffResult{
		Files:    allFiles,
		Indexing: anyClientIndexing(clients),
	}

	b, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal JSON: %v", err)
	}
	return string(b), nil
}

// GetDiagnosticsForFilesJSON returns structured JSON diagnostics for multiple files.
func GetDiagnosticsForFilesJSON(ctx context.Context, router *lsp.Router, filePaths []string, limit int, showDiff bool, filter DiagnosticFilter) (string, error) {
	if showDiff {
		var allFiles []JSONFileDiagnosticDiff
		anyIndexing := false
		for _, fp := range filePaths {
			client, err := router.ClientForFile(ctx, fp)
			if err != nil {
				allFiles = append(allFiles, JSONFileDiagnosticDiff{
					File:     RelativePath(fp),
					New:      []JSONDiagnostic{},
					Resolved: []JSONDiagnostic{},
					Error:    err.Error(),
				})
				continue
			}

			if client.IsIndexing() {
				anyIndexing = true
			}

			uri, _, err := syncAndFetchDiagnostics(ctx, client, fp)
			if err != nil {
				allFiles = append(allFiles, JSONFileDiagnosticDiff{
					File:     RelativePath(fp),
					New:      []JSONDiagnostic{},
					Resolved: []JSONDiagnostic{},
					Error:    err.Error(),
				})
				continue
			}

			newDiags, resolved := client.GetDiagnosticDiff(uri)
			newDiags = FilterDiagnostics(newDiags, filter)
			resolved = FilterDiagnostics(resolved, filter)

			totalCount := len(newDiags) + len(resolved)
			if limit > 0 && totalCount > limit {
				if len(newDiags) >= limit {
					newDiags = newDiags[:limit]
					resolved = nil
				} else {
					remaining := limit - len(newDiags)
					if len(resolved) > remaining {
						resolved = resolved[:remaining]
					}
				}
			}

			combinedDiags := append(newDiags, resolved...)
			fixableSet := getFixableDiagnostics(ctx, client, uri, combinedDiags)

			jsonNew := make([]JSONDiagnostic, 0, len(newDiags))
			for _, d := range newDiags {
				jsonNew = append(jsonNew, convertDiagnostic(d, fp, fixableSet))
			}
			jsonResolved := make([]JSONDiagnostic, 0, len(resolved))
			for _, d := range resolved {
				jsonResolved = append(jsonResolved, convertDiagnostic(d, fp, fixableSet))
			}

			allFiles = append(allFiles, JSONFileDiagnosticDiff{
				File:     RelativePath(fp),
				New:      jsonNew,
				Resolved: jsonResolved,
			})
		}

		if allFiles == nil {
			allFiles = []JSONFileDiagnosticDiff{}
		}
		result := JSONDiagnosticDiffResult{Files: allFiles, Indexing: anyIndexing}
		b, err := json.Marshal(result)
		if err != nil {
			return "", fmt.Errorf("failed to marshal JSON: %v", err)
		}
		return string(b), nil
	}

	// Non-diff mode
	var allFiles []JSONFileDiagnostics
	total := 0
	anyIndexing := false

	for _, fp := range filePaths {
		client, err := router.ClientForFile(ctx, fp)
		if err != nil {
			allFiles = append(allFiles, JSONFileDiagnostics{
				File:        RelativePath(fp),
				Diagnostics: []JSONDiagnostic{},
				Error:       err.Error(),
			})
			continue
		}

		if client.IsIndexing() {
			anyIndexing = true
		}

		uri, diagnostics, err := syncAndFetchDiagnostics(ctx, client, fp)
		if err != nil {
			allFiles = append(allFiles, JSONFileDiagnostics{
				File:        RelativePath(fp),
				Diagnostics: []JSONDiagnostic{},
				Error:       err.Error(),
			})
			continue
		}

		diagnostics = FilterDiagnostics(diagnostics, filter)
		totalCount := len(diagnostics)
		truncated := false

		if limit > 0 && len(diagnostics) > limit {
			diagnostics = diagnostics[:limit]
			truncated = true
		}

		fixableSet := getFixableDiagnostics(ctx, client, uri, diagnostics)

		jsonDiags := make([]JSONDiagnostic, 0, len(diagnostics))
		for _, d := range diagnostics {
			jsonDiags = append(jsonDiags, convertDiagnostic(d, fp, fixableSet))
		}

		allFiles = append(allFiles, JSONFileDiagnostics{
			File:        RelativePath(fp),
			Diagnostics: jsonDiags,
			Count:       totalCount,
			Truncated:   truncated,
		})
		total += totalCount
	}

	if allFiles == nil {
		allFiles = []JSONFileDiagnostics{}
	}

	result := JSONDiagnosticsResult{
		Files:    allFiles,
		Total:    total,
		Indexing: anyIndexing,
	}

	b, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal JSON: %v", err)
	}
	return string(b), nil
}

// CollectFilesFromDirectory walks a directory recursively and returns absolute
// paths of source files that have LSP support. It respects gitignore, watcher
// exclusion lists, and skips large/binary files.
func CollectFilesFromDirectory(dir string) ([]string, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", dir)
	}

	cfg := common.DefaultWatcherConfig()

	// Build gitignore matcher from the workspace root (or the dir itself as fallback)
	gitRoot := workspaceRoot
	if gitRoot == "" {
		gitRoot = dir
	}
	gitMatcher, _ := common.NewGitignoreMatcher(gitRoot)

	var files []string
	err = filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || cfg.ExcludedDirs[name] {
				return filepath.SkipDir
			}
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if cfg.ExcludedFileExtensions[ext] || cfg.LargeBinaryExtensions[ext] {
			return nil
		}

		if lsp.DetectLanguageID(path) == "" {
			return nil
		}

		if gitMatcher != nil && gitMatcher.IsIgnored(path) {
			return nil
		}

		fi, err := d.Info()
		if err == nil && fi.Size() > cfg.MaxFileSize {
			return nil
		}

		abs, err := filepath.Abs(path)
		if err != nil {
			return nil
		}
		files = append(files, abs)
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Strings(files)
	return files, nil
}
