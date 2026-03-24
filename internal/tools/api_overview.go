package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/angalato08/mcp-language-server/internal/lsp"
	"github.com/angalato08/mcp-language-server/internal/protocol"
)

// symbolTarget represents a symbol to hover on, with metadata for formatting.
type symbolTarget struct {
	Name   string
	Kind   protocol.SymbolKind
	Line   int // 1-indexed for display
	Column int // 1-indexed for hover
	Indent int // nesting level (0 = top-level)
}

// GetAPIOverview returns signatures and documentation for all symbols in a file.
// It combines document_symbols with batch hover to provide a complete API surface view.
func GetAPIOverview(ctx context.Context, client *lsp.Client, filePath string, symbolKinds []protocol.SymbolKind, includeChildren bool) (string, error) {
	synced, err := client.SyncFileFromDisk(ctx, filePath)
	if err != nil {
		return "", fmt.Errorf("could not sync file: %v", err)
	}

	uri := protocol.DocumentUri("file://" + filePath)
	if synced {
		client.WaitForDiagnostics(uri, 3*time.Second)
	} else {
		client.WaitForDiagnostics(uri, 1*time.Second)
	}
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

	// Build kind filter
	kindFilter := make(map[protocol.SymbolKind]bool)
	for _, k := range symbolKinds {
		kindFilter[k] = true
	}

	// Collect symbol targets
	targets := collectSymbolTargets(symbols, kindFilter, includeChildren)
	if len(targets) == 0 {
		return fmt.Sprintf("No matching symbols found in %s", RelativePath(filePath)), nil
	}

	// Build hover positions
	positions := make([]HoverPosition, len(targets))
	for i, t := range targets {
		positions[i] = HoverPosition{Line: t.Line, Column: t.Column}
	}

	// Batch hover
	hoverResults, err := BatchHoverInfo(ctx, client, filePath, positions)
	if err != nil {
		return "", fmt.Errorf("failed to get hover information: %v", err)
	}

	// Format output
	return formatAPIOverview(filePath, targets, hoverResults), nil
}

// collectSymbolTargets walks the symbol tree and collects hover targets.
func collectSymbolTargets(symbols []protocol.DocumentSymbolResult, kindFilter map[protocol.SymbolKind]bool, includeChildren bool) []symbolTarget {
	var targets []symbolTarget

	for _, sym := range symbols {
		switch ds := sym.(type) {
		case *protocol.DocumentSymbol:
			collectDocumentSymbolTargets(ds, kindFilter, includeChildren, 0, &targets)
		case *protocol.SymbolInformation:
			if len(kindFilter) > 0 && !kindFilter[ds.Kind] {
				continue
			}
			targets = append(targets, symbolTarget{
				Name:   ds.Name,
				Kind:   ds.Kind,
				Line:   int(ds.Location.Range.Start.Line) + 1,
				Column: int(ds.Location.Range.Start.Character) + 1,
				Indent: 0,
			})
		}
	}

	return targets
}

func collectDocumentSymbolTargets(sym *protocol.DocumentSymbol, kindFilter map[protocol.SymbolKind]bool, includeChildren bool, indent int, targets *[]symbolTarget) {
	if indent == 0 && len(kindFilter) > 0 && !kindFilter[sym.Kind] {
		return
	}

	*targets = append(*targets, symbolTarget{
		Name:   sym.Name,
		Kind:   sym.Kind,
		Line:   int(sym.SelectionRange.Start.Line) + 1,
		Column: int(sym.SelectionRange.Start.Character) + 1,
		Indent: indent,
	})

	if includeChildren {
		for i := range sym.Children {
			collectDocumentSymbolTargets(&sym.Children[i], kindFilter, includeChildren, indent+1, targets)
		}
	}
}

func formatAPIOverview(filePath string, targets []symbolTarget, hoverResults []HoverResult) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("API Overview for %s\n", RelativePath(filePath)))

	firstBlock := true
	for i, t := range targets {
		kindName := protocol.TableKindMap[t.Kind]
		headerLevel := "###"
		prefix := ""
		if t.Indent > 0 {
			level := 3 + t.Indent
			if level > 4 {
				level = 4
			}
			headerLevel = strings.Repeat("#", level)
			prefix = strings.Repeat("  ", t.Indent)
		}

		if t.Indent == 0 && !firstBlock {
			sb.WriteString("\n---\n")
		}
		firstBlock = false

		sb.WriteString(fmt.Sprintf("\n%s%s %s %s (L%d)\n", prefix, headerLevel, kindName, t.Name, t.Line))

		if i < len(hoverResults) {
			content := hoverResults[i].Content
			if content == "" {
				sb.WriteString(fmt.Sprintf("\n%sNo hover information available\n", prefix))
			} else {
				sb.WriteString(fmt.Sprintf("\n%s%s\n", prefix, content))
			}
		}
	}

	return sb.String()
}
