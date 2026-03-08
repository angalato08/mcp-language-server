package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/isaacphi/mcp-language-server/internal/lsp"
	"github.com/isaacphi/mcp-language-server/internal/protocol"
)

// GetDocumentSymbols returns all symbols in a file with hierarchy.
func GetDocumentSymbols(ctx context.Context, client *lsp.Client, filePath string) (string, error) {
	err := client.OpenFile(ctx, filePath)
	if err != nil {
		return "", fmt.Errorf("could not open file: %v", err)
	}

	uri := protocol.DocumentUri("file://" + filePath)
	params := protocol.DocumentSymbolParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	}

	result, err := client.DocumentSymbol(ctx, params)
	if err != nil {
		return "", fmt.Errorf("failed to get document symbols: %v", err)
	}

	symbols, err := result.Results()
	if err != nil {
		return "", fmt.Errorf("failed to parse document symbol results: %v", err)
	}

	if len(symbols) == 0 {
		return fmt.Sprintf("No symbols found in %s", RelativePath(filePath)), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Symbols in %s:\n\n", RelativePath(filePath)))

	for _, sym := range symbols {
		switch ds := sym.(type) {
		case *protocol.DocumentSymbol:
			formatDocumentSymbol(&sb, ds, 1)
		case *protocol.SymbolInformation:
			formatSymbolInformation(&sb, ds)
		}
	}

	return sb.String(), nil
}

func formatDocumentSymbol(sb *strings.Builder, sym *protocol.DocumentSymbol, indent int) {
	prefix := strings.Repeat("  ", indent)
	kind := protocol.TableKindMap[sym.Kind]
	line := sym.Range.Start.Line + 1
	sb.WriteString(fmt.Sprintf("%s%s %s (L%d)\n", prefix, kind, sym.Name, line))
	for i := range sym.Children {
		formatDocumentSymbol(sb, &sym.Children[i], indent+1)
	}
}

func formatSymbolInformation(sb *strings.Builder, sym *protocol.SymbolInformation) {
	kind := protocol.TableKindMap[sym.Kind]
	line := sym.Location.Range.Start.Line + 1
	container := ""
	if sym.ContainerName != "" {
		container = fmt.Sprintf(" [%s]", sym.ContainerName)
	}
	sb.WriteString(fmt.Sprintf("  %s %s%s (L%d)\n", kind, sym.Name, container, line))
}

// SearchWorkspaceSymbols searches for symbols across the workspace by name.
func SearchWorkspaceSymbols(ctx context.Context, client *lsp.Client, query string, limit int) (string, error) {
	symbolResult, err := client.Symbol(ctx, protocol.WorkspaceSymbolParams{
		Query: query,
	})
	if err != nil {
		return "", fmt.Errorf("failed to search workspace symbols: %v", err)
	}

	results, err := symbolResult.Results()
	if err != nil {
		return "", fmt.Errorf("failed to parse workspace symbol results: %v", err)
	}

	if len(results) == 0 {
		return fmt.Sprintf("No workspace symbols matching '%s'", query), nil
	}

	// Apply limit
	total := len(results)
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Workspace symbols matching '%s':\n\n", query))

	for _, sym := range results {
		loc := sym.GetLocation()
		filePath := RelativePath(strings.TrimPrefix(string(loc.URI), "file://"))
		line := loc.Range.Start.Line + 1

		kind := ""
		container := ""
		switch s := sym.(type) {
		case *protocol.SymbolInformation:
			kind = protocol.TableKindMap[s.Kind]
			if s.ContainerName != "" {
				container = s.ContainerName + "."
			}
		case *protocol.WorkspaceSymbol:
			kind = protocol.TableKindMap[s.Kind]
			if s.ContainerName != "" {
				container = s.ContainerName + "."
			}
		}

		name := sym.GetName()
		sb.WriteString(fmt.Sprintf("  %s %s%s — %s:%d\n", kind, container, name, filePath, line))
	}

	if limit > 0 && total > limit {
		sb.WriteString(fmt.Sprintf("\nShowing %d of %d symbols\n", limit, total))
	} else {
		sb.WriteString(fmt.Sprintf("\nFound %d symbols\n", total))
	}

	return sb.String(), nil
}
