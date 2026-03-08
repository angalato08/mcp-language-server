package tools

import (
	"context"
	"fmt"
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
