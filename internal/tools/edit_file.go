package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/angalato08/mcp-language-server/internal/lsp"
)

type TextEdit struct {
	StartLine int    `json:"startLine" jsonschema:"required,description=Start line to replace, inclusive"`
	EndLine   int    `json:"endLine" jsonschema:"required,description=End line to replace, inclusive"`
	NewText   string `json:"newText" jsonschema:"description=Replacement text. Replace with the new text. Leave blank to remove lines."`
}

func ApplyTextEdits(ctx context.Context, client *lsp.Client, filePath string, edits []TextEdit) (string, error) {
	err := client.OpenFile(ctx, filePath)
	if err != nil {
		return "", fmt.Errorf("could not open file: %v", err)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %v", err)
	}

	lineEnding := "\n"
	if bytes.Contains(content, []byte("\r\n")) {
		lineEnding = "\r\n"
	}
	endsWithNewline := len(content) > 0 && bytes.HasSuffix(content, []byte(lineEnding))

	lines := strings.Split(string(content), lineEnding)

	// Sort edits by start line descending (bottom to top)
	// so earlier edits don't shift line numbers for later ones
	sort.Slice(edits, func(i, j int) bool {
		return edits[i].StartLine > edits[j].StartLine
	})

	// Validate no overlapping edits (already sorted descending)
	for i := 1; i < len(edits); i++ {
		if edits[i].EndLine >= edits[i-1].StartLine {
			return "", fmt.Errorf("overlapping edits: lines %d-%d and lines %d-%d",
				edits[i].StartLine, edits[i].EndLine, edits[i-1].StartLine, edits[i-1].EndLine)
		}
	}

	totalRemoved := 0
	totalAdded := 0

	for _, edit := range edits {
		if edit.StartLine < 1 {
			return "", fmt.Errorf("start line must be >= 1, got %d", edit.StartLine)
		}

		startIdx := edit.StartLine - 1
		endIdx := edit.EndLine - 1

		if startIdx >= len(lines) {
			startIdx = len(lines) - 1
		}
		if endIdx >= len(lines) {
			endIdx = len(lines) - 1
		}
		if endIdx < startIdx {
			endIdx = startIdx
		}

		removedCount := endIdx - startIdx + 1
		totalRemoved += removedCount

		var newLines []string
		if edit.NewText != "" {
			newLines = strings.Split(edit.NewText, "\n")
		}
		totalAdded += len(newLines)

		// Splice: lines[:startIdx] + newLines + lines[endIdx+1:]
		result := make([]string, 0, len(lines)-removedCount+len(newLines))
		result = append(result, lines[:startIdx]...)
		result = append(result, newLines...)
		if endIdx+1 < len(lines) {
			result = append(result, lines[endIdx+1:]...)
		}
		lines = result
	}

	// Rebuild file content
	var buf strings.Builder
	for i, line := range lines {
		if i > 0 {
			buf.WriteString(lineEnding)
		}
		buf.WriteString(line)
	}
	if endsWithNewline && !strings.HasSuffix(buf.String(), lineEnding) {
		buf.WriteString(lineEnding)
	}

	if err := os.WriteFile(filePath, []byte(buf.String()), 0644); err != nil {
		return "", fmt.Errorf("failed to write file: %v", err)
	}

	// Notify the LSP server about the change so diagnostics update
	if err := client.NotifyChange(ctx, filePath); err != nil {
		toolsLogger.Warn("Failed to notify LSP of text edit change: %v", err)
	}

	return fmt.Sprintf("Successfully applied text edits. %d lines removed, %d lines added.", totalRemoved, totalAdded), nil
}
