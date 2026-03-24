package tools

import (
	"testing"

	"github.com/angalato08/mcp-language-server/internal/protocol"
)

func TestDecodeSemanticTokens(t *testing.T) {
	t.Run("empty data", func(t *testing.T) {
		tokens := decodeSemanticTokens(nil, []string{"function"}, protocol.Position{})
		if len(tokens) != 0 {
			t.Errorf("expected 0 tokens, got %d", len(tokens))
		}
	})

	t.Run("fewer than 5 elements", func(t *testing.T) {
		tokens := decodeSemanticTokens([]uint32{1, 2, 3}, []string{"function"}, protocol.Position{})
		if len(tokens) != 0 {
			t.Errorf("expected 0 tokens, got %d", len(tokens))
		}
	})

	t.Run("single token", func(t *testing.T) {
		// deltaLine=2, deltaChar=5, length=3, typeIdx=0, modifiers=0
		data := []uint32{2, 5, 3, 0, 0}
		types := []string{"function", "variable"}
		tokens := decodeSemanticTokens(data, types, protocol.Position{Line: 10, Character: 0})

		if len(tokens) != 1 {
			t.Fatalf("expected 1 token, got %d", len(tokens))
		}
		tok := tokens[0]
		if tok.Line != 12 {
			t.Errorf("expected line 12, got %d", tok.Line)
		}
		if tok.Char != 5 {
			t.Errorf("expected char 5, got %d", tok.Char)
		}
		if tok.Length != 3 {
			t.Errorf("expected length 3, got %d", tok.Length)
		}
		if tok.TokenType != "function" {
			t.Errorf("expected type 'function', got %q", tok.TokenType)
		}
	})

	t.Run("multi-line tokens", func(t *testing.T) {
		// Token 1: deltaLine=1, deltaChar=4, length=5, type=0(function)
		// Token 2: deltaLine=2, deltaChar=8, length=3, type=1(variable)
		data := []uint32{
			1, 4, 5, 0, 0,
			2, 8, 3, 1, 0,
		}
		types := []string{"function", "variable"}
		tokens := decodeSemanticTokens(data, types, protocol.Position{Line: 0, Character: 0})

		if len(tokens) != 2 {
			t.Fatalf("expected 2 tokens, got %d", len(tokens))
		}
		// Token 1: line 0+1=1, char=4 (absolute since line changed)
		if tokens[0].Line != 1 || tokens[0].Char != 4 {
			t.Errorf("token 0: expected (1,4), got (%d,%d)", tokens[0].Line, tokens[0].Char)
		}
		// Token 2: line 1+2=3, char=8 (absolute since line changed)
		if tokens[1].Line != 3 || tokens[1].Char != 8 {
			t.Errorf("token 1: expected (3,8), got (%d,%d)", tokens[1].Line, tokens[1].Char)
		}
	})

	t.Run("same-line deltas", func(t *testing.T) {
		// Token 1: deltaLine=1, deltaChar=2, length=4, type=0
		// Token 2: deltaLine=0, deltaChar=6, length=3, type=0 (same line, relative char)
		data := []uint32{
			1, 2, 4, 0, 0,
			0, 6, 3, 0, 0,
		}
		types := []string{"function"}
		tokens := decodeSemanticTokens(data, types, protocol.Position{Line: 5, Character: 0})

		if len(tokens) != 2 {
			t.Fatalf("expected 2 tokens, got %d", len(tokens))
		}
		// Token 1: line 5+1=6, char=2
		if tokens[0].Line != 6 || tokens[0].Char != 2 {
			t.Errorf("token 0: expected (6,2), got (%d,%d)", tokens[0].Line, tokens[0].Char)
		}
		// Token 2: same line 6, char=2+6=8
		if tokens[1].Line != 6 || tokens[1].Char != 8 {
			t.Errorf("token 1: expected (6,8), got (%d,%d)", tokens[1].Line, tokens[1].Char)
		}
	})

	t.Run("out-of-range type index", func(t *testing.T) {
		data := []uint32{0, 5, 3, 99, 0}
		types := []string{"function"}
		tokens := decodeSemanticTokens(data, types, protocol.Position{})

		if len(tokens) != 1 {
			t.Fatalf("expected 1 token, got %d", len(tokens))
		}
		if tokens[0].TokenType != "" {
			t.Errorf("expected empty token type, got %q", tokens[0].TokenType)
		}
	})
}

func TestRangeContains(t *testing.T) {
	r := protocol.Range{
		Start: protocol.Position{Line: 5, Character: 10},
		End:   protocol.Position{Line: 5, Character: 20},
	}

	tests := []struct {
		name     string
		pos      protocol.Position
		expected bool
	}{
		{"inside", protocol.Position{Line: 5, Character: 15}, true},
		{"at start", protocol.Position{Line: 5, Character: 10}, true},
		{"at end (exclusive)", protocol.Position{Line: 5, Character: 20}, false},
		{"before start char", protocol.Position{Line: 5, Character: 9}, false},
		{"before start line", protocol.Position{Line: 4, Character: 15}, false},
		{"after end line", protocol.Position{Line: 6, Character: 0}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := rangeContains(r, tc.pos)
			if got != tc.expected {
				t.Errorf("rangeContains(%v) = %v, want %v", tc.pos, got, tc.expected)
			}
		})
	}

	// Multi-line range
	t.Run("multi-line range", func(t *testing.T) {
		mr := protocol.Range{
			Start: protocol.Position{Line: 10, Character: 5},
			End:   protocol.Position{Line: 15, Character: 3},
		}
		if !rangeContains(mr, protocol.Position{Line: 12, Character: 0}) {
			t.Error("expected middle line to be inside")
		}
		if !rangeContains(mr, protocol.Position{Line: 10, Character: 5}) {
			t.Error("expected start position to be inside")
		}
		if rangeContains(mr, protocol.Position{Line: 15, Character: 3}) {
			t.Error("expected end position to be outside (exclusive)")
		}
		if rangeContains(mr, protocol.Position{Line: 10, Character: 4}) {
			t.Error("expected before start char to be outside")
		}
	})
}
