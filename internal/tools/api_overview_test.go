package tools

import (
	"testing"

	"github.com/angalato08/mcp-language-server/internal/protocol"
)

func makeDocSym(name string, kind protocol.SymbolKind, line, col uint32, children ...protocol.DocumentSymbol) protocol.DocumentSymbol {
	return protocol.DocumentSymbol{
		Name: name,
		Kind: kind,
		SelectionRange: protocol.Range{
			Start: protocol.Position{Line: line, Character: col},
		},
		Children: children,
	}
}

func TestCollectDocumentSymbolTargets_Flat(t *testing.T) {
	sym := makeDocSym("MyFunc", protocol.Function, 10, 0)

	var targets []symbolTarget
	collectDocumentSymbolTargets(&sym, nil, true, 0, &targets)

	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	if targets[0].Name != "MyFunc" {
		t.Errorf("name: got %q, want %q", targets[0].Name, "MyFunc")
	}
	if targets[0].Indent != 0 {
		t.Errorf("indent: got %d, want 0", targets[0].Indent)
	}
	if targets[0].Line != 11 { // 0-indexed to 1-indexed
		t.Errorf("line: got %d, want 11", targets[0].Line)
	}
}

func TestCollectDocumentSymbolTargets_Nested3Deep(t *testing.T) {
	method := makeDocSym("doWork", protocol.Method, 20, 4)
	class := makeDocSym("Worker", protocol.Class, 10, 2, method)
	ns := makeDocSym("myns", protocol.Namespace, 0, 0, class)

	var targets []symbolTarget
	collectDocumentSymbolTargets(&ns, nil, true, 0, &targets)

	if len(targets) != 3 {
		t.Fatalf("expected 3 targets, got %d", len(targets))
	}

	// Verify nesting order and indent levels
	expected := []struct {
		name   string
		indent int
	}{
		{"myns", 0},
		{"Worker", 1},
		{"doWork", 2},
	}
	for i, exp := range expected {
		if targets[i].Name != exp.name {
			t.Errorf("target[%d] name: got %q, want %q", i, targets[i].Name, exp.name)
		}
		if targets[i].Indent != exp.indent {
			t.Errorf("target[%d] indent: got %d, want %d", i, targets[i].Indent, exp.indent)
		}
	}
}

func TestCollectDocumentSymbolTargets_NoChildren(t *testing.T) {
	method := makeDocSym("doWork", protocol.Method, 20, 4)
	class := makeDocSym("Worker", protocol.Class, 10, 2, method)
	ns := makeDocSym("myns", protocol.Namespace, 0, 0, class)

	var targets []symbolTarget
	collectDocumentSymbolTargets(&ns, nil, false, 0, &targets)

	if len(targets) != 1 {
		t.Fatalf("expected 1 target (top-level only), got %d", len(targets))
	}
	if targets[0].Name != "myns" {
		t.Errorf("name: got %q, want %q", targets[0].Name, "myns")
	}
}

func TestCollectDocumentSymbolTargets_KindFilterTopLevelOnly(t *testing.T) {
	// Kind filter should only apply at indent 0
	method := makeDocSym("doWork", protocol.Method, 20, 4)
	class := makeDocSym("Worker", protocol.Class, 10, 2, method)

	// Filter to classes only — methods at top level would be excluded,
	// but method as child of class should still appear
	kindFilter := map[protocol.SymbolKind]bool{
		protocol.Class: true,
	}

	var targets []symbolTarget
	collectDocumentSymbolTargets(&class, kindFilter, true, 0, &targets)

	if len(targets) != 2 {
		t.Fatalf("expected 2 targets (class + child method), got %d", len(targets))
	}
	if targets[0].Name != "Worker" {
		t.Errorf("target[0] name: got %q, want %q", targets[0].Name, "Worker")
	}
	if targets[1].Name != "doWork" {
		t.Errorf("target[1] name: got %q, want %q", targets[1].Name, "doWork")
	}
}

func TestCollectDocumentSymbolTargets_KindFilterExcludesTopLevel(t *testing.T) {
	// A top-level method should be excluded if kind filter doesn't include Method
	method := makeDocSym("helper", protocol.Method, 5, 0)

	kindFilter := map[protocol.SymbolKind]bool{
		protocol.Class: true,
	}

	var targets []symbolTarget
	collectDocumentSymbolTargets(&method, kindFilter, true, 0, &targets)

	if len(targets) != 0 {
		t.Fatalf("expected 0 targets (method excluded by filter), got %d", len(targets))
	}
}

func TestCollectSymbolTargets_ViaInterface(t *testing.T) {
	method := makeDocSym("doWork", protocol.Method, 20, 4)
	class := makeDocSym("Worker", protocol.Class, 10, 2, method)

	symbols := []protocol.DocumentSymbolResult{&class}
	targets := collectSymbolTargets(symbols, nil, true)

	if len(targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(targets))
	}
}
