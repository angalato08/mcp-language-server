package tools

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/angalato08/mcp-language-server/internal/lsp"
	"github.com/angalato08/mcp-language-server/internal/protocol"
)

// matchSymbol checks if a symbol matches the given query name.
// It handles qualified names (Type.Method, Type::Method), container matching,
// and method name matching for various languages.
func matchSymbol(symbol protocol.WorkspaceSymbolResult, query string) bool {
	name := symbol.GetName()

	// Exact match is always accepted
	if name == query {
		return true
	}

	// For SymbolInformation, we have richer matching options
	si, ok := symbol.(*protocol.SymbolInformation)
	if !ok {
		return false
	}

	if strings.Contains(query, ".") {
		// Qualified name like "Type.Method"
		parts := strings.SplitN(query, ".", 2)
		if len(parts) == 2 {
			// Match: symbol name is the method and container is the type
			if si.Name == parts[1] && si.ContainerName == parts[0] {
				return true
			}
			// Match: symbol name is already qualified as "Type.Method"
			fullDot := si.ContainerName + "." + si.Name
			if fullDot == query {
				return true
			}
		}
	}

	if strings.Contains(query, "::") {
		// C++ qualified name like "Namespace::Class::Method"
		parts := strings.SplitN(query, "::", 2)
		if len(parts) == 2 {
			if si.Name == parts[1] && strings.HasSuffix(si.ContainerName, parts[0]) {
				return true
			}
			fullCpp := si.ContainerName + "::" + si.Name
			if fullCpp == query {
				return true
			}
		}
	}

	// For methods, match if the symbol name ends with the separator + query
	if si.Kind == protocol.Method {
		if strings.HasSuffix(name, "::"+query) || strings.HasSuffix(name, "."+query) {
			return true
		}
	}

	return false
}

// Gets the full code block surrounding the start of the input location
func GetFullDefinition(ctx context.Context, client *lsp.Client, startLocation protocol.Location, symbolName string) (string, protocol.Location, error) {
	// Ensure the file is opened so the LSP server can provide document symbols
	filePath, err := url.PathUnescape(strings.TrimPrefix(string(startLocation.URI), "file://"))
	if err != nil {
		return "", protocol.Location{}, fmt.Errorf("failed to unescape URI: %w", err)
	}
	if err := client.OpenFile(ctx, filePath); err != nil {
		return "", protocol.Location{}, fmt.Errorf("failed to open file: %w", err)
	}

	symParams := protocol.DocumentSymbolParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: startLocation.URI,
		},
	}

	// Get all symbols in document
	symResult, err := client.DocumentSymbol(ctx, symParams)
	if err != nil {
		return "", protocol.Location{}, fmt.Errorf("failed to get document symbols: %w", err)
	}

	symbols, err := symResult.Results()
	if err != nil {
		return "", protocol.Location{}, fmt.Errorf("failed to process document symbols: %w", err)
	}

	var symbolRange protocol.Range
	found := false

	// Search for symbol at startLocation
	var searchSymbols func(symbols []protocol.DocumentSymbolResult) bool
	searchSymbols = func(symbols []protocol.DocumentSymbolResult) bool {
		for _, sym := range symbols {
			if containsPosition(sym.GetRange(), startLocation.Range.Start) {
				// Check children first for a more specific match
				if ds, ok := sym.(*protocol.DocumentSymbol); ok && len(ds.Children) > 0 {
					childSymbols := make([]protocol.DocumentSymbolResult, len(ds.Children))
					for i := range ds.Children {
						childSymbols[i] = &ds.Children[i]
					}
					if searchSymbols(childSymbols) {
						return true // Child matched — more specific
					}
				}
				// No more specific child — use this symbol
				symbolRange = sym.GetRange()
				found = true
				return true
			}
		}
		return false
	}

	found = searchSymbols(symbols)

	// When symbolName is provided and the matched symbol is a container with
	// children, try to narrow down to a child whose Name matches symbolName.
	if found && symbolName != "" {
		var refineSearch func([]protocol.DocumentSymbolResult) bool
		refineSearch = func(syms []protocol.DocumentSymbolResult) bool {
			for _, sym := range syms {
				ds, ok := sym.(*protocol.DocumentSymbol)
				if !ok {
					continue
				}
				if ds.Name == symbolName {
					symbolRange = ds.Range
					return true
				}
				if len(ds.Children) > 0 {
					childSymbols := make([]protocol.DocumentSymbolResult, len(ds.Children))
					for i := range ds.Children {
						childSymbols[i] = &ds.Children[i]
					}
					if refineSearch(childSymbols) {
						return true
					}
				}
			}
			return false
		}

		// Only refine if the current match is a container with children
		for _, sym := range symbols {
			ds, ok := sym.(*protocol.DocumentSymbol)
			if !ok {
				continue
			}
			if ds.Range == symbolRange && len(ds.Children) > 0 {
				childSymbols := make([]protocol.DocumentSymbolResult, len(ds.Children))
				for i := range ds.Children {
					childSymbols[i] = &ds.Children[i]
				}
				refineSearch(childSymbols)
				break
			}
		}
	}

	if found {
		// Convert URI to filesystem path
		filePath, err := url.PathUnescape(strings.TrimPrefix(string(startLocation.URI), "file://"))
		if err != nil {
			return "", protocol.Location{}, fmt.Errorf("failed to unescape URI: %w", err)
		}

		// Read the file to get the full lines of the definition
		// because we may have a start and end column
		content, err := os.ReadFile(filePath)
		if err != nil {
			return "", protocol.Location{}, fmt.Errorf("failed to read file: %w", err)
		}

		lines := strings.Split(string(content), "\n")

		// Extend start to beginning of line
		symbolRange.Start.Character = 0

		// Get the line at the end of the range
		if int(symbolRange.End.Line) >= len(lines) {
			return "", protocol.Location{}, fmt.Errorf("line number out of range")
		}

		line := lines[symbolRange.End.Line]
		trimmedLine := strings.TrimSpace(line)

		// In some cases (python), constant definitions do not include the full body and instead
		// end with an opening bracket. In this case, parse the file until the closing bracket
		if len(trimmedLine) > 0 {
			lastChar := trimmedLine[len(trimmedLine)-1]
			if lastChar == '(' || lastChar == '[' || lastChar == '{' || lastChar == '<' {
				// Find matching closing bracket
				bracketStack := []rune{rune(lastChar)}
				lineNum := symbolRange.End.Line + 1

				for lineNum < uint32(len(lines)) {
					line := lines[lineNum]
					for pos, char := range line {
						if char == '(' || char == '[' || char == '{' || char == '<' {
							bracketStack = append(bracketStack, char)
						} else if char == ')' || char == ']' || char == '}' || char == '>' {
							if len(bracketStack) > 0 {
								lastOpen := bracketStack[len(bracketStack)-1]
								if (lastOpen == '(' && char == ')') ||
									(lastOpen == '[' && char == ']') ||
									(lastOpen == '{' && char == '}') ||
									(lastOpen == '<' && char == '>') {
									bracketStack = bracketStack[:len(bracketStack)-1]
									if len(bracketStack) == 0 {
										// Found matching bracket - update range
										symbolRange.End.Line = lineNum
										symbolRange.End.Character = uint32(pos + 1)
										goto foundClosing
									}
								}
							}
						}
					}
					lineNum++
				}
			foundClosing:
			}
		}

		// Update location with new range
		startLocation.Range = symbolRange

		// Return the text within the range
		if int(symbolRange.End.Line) >= len(lines) {
			return "", protocol.Location{}, fmt.Errorf("end line out of range")
		}

		selectedLines := lines[symbolRange.Start.Line : symbolRange.End.Line+1]
		return strings.Join(selectedLines, "\n"), startLocation, nil
	}

	return "", protocol.Location{}, fmt.Errorf("symbol not found")
}

// GetLineRangesToDisplay determines which lines should be displayed for a set of locations
func GetLineRangesToDisplay(ctx context.Context, client *lsp.Client, locations []protocol.Location, totalLines int, contextLines int) (map[int]bool, error) {
	// Set to track which lines need to be displayed
	linesToShow := make(map[int]bool)

	// For each location, get its container and add relevant lines
	for _, loc := range locations {
		// Use GetFullDefinition to find container
		_, containerLoc, err := GetFullDefinition(ctx, client, loc, "")
		if err != nil {
			// If container not found, just use the location's line
			refLine := int(loc.Range.Start.Line)
			linesToShow[refLine] = true

			// Add context lines
			for i := refLine - contextLines; i <= refLine+contextLines; i++ {
				if i >= 0 && i < totalLines {
					linesToShow[i] = true
				}
			}
			continue
		}

		// Add container start and end lines
		containerStart := int(containerLoc.Range.Start.Line)
		containerEnd := int(containerLoc.Range.End.Line)
		linesToShow[containerStart] = true
		// linesToShow[containerEnd] = true

		// Add the reference line
		refLine := int(loc.Range.Start.Line)
		linesToShow[refLine] = true

		// Add context lines around the reference
		for i := refLine - contextLines; i <= refLine+contextLines; i++ {
			if i >= 0 && i < totalLines && i >= containerStart && i <= containerEnd {
				linesToShow[i] = true
			}
		}
	}

	return linesToShow, nil
}
