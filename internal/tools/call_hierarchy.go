package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/angalato08/mcp-language-server/internal/lsp"
	"github.com/angalato08/mcp-language-server/internal/protocol"
)

// GetIncomingCalls finds all callers of the symbol at the given position.
// Returns a formatted list of functions/methods that call the target.
func GetIncomingCalls(ctx context.Context, client *lsp.Client, filePath string, line, column int) (string, error) {
	items, err := prepareCallHierarchy(ctx, client, filePath, line, column)
	if err != nil {
		return "", err
	}

	if len(items) == 0 {
		return "No call hierarchy item found at this position", nil
	}

	var result strings.Builder
	for _, item := range items {
		result.WriteString(fmt.Sprintf("Incoming calls to: %s (%s)\n\n",
			item.Name, protocol.TableKindMap[item.Kind]))

		incoming, err := client.IncomingCalls(ctx, protocol.CallHierarchyIncomingCallsParams{
			Item: item,
		})
		if err != nil {
			if strings.Contains(err.Error(), "code: -32601") {
				return "", fmt.Errorf("incoming_calls is not supported by this language server")
			}
			return "", fmt.Errorf("failed to get incoming calls: %v", err)
		}

		if len(incoming) == 0 {
			result.WriteString("No callers found.\n")
			continue
		}

		for i, call := range incoming {
			result.WriteString(formatCallHierarchyItem(i+1, call.From, call.FromRanges))
		}
	}

	return result.String(), nil
}

// GetOutgoingCalls finds all functions/methods called by the symbol at the given position.
// Returns a formatted list of functions/methods that the target calls.
func GetOutgoingCalls(ctx context.Context, client *lsp.Client, filePath string, line, column int) (string, error) {
	items, err := prepareCallHierarchy(ctx, client, filePath, line, column)
	if err != nil {
		return "", err
	}

	if len(items) == 0 {
		return "No call hierarchy item found at this position", nil
	}

	var result strings.Builder
	for _, item := range items {
		result.WriteString(fmt.Sprintf("Outgoing calls from: %s (%s)\n\n",
			item.Name, protocol.TableKindMap[item.Kind]))

		outgoing, err := client.OutgoingCalls(ctx, protocol.CallHierarchyOutgoingCallsParams{
			Item: item,
		})
		if err != nil {
			if strings.Contains(err.Error(), "code: -32601") {
				return "", fmt.Errorf("outgoing_calls is not supported by this language server (e.g. clangd does not implement callHierarchy/outgoingCalls)")
			}
			return "", fmt.Errorf("failed to get outgoing calls: %v", err)
		}

		if len(outgoing) == 0 {
			result.WriteString("No outgoing calls found.\n")
			continue
		}

		for i, call := range outgoing {
			result.WriteString(formatCallHierarchyItem(i+1, call.To, call.FromRanges))
		}
	}

	return result.String(), nil
}

func prepareCallHierarchy(ctx context.Context, client *lsp.Client, filePath string, line, column int) ([]protocol.CallHierarchyItem, error) {
	err := client.OpenFile(ctx, filePath)
	if err != nil {
		return nil, fmt.Errorf("could not open file: %v", err)
	}

	params := protocol.CallHierarchyPrepareParams{
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

	items, err := client.PrepareCallHierarchy(ctx, params)
	if err != nil {
		if strings.Contains(err.Error(), "code: -32601") {
			return nil, fmt.Errorf("call hierarchy is not supported by this language server")
		}
		return nil, fmt.Errorf("call hierarchy failed: %v", err)
	}

	return items, nil
}

func formatCallHierarchyItem(index int, item protocol.CallHierarchyItem, callSites []protocol.Range) string {
	var b strings.Builder

	kind := protocol.TableKindMap[item.Kind]
	if kind == "" {
		kind = "Unknown"
	}

	b.WriteString(fmt.Sprintf("%d. %s (%s)\n", index, item.Name, kind))
	if item.Detail != "" {
		b.WriteString(fmt.Sprintf("   Detail: %s\n", item.Detail))
	}
	b.WriteString(fmt.Sprintf("   File: %s\n",
		RelativePath(strings.TrimPrefix(string(item.URI), "file://"))))
	b.WriteString(fmt.Sprintf("   Range: L%d:C%d - L%d:C%d\n",
		item.Range.Start.Line+1,
		item.Range.Start.Character+1,
		item.Range.End.Line+1,
		item.Range.End.Character+1))

	if len(callSites) > 0 {
		b.WriteString(fmt.Sprintf("   Call sites: %d", len(callSites)))
		for _, r := range callSites {
			b.WriteString(fmt.Sprintf(" [L%d:C%d]", r.Start.Line+1, r.Start.Character+1))
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	return b.String()
}
