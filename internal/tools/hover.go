package tools

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/angalato08/mcp-language-server/internal/lsp"
	"github.com/angalato08/mcp-language-server/internal/protocol"
)

// HoverPosition represents a position to hover on (1-indexed).
type HoverPosition struct {
	Line   int
	Column int
}

// HoverResult holds the hover result for a single position.
type HoverResult struct {
	Line    int
	Column  int
	Content string
}

// GetHoverInfo retrieves hover information (type, documentation) for a symbol at the specified position
func GetHoverInfo(ctx context.Context, client *lsp.Client, filePath string, line, column int) (string, error) {
	// Open the file if not already open
	err := client.OpenFile(ctx, filePath)
	if err != nil {
		return "", fmt.Errorf("could not open file: %v", err)
	}

	params := protocol.HoverParams{}

	// Convert 1-indexed line/column to 0-indexed for LSP protocol
	position := protocol.Position{
		Line:      uint32(line - 1),
		Character: uint32(column - 1),
	}
	uri := protocol.DocumentUri("file://" + filePath)
	params.TextDocument = protocol.TextDocumentIdentifier{
		URI: uri,
	}
	params.Position = position

	// Execute the hover request
	hoverResult, err := client.Hover(ctx, params)
	if err != nil {
		return "", fmt.Errorf("failed to get hover information: %v", err)
	}

	var result strings.Builder

	// Process the hover contents
	content := formatHoverContents(hoverResult.Contents)
	if content == "" {
		// Extract the line where the hover was requested
		lineText, err := ExtractTextFromLocation(protocol.Location{
			URI: uri,
			Range: protocol.Range{
				Start: protocol.Position{
					Line:      position.Line,
					Character: 0,
				},
				End: protocol.Position{
					Line:      position.Line + 1,
					Character: 0,
				},
			},
		})
		if err != nil {
			toolsLogger.Warn("failed to extract line at position: %v", err)
		}
		result.WriteString(fmt.Sprintf("No hover information available for this position on the following line:\n%s", lineText))
	} else {
		result.WriteString(content)
	}

	return result.String(), nil
}

func formatHoverContents(contents protocol.Or_Hover_contents) string {
	switch v := contents.Value.(type) {
	case protocol.MarkupContent:
		return v.Value
	case protocol.MarkedString:
		return formatMarkedString(v)
	case []protocol.MarkedString:
		var parts []string
		for _, ms := range v {
			parts = append(parts, formatMarkedString(ms))
		}
		return strings.Join(parts, "\n\n")
	case string:
		return v
	default:
		return ""
	}
}

func formatMarkedString(ms protocol.MarkedString) string {
	switch v := ms.Value.(type) {
	case string:
		return v
	case protocol.MarkedStringWithLanguage:
		if v.Language != "" {
			return fmt.Sprintf("```%s\n%s\n```", v.Language, v.Value)
		}
		return v.Value
	default:
		return ""
	}
}

// BatchHoverInfo retrieves hover information for multiple positions in a single file concurrently.
func BatchHoverInfo(ctx context.Context, client *lsp.Client, filePath string, positions []HoverPosition) ([]HoverResult, error) {
	if len(positions) == 0 {
		return nil, nil
	}

	err := client.OpenFile(ctx, filePath)
	if err != nil {
		return nil, fmt.Errorf("could not open file: %v", err)
	}

	uri := protocol.DocumentUri("file://" + filePath)
	results := make([]HoverResult, len(positions))

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(5)

	for i, pos := range positions {
		i, pos := i, pos
		g.Go(func() error {
			params := protocol.HoverParams{
				TextDocumentPositionParams: protocol.TextDocumentPositionParams{
					TextDocument: protocol.TextDocumentIdentifier{URI: uri},
					Position: protocol.Position{
						Line:      uint32(pos.Line - 1),
						Character: uint32(pos.Column - 1),
					},
				},
			}

			hoverResult, err := client.Hover(gctx, params)
			if err != nil {
				results[i] = HoverResult{Line: pos.Line, Column: pos.Column}
				return nil
			}

			content := formatHoverContents(hoverResult.Contents)
			results[i] = HoverResult{
				Line:    pos.Line,
				Column:  pos.Column,
				Content: content,
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return results, nil
}

// FormatBatchHoverResults formats batch hover results into a human-readable string.
func FormatBatchHoverResults(filePath string, results []HoverResult) string {
	if len(results) == 0 {
		return "No positions provided"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Hover results for %s\n", RelativePath(filePath)))

	for i, r := range results {
		if i > 0 {
			sb.WriteString("\n---\n")
		}
		sb.WriteString(fmt.Sprintf("\n## L%d:%d\n\n", r.Line, r.Column))
		if r.Content == "" {
			sb.WriteString("No hover information available")
		} else {
			sb.WriteString(r.Content)
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
