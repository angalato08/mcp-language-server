package tools

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/angalato08/mcp-language-server/internal/lsp"
	"github.com/angalato08/mcp-language-server/internal/protocol"
	"github.com/angalato08/mcp-language-server/internal/utilities"
)

// FormatFile requests the LSP server to format an entire file and applies the edits.
// Returns a clear error if the language server does not support formatting.
func FormatFile(ctx context.Context, client *lsp.Client, filePath string, tabSize int, insertSpaces bool) (string, error) {
	err := client.OpenFile(ctx, filePath)
	if err != nil {
		return "", fmt.Errorf("could not open file: %v", err)
	}

	uri := protocol.DocumentUri("file://" + filePath)
	params := protocol.DocumentFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: uri,
		},
		Options: protocol.FormattingOptions{
			TabSize:                uint32(tabSize),
			InsertSpaces:           insertSpaces,
			TrimTrailingWhitespace: true,
			InsertFinalNewline:     true,
		},
	}

	edits, err := client.Formatting(ctx, params)
	if err != nil {
		return "", fmt.Errorf("formatting not supported or failed: %v", err)
	}

	// Filter out identity edits (where new text matches old text at the edit range).
	// Some language servers may return no-op edits for already-formatted files.
	edits = filterIdentityEdits(filePath, edits)

	if len(edits) == 0 {
		return "File is already formatted. No changes needed.", nil
	}

	if err := utilities.ApplyTextEdits(uri, edits); err != nil {
		return "", fmt.Errorf("failed to apply formatting edits: %v", err)
	}

	// Notify the LSP server about the change
	if err := client.NotifyChange(ctx, filePath); err != nil {
		toolsLogger.Warn("Failed to notify LSP of formatting change: %v", err)
	}

	var result strings.Builder
	result.WriteString(fmt.Sprintf("Formatted %s\n", RelativePath(filePath)))
	result.WriteString(fmt.Sprintf("Applied %d edit(s)\n", len(edits)))

	return result.String(), nil
}

// filterIdentityEdits removes edits where the new text is identical to the
// existing text in the file at the edit range. Some language servers (e.g.
// clangd) may return such no-op edits for already-formatted files.
func filterIdentityEdits(filePath string, edits []protocol.TextEdit) []protocol.TextEdit {
	content, err := os.ReadFile(filePath)
	if err != nil {
		// If we can't read the file, return all edits unfiltered
		return edits
	}

	lines := strings.Split(string(content), "\n")

	var filtered []protocol.TextEdit
	for _, edit := range edits {
		startLine := int(edit.Range.Start.Line)
		endLine := int(edit.Range.End.Line)
		startChar := int(edit.Range.Start.Character)
		endChar := int(edit.Range.End.Character)

		if startLine < 0 || startLine >= len(lines) || endLine < 0 || endLine >= len(lines) {
			// Can't validate, keep the edit
			filtered = append(filtered, edit)
			continue
		}

		// Extract the old text from the range
		var oldText string
		if startLine == endLine {
			line := lines[startLine]
			if startChar > len(line) {
				startChar = len(line)
			}
			if endChar > len(line) {
				endChar = len(line)
			}
			oldText = line[startChar:endChar]
		} else {
			var parts []string
			// First line from startChar
			firstLine := lines[startLine]
			if startChar > len(firstLine) {
				startChar = len(firstLine)
			}
			parts = append(parts, firstLine[startChar:])
			// Middle lines
			for i := startLine + 1; i < endLine; i++ {
				parts = append(parts, lines[i])
			}
			// Last line up to endChar
			lastLine := lines[endLine]
			if endChar > len(lastLine) {
				endChar = len(lastLine)
			}
			parts = append(parts, lastLine[:endChar])
			oldText = strings.Join(parts, "\n")
		}

		if oldText != edit.NewText {
			filtered = append(filtered, edit)
		}
	}

	return filtered
}
