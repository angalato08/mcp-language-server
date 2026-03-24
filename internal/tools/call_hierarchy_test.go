package tools

import (
	"strings"
	"testing"

	"github.com/angalato08/mcp-language-server/internal/protocol"
)

func TestFormatIncomingCallsShortBody(t *testing.T) {
	oldRoot := workspaceRoot
	workspaceRoot = "/workspace"
	defer func() { workspaceRoot = oldRoot }()

	t.Run("no callers", func(t *testing.T) {
		result := formatIncomingCallsShortBody(nil)
		if !strings.Contains(result, "0 callers:") {
			t.Errorf("Expected '0 callers:', got:\n%s", result)
		}
	})

	t.Run("single caller single site", func(t *testing.T) {
		calls := []protocol.CallHierarchyIncomingCall{
			{
				From: protocol.CallHierarchyItem{
					Name: "main",
					Kind: protocol.Function,
					URI:  "file:///workspace/cmd/main.go",
				},
				FromRanges: []protocol.Range{
					{Start: protocol.Position{Line: 41}},
				},
			},
		}
		result := formatIncomingCallsShortBody(calls)
		if !strings.Contains(result, "1 callers:") {
			t.Errorf("Expected '1 callers:', got:\n%s", result)
		}
		if !strings.Contains(result, "main (Function) — cmd/main.go:L42") {
			t.Errorf("Expected formatted caller line, got:\n%s", result)
		}
	})

	t.Run("multiple callers multiple sites", func(t *testing.T) {
		calls := []protocol.CallHierarchyIncomingCall{
			{
				From: protocol.CallHierarchyItem{
					Name: "processRequest",
					Kind: protocol.Function,
					URI:  "file:///workspace/src/handler.go",
				},
				FromRanges: []protocol.Range{
					{Start: protocol.Position{Line: 164}},
					{Start: protocol.Position{Line: 169}},
					{Start: protocol.Position{Line: 174}},
				},
			},
			{
				From: protocol.CallHierarchyItem{
					Name: "initializeSystem",
					Kind: protocol.Function,
					URI:  "file:///workspace/src/init.go",
				},
				FromRanges: []protocol.Range{
					{Start: protocol.Position{Line: 66}},
				},
			},
		}
		result := formatIncomingCallsShortBody(calls)
		if !strings.Contains(result, "2 callers:") {
			t.Errorf("Expected '2 callers:', got:\n%s", result)
		}
		if !strings.Contains(result, "processRequest (Function) — src/handler.go:L165, L170, L175") {
			t.Errorf("Expected processRequest line, got:\n%s", result)
		}
		if !strings.Contains(result, "initializeSystem (Function) — src/init.go:L67") {
			t.Errorf("Expected initializeSystem line, got:\n%s", result)
		}
	})
}

func TestFormatOutgoingCallsShortBody(t *testing.T) {
	oldRoot := workspaceRoot
	workspaceRoot = "/workspace"
	defer func() { workspaceRoot = oldRoot }()

	t.Run("no callees", func(t *testing.T) {
		result := formatOutgoingCallsShortBody(nil)
		if !strings.Contains(result, "0 callees:") {
			t.Errorf("Expected '0 callees:', got:\n%s", result)
		}
	})

	t.Run("single callee", func(t *testing.T) {
		calls := []protocol.CallHierarchyOutgoingCall{
			{
				To: protocol.CallHierarchyItem{
					Name: "helper",
					Kind: protocol.Function,
					URI:  "file:///workspace/src/util.go",
				},
				FromRanges: []protocol.Range{
					{Start: protocol.Position{Line: 9}},
				},
			},
		}
		result := formatOutgoingCallsShortBody(calls)
		if !strings.Contains(result, "1 callees:") {
			t.Errorf("Expected '1 callees:', got:\n%s", result)
		}
		if !strings.Contains(result, "helper (Function) — src/util.go:L10") {
			t.Errorf("Expected formatted callee line, got:\n%s", result)
		}
	})

	t.Run("multiple callees", func(t *testing.T) {
		calls := []protocol.CallHierarchyOutgoingCall{
			{
				To: protocol.CallHierarchyItem{
					Name: "validate",
					Kind: protocol.Method,
					URI:  "file:///workspace/src/validator.go",
				},
				FromRanges: []protocol.Range{
					{Start: protocol.Position{Line: 19}},
					{Start: protocol.Position{Line: 24}},
				},
			},
			{
				To: protocol.CallHierarchyItem{
					Name: "log",
					Kind: protocol.Function,
					URI:  "file:///workspace/src/logger.go",
				},
				FromRanges: []protocol.Range{
					{Start: protocol.Position{Line: 5}},
				},
			},
		}
		result := formatOutgoingCallsShortBody(calls)
		if !strings.Contains(result, "2 callees:") {
			t.Errorf("Expected '2 callees:', got:\n%s", result)
		}
		if !strings.Contains(result, "validate (Method) — src/validator.go:L20, L25") {
			t.Errorf("Expected validate line, got:\n%s", result)
		}
		if !strings.Contains(result, "log (Function) — src/logger.go:L6") {
			t.Errorf("Expected log line, got:\n%s", result)
		}
	})
}
