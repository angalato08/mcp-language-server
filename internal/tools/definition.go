package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/angalato08/mcp-language-server/internal/lsp"
	"github.com/angalato08/mcp-language-server/internal/protocol"
)

func ReadDefinition(ctx context.Context, client *lsp.Client, symbolName string) (string, error) {
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

	var definitions []string
	for _, symbol := range results {
		kind := ""
		container := ""

		// Skip symbols that we are not looking for. workspace/symbol may return
		// a large number of fuzzy matches.
		if !matchSymbol(symbol, symbolName) {
			continue
		}

		if v, ok := symbol.(*protocol.SymbolInformation); ok {
			kind = fmt.Sprintf("Kind: %s\n", protocol.TableKindMap[v.Kind])
			if v.ContainerName != "" {
				container = fmt.Sprintf("Container Name: %s\n", v.ContainerName)
			}
		}

		toolsLogger.Debug("Found symbol: %s", symbol.GetName())
		loc := symbol.GetLocation()

		banner := "---\n\n"
		definition, finalLoc, err := GetFullDefinition(ctx, client, loc, symbolName)
		if err != nil {
			toolsLogger.Error("Error getting definition: %v", err)
			continue
		}

		locationInfo := fmt.Sprintf(
			"Symbol: %s\n"+
				"File: %s\n"+
				kind+
				container+
				"Range: L%d:C%d - L%d:C%d\n\n",
			symbol.GetName(),
			RelativePath(strings.TrimPrefix(string(finalLoc.URI), "file://")),
			finalLoc.Range.Start.Line+1,
			finalLoc.Range.Start.Character+1,
			finalLoc.Range.End.Line+1,
			finalLoc.Range.End.Character+1,
		)

		definition = addLineNumbers(definition, int(finalLoc.Range.Start.Line)+1)
		definitions = append(definitions, banner+locationInfo+definition+"\n")
	}

	if len(definitions) == 0 {
		return fmt.Sprintf("%s not found", symbolName), nil
	}

	return strings.Join(definitions, ""), nil
}

func ReadDefinitionAtPosition(ctx context.Context, client *lsp.Client, filePath string, line, column int) (string, error) {
	// Open the file if not already open
	err := client.OpenFile(ctx, filePath)
	if err != nil {
		return "", fmt.Errorf("could not open file: %v", err)
	}

	params := protocol.DefinitionParams{
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

	defResult, err := client.Definition(ctx, params)
	if err != nil {
		return "", fmt.Errorf("failed to get definition: %v", err)
	}

	locations, err := defResult.Locations()
	if err != nil {
		return "", fmt.Errorf("failed to parse definition locations: %v", err)
	}

	if len(locations) == 0 {
		return "No definition found at this position", nil
	}

	var definitions []string
	for _, loc := range locations {
		banner := "---\n\n"
		definition, finalLoc, err := GetFullDefinition(ctx, client, loc, "")
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
		definitions = append(definitions, banner+locationInfo+definition+"\n")
	}

	return strings.Join(definitions, ""), nil
}

func ReadTypeDefinition(ctx context.Context, client *lsp.Client, symbolName string) (string, error) {
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

	var definitions []string
	for _, symbol := range results {
		if !matchSymbol(symbol, symbolName) {
			continue
		}

		loc := symbol.GetLocation()
		// Open the file to ensure the LSP server is aware of it
		err := client.OpenFile(ctx, loc.URI.Path())
		if err != nil {
			toolsLogger.Error("Error opening file: %v", err)
			continue
		}

		params := protocol.TypeDefinitionParams{
			TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: loc.URI,
				},
				Position: loc.Range.Start,
			},
		}

		typeDefResult, err := client.TypeDefinition(ctx, params)
		if err != nil {
			toolsLogger.Error("Error getting type definition: %v", err)
			continue
		}

		locations, err := typeDefResult.Locations()
		if err != nil {
			toolsLogger.Error("Error parsing type definition locations: %v", err)
			continue
		}

		for _, loc := range locations {
			definitions = append(definitions, formatTypeDefinition(ctx, client, loc, symbol.GetName()))
		}
	}

	if len(definitions) == 0 {
		return fmt.Sprintf("Type definition for %s not found", symbolName), nil
	}

	return strings.Join(definitions, ""), nil
}

func ReadTypeDefinitionAtPosition(ctx context.Context, client *lsp.Client, filePath string, line, column int) (string, error) {
	// Open the file if not already open
	err := client.OpenFile(ctx, filePath)
	if err != nil {
		return "", fmt.Errorf("could not open file: %v", err)
	}

	params := protocol.TypeDefinitionParams{
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

	typeDefResult, err := client.TypeDefinition(ctx, params)
	if err != nil {
		return "", fmt.Errorf("failed to get type definition: %v", err)
	}

	locations, err := typeDefResult.Locations()
	if err != nil {
		return "", fmt.Errorf("failed to parse type definition locations: %v", err)
	}

	if len(locations) == 0 {
		return "No type definition found at this position", nil
	}

	var definitions []string
	for _, loc := range locations {
		definitions = append(definitions, formatTypeDefinition(ctx, client, loc, ""))
	}

	return strings.Join(definitions, ""), nil
}

// formatTypeDefinition returns a concise type summary via hover, falling back
// to a truncated source definition when hover information is unavailable.
func formatTypeDefinition(ctx context.Context, client *lsp.Client, loc protocol.Location, symbolLabel string) string {
	defFilePath := strings.TrimPrefix(string(loc.URI), "file://")
	displayPath := RelativePath(defFilePath)

	banner := "---\n\n"
	locationInfo := fmt.Sprintf(
		"File: %s\n"+
			"Range: L%d:C%d - L%d:C%d\n",
		displayPath,
		loc.Range.Start.Line+1,
		loc.Range.Start.Character+1,
		loc.Range.End.Line+1,
		loc.Range.End.Character+1,
	)
	if symbolLabel != "" {
		locationInfo = fmt.Sprintf("Type of Symbol: %s\n", symbolLabel) + locationInfo
	}

	// Try hover first — produces a concise type signature
	hoverParams := protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: loc.URI},
			Position:     loc.Range.Start,
		},
	}
	hoverResult, err := client.Hover(ctx, hoverParams)
	if err == nil {
		content := formatHoverContents(hoverResult.Contents)
		if content != "" {
			return banner + locationInfo + "\n" + content + "\n"
		}
	}

	// Fall back to full definition with a line cap
	definition, finalLoc, err := GetFullDefinition(ctx, client, loc, "")
	if err != nil {
		toolsLogger.Error("Error getting definition: %v", err)
		return banner + locationInfo + "\n"
	}

	// Update location info with the resolved range
	locationInfo = fmt.Sprintf(
		"File: %s\n"+
			"Range: L%d:C%d - L%d:C%d\n",
		RelativePath(strings.TrimPrefix(string(finalLoc.URI), "file://")),
		finalLoc.Range.Start.Line+1,
		finalLoc.Range.Start.Character+1,
		finalLoc.Range.End.Line+1,
		finalLoc.Range.End.Character+1,
	)
	if symbolLabel != "" {
		locationInfo = fmt.Sprintf("Type of Symbol: %s\n", symbolLabel) + locationInfo
	}

	// Truncate large definitions to keep output practical
	const maxDefLines = 60
	lines := strings.Split(definition, "\n")
	truncated := false
	if len(lines) > maxDefLines {
		lines = lines[:maxDefLines]
		truncated = true
	}
	definition = strings.Join(lines, "\n")

	definition = addLineNumbers(definition, int(finalLoc.Range.Start.Line)+1)
	result := banner + locationInfo + "\n" + definition + "\n"
	if truncated {
		result += fmt.Sprintf("\n... truncated (showing first %d lines of definition, use `definition` or `get_definition` for full source)\n", maxDefLines)
	}
	return result
}
