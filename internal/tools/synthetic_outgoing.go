package tools

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/angalato08/mcp-language-server/internal/lsp"
	"github.com/angalato08/mcp-language-server/internal/protocol"
)

// decodedToken represents a single semantic token with absolute positions.
type decodedToken struct {
	Line      uint32
	Char      uint32
	Length    uint32
	TokenType string
}

// resolvedToken holds a decoded token and its resolved definition locations.
type resolvedToken struct {
	token     decodedToken
	locations []protocol.Location
}

// syntheticOutgoingCalls synthesizes outgoing calls using semantic tokens + definition lookups.
// This is used as a fallback when the LSP server doesn't support callHierarchy/outgoingCalls (e.g. clangd).
func syntheticOutgoingCalls(ctx context.Context, client *lsp.Client, item protocol.CallHierarchyItem) ([]protocol.CallHierarchyOutgoingCall, error) {
	legend := client.SemanticTokenLegend()
	if legend == nil {
		return nil, fmt.Errorf("no semantic token legend available")
	}

	// Build set of callable token type indices
	callableTypes := make(map[int]bool)
	for i, t := range legend.TokenTypes {
		switch t {
		case "function", "method":
			callableTypes[i] = true
		}
	}

	// Request semantic tokens — try range first, fall back to full
	uri := item.URI
	var tokData []uint32
	tokResult, err := client.SemanticTokensRange(ctx, protocol.SemanticTokensRangeParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Range:        item.Range,
	})
	if err != nil {
		// Server may not support range; fall back to full document tokens
		fullResult, fullErr := client.SemanticTokensFull(ctx, protocol.SemanticTokensParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		})
		if fullErr != nil {
			return nil, fmt.Errorf("semantic tokens request failed: %w", fullErr)
		}
		tokData = fullResult.Data
	} else {
		tokData = tokResult.Data
	}

	// Decode from document start (line 0, char 0) since full tokens are document-relative
	allTokens := decodeSemanticTokens(tokData, legend.TokenTypes, protocol.Position{Line: 0, Character: 0})

	// Filter to only tokens within the item's range
	var tokens []decodedToken
	for _, tok := range allTokens {
		pos := protocol.Position{Line: tok.Line, Character: tok.Char}
		if rangeContains(item.Range, pos) {
			tokens = append(tokens, tok)
		}
	}

	// Filter for callable types and skip tokens inside the function's own name (SelectionRange)
	var callableTokens []decodedToken
	for _, tok := range tokens {
		if !callableTypes[tokenTypeIndex(tok.TokenType, legend.TokenTypes)] {
			continue
		}
		// Skip the function's own name token
		pos := protocol.Position{Line: tok.Line, Character: tok.Char}
		if rangeContains(item.SelectionRange, pos) {
			continue
		}
		callableTokens = append(callableTokens, tok)
	}

	if len(callableTokens) == 0 {
		return nil, nil
	}

	// Resolve definitions in parallel with concurrency limit
	results := make([]resolvedToken, len(callableTokens))

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(5)

	for i, tok := range callableTokens {
		i, tok := i, tok
		g.Go(func() error {
			defResp, err := client.Definition(gctx, protocol.DefinitionParams{
				TextDocumentPositionParams: protocol.TextDocumentPositionParams{
					TextDocument: protocol.TextDocumentIdentifier{URI: uri},
					Position:     protocol.Position{Line: tok.Line, Character: tok.Char},
				},
			})
			if err != nil {
				// Skip failures silently
				return nil
			}
			locs, err := defResp.Locations()
			if err != nil {
				return nil
			}
			results[i] = resolvedToken{token: tok, locations: locs}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return deduplicateOutgoingCalls(ctx, client, results, uri)
}

// deduplicateOutgoingCalls groups resolved definitions by target location,
// calls prepareCallHierarchy to get proper CallHierarchyItems, and merges FromRanges.
func deduplicateOutgoingCalls(ctx context.Context, client *lsp.Client, results []resolvedToken, sourceURI protocol.DocumentUri) ([]protocol.CallHierarchyOutgoingCall, error) {
	// Group by target location key
	type targetInfo struct {
		loc        protocol.Location
		fromRanges []protocol.Range
	}
	targets := make(map[string]*targetInfo)

	for _, r := range results {
		if len(r.locations) == 0 {
			continue
		}
		loc := r.locations[0]
		key := fmt.Sprintf("%s|%d:%d", loc.URI, loc.Range.Start.Line, loc.Range.Start.Character)
		fromRange := protocol.Range{
			Start: protocol.Position{Line: r.token.Line, Character: r.token.Char},
			End:   protocol.Position{Line: r.token.Line, Character: r.token.Char + r.token.Length},
		}
		if t, ok := targets[key]; ok {
			t.fromRanges = append(t.fromRanges, fromRange)
		} else {
			targets[key] = &targetInfo{loc: loc, fromRanges: []protocol.Range{fromRange}}
		}
	}

	var outgoing []protocol.CallHierarchyOutgoingCall
	for _, t := range targets {
		// Try prepareCallHierarchy to get a proper item (needed for recursive walkOutgoing)
		filePath := strings.TrimPrefix(string(t.loc.URI), "file://")
		_ = client.OpenFile(ctx, filePath)

		items, err := client.PrepareCallHierarchy(ctx, protocol.CallHierarchyPrepareParams{
			TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: t.loc.URI},
				Position:     t.loc.Range.Start,
			},
		})

		var item protocol.CallHierarchyItem
		if err == nil && len(items) > 0 {
			item = items[0]
		} else {
			// Fallback to minimal item
			item = protocol.CallHierarchyItem{
				Name:           fmt.Sprintf("symbol@%d:%d", t.loc.Range.Start.Line+1, t.loc.Range.Start.Character+1),
				Kind:           protocol.Function,
				URI:            t.loc.URI,
				Range:          t.loc.Range,
				SelectionRange: t.loc.Range,
			}
		}

		outgoing = append(outgoing, protocol.CallHierarchyOutgoingCall{
			To:         item,
			FromRanges: t.fromRanges,
		})
	}

	return outgoing, nil
}

// decodeSemanticTokens decodes the relative-encoded semantic token data into absolute positions.
// basePos is the start position of the range that tokens are relative to.
func decodeSemanticTokens(data []uint32, tokenTypes []string, basePos protocol.Position) []decodedToken {
	if len(data) < 5 {
		return nil
	}

	var tokens []decodedToken
	var prevLine, prevChar uint32
	prevLine = basePos.Line
	prevChar = basePos.Character

	for i := 0; i+4 < len(data); i += 5 {
		deltaLine := data[i]
		deltaChar := data[i+1]
		length := data[i+2]
		typeIdx := data[i+3]
		// data[i+4] is modifiers, ignored

		if deltaLine > 0 {
			prevLine += deltaLine
			prevChar = deltaChar // absolute when line changes
		} else {
			prevChar += deltaChar // relative on same line
		}

		typeName := ""
		if int(typeIdx) < len(tokenTypes) {
			typeName = tokenTypes[typeIdx]
		}

		tokens = append(tokens, decodedToken{
			Line:      prevLine,
			Char:      prevChar,
			Length:    length,
			TokenType: typeName,
		})
	}

	return tokens
}

// tokenTypeIndex returns the index of a token type in the legend, or -1 if not found.
func tokenTypeIndex(typeName string, tokenTypes []string) int {
	for i, t := range tokenTypes {
		if t == typeName {
			return i
		}
	}
	return -1
}

// rangeContains returns true if pos is within the given range (inclusive start, exclusive end).
func rangeContains(r protocol.Range, pos protocol.Position) bool {
	// Before range start
	if pos.Line < r.Start.Line || (pos.Line == r.Start.Line && pos.Character < r.Start.Character) {
		return false
	}
	// At or after range end
	if pos.Line > r.End.Line || (pos.Line == r.End.Line && pos.Character >= r.End.Character) {
		return false
	}
	return true
}
