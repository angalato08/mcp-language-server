package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/angalato08/mcp-language-server/internal/lsp"
	"github.com/angalato08/mcp-language-server/internal/protocol"
)

// FindImplementation finds concrete implementations of an interface/abstract symbol
// by name using workspace/symbol lookup followed by textDocument/implementation.
func FindImplementation(ctx context.Context, client *lsp.Client, symbolName string) (string, error) {
	symbolResult, err := client.Symbol(ctx, protocol.WorkspaceSymbolParams{
		Query: symbolName,
	})
	if err != nil {
		return "", fmt.Errorf("failed to fetch symbol: %v", err)
	}

	results, err := symbolResult.Results()
	if err != nil {
		return "", fmt.Errorf("failed to parse results: %v", err)
	}

	var implementations []string
	for _, symbol := range results {
		if !matchSymbol(symbol, symbolName) {
			continue
		}

		loc := symbol.GetLocation()
		if err := client.OpenFile(ctx, loc.URI.Path()); err != nil {
			toolsLogger.Error("Error opening file: %v", err)
			continue
		}

		params := protocol.ImplementationParams{
			TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: loc.URI,
				},
				Position: loc.Range.Start,
			},
		}

		implResult, err := client.Implementation(ctx, params)
		if err != nil {
			toolsLogger.Error("Error getting implementation: %v", err)
			continue
		}

		locations, err := implResult.Locations()
		if err != nil {
			toolsLogger.Error("Error parsing implementation locations: %v", err)
			continue
		}

		for _, implLoc := range locations {
			banner := "---\n\n"
			definition, finalLoc, err := GetFullDefinition(ctx, client, implLoc)
			if err != nil {
				toolsLogger.Error("Error getting definition: %v", err)
				continue
			}

			locationInfo := fmt.Sprintf(
				"Implementation of: %s\n"+
					"File: %s\n"+
					"Range: L%d:C%d - L%d:C%d\n\n",
				symbol.GetName(),
				RelativePath(strings.TrimPrefix(string(finalLoc.URI), "file://")),
				finalLoc.Range.Start.Line+1,
				finalLoc.Range.Start.Character+1,
				finalLoc.Range.End.Line+1,
				finalLoc.Range.End.Character+1,
			)

			definition = addLineNumbers(definition, int(finalLoc.Range.Start.Line)+1)
			implementations = append(implementations, banner+locationInfo+definition+"\n")
		}
	}

	if len(implementations) == 0 {
		return fmt.Sprintf("No implementations found for %s", symbolName), nil
	}

	return strings.Join(implementations, ""), nil
}

// FindImplementationAtPosition finds concrete implementations of a symbol at a
// specific file position using textDocument/implementation.
func FindImplementationAtPosition(ctx context.Context, client *lsp.Client, filePath string, line, column int) (string, error) {
	err := client.OpenFile(ctx, filePath)
	if err != nil {
		return "", fmt.Errorf("could not open file: %v", err)
	}

	params := protocol.ImplementationParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: protocol.DocumentUri("file://" + filePath),
			},
			Position: protocol.Position{
				Line:      uint32(line - 1),
				Character: uint32(column - 1),
			},
		},
	}

	implResult, err := client.Implementation(ctx, params)
	if err != nil {
		return "", fmt.Errorf("failed to get implementation: %v", err)
	}

	locations, err := implResult.Locations()
	if err != nil {
		return "", fmt.Errorf("failed to parse implementation locations: %v", err)
	}

	if len(locations) == 0 {
		return "No implementations found at this position", nil
	}

	var implementations []string
	for _, loc := range locations {
		banner := "---\n\n"
		definition, finalLoc, err := GetFullDefinition(ctx, client, loc)
		if err != nil {
			toolsLogger.Error("Error getting definition: %v", err)
			continue
		}

		locationInfo := fmt.Sprintf(
			"File: %s\n"+
				"Range: L%d:C%d - L%d:C%d\n\n",
			RelativePath(strings.TrimPrefix(string(finalLoc.URI), "file://")),
			finalLoc.Range.Start.Line+1,
			finalLoc.Range.Start.Character+1,
			finalLoc.Range.End.Line+1,
			finalLoc.Range.End.Character+1,
		)

		definition = addLineNumbers(definition, int(finalLoc.Range.Start.Line)+1)
		implementations = append(implementations, banner+locationInfo+definition+"\n")
	}

	return strings.Join(implementations, ""), nil
}
