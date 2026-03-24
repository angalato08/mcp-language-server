package tools

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/angalato08/mcp-language-server/internal/lsp"
	"github.com/angalato08/mcp-language-server/internal/protocol"
)

func FindReferences(ctx context.Context, client *lsp.Client, symbolName string, limit, offset int, outputFormat string) (string, error) {
	// First get the symbol location like ReadDefinition does
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

	var allReferences []string
	for _, symbol := range results {
		if !matchSymbol(symbol, symbolName) {
			continue
		}

		// Get the location of the symbol
		loc := symbol.GetLocation()

		// File is likely to be opened already, but may not be.
		err := client.OpenFile(ctx, loc.URI.Path())
		if err != nil {
			toolsLogger.Error("Error opening file: %v", err)
			continue
		}

		// The workspace/symbol range start may point to the beginning of the
		// declaration (e.g. "pub" in "pub fn helper_function"), not the symbol
		// name itself. Find the actual name position within the declaration.
		position := loc.Range.Start
		filePath := strings.TrimPrefix(string(loc.URI), "file://")
		content, readErr := os.ReadFile(filePath)
		if readErr == nil {
			lines := strings.Split(string(content), "\n")
			startLine := int(loc.Range.Start.Line)
			endLine := int(loc.Range.End.Line)
			if endLine >= len(lines) {
				endLine = len(lines) - 1
			}
			for lineNum := startLine; lineNum <= endLine; lineNum++ {
				col := strings.Index(lines[lineNum], symbolName)
				if col >= 0 {
					position = protocol.Position{
						Line:      uint32(lineNum),
						Character: uint32(col),
					}
					break
				}
			}
		}

		// Use LSP references request with correct params structure
		refsParams := protocol.ReferenceParams{
			TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: loc.URI,
				},
				Position: position,
			},
			Context: protocol.ReferenceContext{
				IncludeDeclaration: false,
			},
		}
		refs, err := client.References(ctx, refsParams)
		if err != nil {
			return "", fmt.Errorf("failed to get references: %v", err)
		}

		if len(refs) == 0 {
			continue
		}

		formatted, err := formatReferences(ctx, client, refs, limit, offset, outputFormat)
		if err != nil {
			return "", err
		}
		if formatted != "" {
			allReferences = append(allReferences, formatted)
		}
	}

	if len(allReferences) == 0 {
		return fmt.Sprintf("No references found for symbol: %s", symbolName), nil
	}

	return strings.Join(allReferences, "\n"), nil
}

func FindReferencesAtPosition(ctx context.Context, client *lsp.Client, filePath string, line, column, limit, offset int, outputFormat string) (string, error) {
	// Open the file if not already open
	err := client.OpenFile(ctx, filePath)
	if err != nil {
		return "", fmt.Errorf("could not open file: %v", err)
	}

	params := protocol.ReferenceParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: protocol.DocumentUri("file://" + filePath),
			},
			Position: protocol.Position{
				Line:      uint32(line - 1),
				Character: uint32(column - 1),
			},
		},
		Context: protocol.ReferenceContext{
			IncludeDeclaration: true,
		},
	}

	refs, err := client.References(ctx, params)
	if err != nil {
		return "", fmt.Errorf("failed to get references: %v", err)
	}

	if len(refs) == 0 {
		return "No references found at this position", nil
	}

	return formatReferences(ctx, client, refs, limit, offset, outputFormat)
}

func formatReferences(ctx context.Context, client *lsp.Client, refs []protocol.Location, limit, offset int, outputFormat string) (string, error) {
	// Get context lines from environment variable
	contextLines := 5
	if envLines := os.Getenv("LSP_CONTEXT_LINES"); envLines != "" {
		if val, err := strconv.Atoi(envLines); err == nil && val >= 0 {
			contextLines = val
		}
	}

	totalRefCount := len(refs)

	// Apply offset
	if offset < 0 {
		offset = 0
	}
	if offset > 0 && offset < len(refs) {
		refs = refs[offset:]
	} else if offset >= len(refs) {
		return fmt.Sprintf("No more references (offset %d exceeds total %d)", offset, totalRefCount), nil
	}

	// Apply limit
	if limit > 0 && len(refs) > limit {
		refs = refs[:limit]
	}

	if outputFormat == "short" {
		return formatReferencesShort(refs, totalRefCount, limit, offset), nil
	}

	// Group references by file
	refsByFile := make(map[protocol.DocumentUri][]protocol.Location)
	for _, ref := range refs {
		refsByFile[ref.URI] = append(refsByFile[ref.URI], ref)
	}

	// Get sorted list of URIs
	uris := make([]string, 0, len(refsByFile))
	for uri := range refsByFile {
		uris = append(uris, string(uri))
	}
	sort.Strings(uris)

	var allReferences []string
	// Prepend limit notice if results were truncated
	if limit > 0 && totalRefCount > offset+limit {
		allReferences = append(allReferences, fmt.Sprintf("Showing %d of %d references (offset %d). Use offset=%d to see more.",
			len(refs), totalRefCount, offset, offset+limit))
	}

	// Process each file's references in sorted order
	for _, uriStr := range uris {
		uri := protocol.DocumentUri(uriStr)
		fileRefs := refsByFile[uri]
		absPath := strings.TrimPrefix(uriStr, "file://")
		displayPath := RelativePath(absPath)

		// Format file header
		fileInfo := fmt.Sprintf("---\n\n%s\nReferences in File: %d\n",
			displayPath,
			len(fileRefs),
		)

		// Format locations with context
		fileContent, err := os.ReadFile(absPath)
		if err != nil {
			// Log error but continue with other files
			allReferences = append(allReferences, fileInfo+"\nError reading file: "+err.Error())
			continue
		}

		lines := strings.Split(string(fileContent), "\n")

		// Track reference locations for header display
		var locStrings []string
		for _, ref := range fileRefs {
			locStr := fmt.Sprintf("L%d:C%d",
				ref.Range.Start.Line+1,
				ref.Range.Start.Character+1)
			locStrings = append(locStrings, locStr)
		}

		// Collect lines to display using the utility function
		linesToShow, err := GetLineRangesToDisplay(ctx, client, fileRefs, len(lines), contextLines)
		if err != nil {
			// Log error but continue with other files
			continue
		}

		// Convert to line ranges using the utility function
		lineRanges := ConvertLinesToRanges(linesToShow, len(lines))

		// Format with locations in header
		formattedOutput := fileInfo
		if len(locStrings) > 0 {
			formattedOutput += "At: " + strings.Join(locStrings, ", ") + "\n"
		}

		// Format the content with ranges
		formattedOutput += "\n" + FormatLinesWithRanges(lines, lineRanges)
		allReferences = append(allReferences, formattedOutput)
	}

	return strings.Join(allReferences, "\n"), nil
}

func formatReferencesShort(refs []protocol.Location, totalRefCount, limit, offset int) string {
	// Group references by file
	type fileEntry struct {
		path  string
		lines []int
	}
	fileOrder := []string{}
	fileMap := make(map[string]*fileEntry)

	for _, ref := range refs {
		absPath := strings.TrimPrefix(string(ref.URI), "file://")
		displayPath := RelativePath(absPath)
		entry, exists := fileMap[displayPath]
		if !exists {
			entry = &fileEntry{path: displayPath}
			fileMap[displayPath] = entry
			fileOrder = append(fileOrder, displayPath)
		}
		entry.lines = append(entry.lines, int(ref.Range.Start.Line)+1)
	}

	sort.Strings(fileOrder)

	var b strings.Builder
	b.WriteString(fmt.Sprintf("%d references in %d files:\n", totalRefCount, len(fileOrder)))

	for _, path := range fileOrder {
		entry := fileMap[path]
		lineStrs := make([]string, len(entry.lines))
		for i, l := range entry.lines {
			lineStrs[i] = fmt.Sprintf("L%d", l)
		}
		b.WriteString(fmt.Sprintf("  %s: %s (%d refs)\n", path, strings.Join(lineStrs, ", "), len(entry.lines)))
	}

	if limit > 0 && totalRefCount > offset+limit {
		b.WriteString(fmt.Sprintf("Use offset=%d to see more.\n", offset+limit))
	}

	return b.String()
}
