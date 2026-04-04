package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/angalato08/mcp-language-server/internal/lsp"
	"github.com/angalato08/mcp-language-server/internal/protocol"
)

func TestFilterDiagnostics(t *testing.T) {
	makeDiag := func(severity protocol.DiagnosticSeverity, code interface{}, msg string) protocol.Diagnostic {
		return protocol.Diagnostic{
			Severity: severity,
			Code:     code,
			Message:  msg,
		}
	}

	tests := []struct {
		name        string
		diagnostics []protocol.Diagnostic
		filter      DiagnosticFilter
		wantCount   int
	}{
		{
			name: "no filter passes all",
			diagnostics: []protocol.Diagnostic{
				makeDiag(protocol.SeverityError, "E001", "error"),
				makeDiag(protocol.SeverityWarning, "W001", "warning"),
				makeDiag(protocol.SeverityHint, "H001", "hint"),
			},
			filter:    DiagnosticFilter{},
			wantCount: 3,
		},
		{
			name: "exclude single code",
			diagnostics: []protocol.Diagnostic{
				makeDiag(protocol.SeverityWarning, "owning-memory", "bad ownership"),
				makeDiag(protocol.SeverityError, "E001", "real error"),
			},
			filter:    DiagnosticFilter{Exclude: []string{"owning-memory"}},
			wantCount: 1,
		},
		{
			name: "exclude substring match",
			diagnostics: []protocol.Diagnostic{
				makeDiag(protocol.SeverityWarning, "cppcoreguidelines-owning-memory", "bad ownership"),
				makeDiag(protocol.SeverityError, "E001", "real error"),
			},
			filter:    DiagnosticFilter{Exclude: []string{"owning-memory"}},
			wantCount: 1,
		},
		{
			name: "exclude with nil code not excluded",
			diagnostics: []protocol.Diagnostic{
				makeDiag(protocol.SeverityWarning, nil, "no code warning"),
				makeDiag(protocol.SeverityError, "E001", "real error"),
			},
			filter:    DiagnosticFilter{Exclude: []string{"owning-memory"}},
			wantCount: 2,
		},
		{
			name: "severity error only",
			diagnostics: []protocol.Diagnostic{
				makeDiag(protocol.SeverityError, "E001", "error"),
				makeDiag(protocol.SeverityWarning, "W001", "warning"),
				makeDiag(protocol.SeverityInformation, "I001", "info"),
				makeDiag(protocol.SeverityHint, "H001", "hint"),
			},
			filter:    DiagnosticFilter{MinSeverity: protocol.SeverityError},
			wantCount: 1,
		},
		{
			name: "severity warning includes error and warning",
			diagnostics: []protocol.Diagnostic{
				makeDiag(protocol.SeverityError, "E001", "error"),
				makeDiag(protocol.SeverityWarning, "W001", "warning"),
				makeDiag(protocol.SeverityInformation, "I001", "info"),
				makeDiag(protocol.SeverityHint, "H001", "hint"),
			},
			filter:    DiagnosticFilter{MinSeverity: protocol.SeverityWarning},
			wantCount: 2,
		},
		{
			name: "combined exclude and severity",
			diagnostics: []protocol.Diagnostic{
				makeDiag(protocol.SeverityError, "E001", "real error"),
				makeDiag(protocol.SeverityError, "unused-includes", "unused include"),
				makeDiag(protocol.SeverityWarning, "W001", "warning"),
				makeDiag(protocol.SeverityHint, "H001", "hint"),
			},
			filter:    DiagnosticFilter{Exclude: []string{"unused-includes"}, MinSeverity: protocol.SeverityError},
			wantCount: 1,
		},
		{
			name:        "empty input no panic",
			diagnostics: []protocol.Diagnostic{},
			filter:      DiagnosticFilter{Exclude: []string{"foo"}, MinSeverity: protocol.SeverityError},
			wantCount:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilterDiagnostics(tt.diagnostics, tt.filter)
			if len(got) != tt.wantCount {
				t.Errorf("FilterDiagnostics() returned %d diagnostics, want %d", len(got), tt.wantCount)
			}
		})
	}
}

// makeDiffDiag creates a diagnostic with position, message, source, and code for diff testing.
func makeDiffDiag(line, char uint32, msg, source string, code interface{}) protocol.Diagnostic {
	return protocol.Diagnostic{
		Range: protocol.Range{
			Start: protocol.Position{Line: line, Character: char},
			End:   protocol.Position{Line: line, Character: char + 1},
		},
		Severity: protocol.SeverityError,
		Message:  msg,
		Source:   source,
		Code:     code,
	}
}

func TestComputeDiagnosticDiff_AllNew(t *testing.T) {
	current := []protocol.Diagnostic{
		makeDiffDiag(1, 0, "error one", "gopls", "E001"),
		makeDiffDiag(5, 3, "error two", "gopls", "E002"),
		makeDiffDiag(10, 0, "error three", "gopls", "E003"),
	}

	newDiags, resolved := lsp.ComputeDiagnosticDiff(nil, current)
	if len(newDiags) != 3 {
		t.Errorf("expected 3 new, got %d", len(newDiags))
	}
	if len(resolved) != 0 {
		t.Errorf("expected 0 resolved, got %d", len(resolved))
	}
}

func TestComputeDiagnosticDiff_AllResolved(t *testing.T) {
	previous := []protocol.Diagnostic{
		makeDiffDiag(1, 0, "error one", "gopls", "E001"),
		makeDiffDiag(5, 3, "error two", "gopls", "E002"),
		makeDiffDiag(10, 0, "error three", "gopls", "E003"),
	}

	newDiags, resolved := lsp.ComputeDiagnosticDiff(previous, nil)
	if len(newDiags) != 0 {
		t.Errorf("expected 0 new, got %d", len(newDiags))
	}
	if len(resolved) != 3 {
		t.Errorf("expected 3 resolved, got %d", len(resolved))
	}
}

func TestComputeDiagnosticDiff_Mixed(t *testing.T) {
	diagA := makeDiffDiag(1, 0, "error A", "gopls", "E001")
	diagB := makeDiffDiag(5, 3, "error B", "gopls", "E002")
	diagC := makeDiffDiag(10, 0, "error C", "gopls", "E003")
	diagD := makeDiffDiag(15, 2, "error D", "gopls", "E004")

	previous := []protocol.Diagnostic{diagA, diagB, diagC}
	current := []protocol.Diagnostic{diagB, diagC, diagD}

	newDiags, resolved := lsp.ComputeDiagnosticDiff(previous, current)
	if len(newDiags) != 1 {
		t.Errorf("expected 1 new, got %d", len(newDiags))
	} else if newDiags[0].Message != "error D" {
		t.Errorf("expected new diag 'error D', got '%s'", newDiags[0].Message)
	}
	if len(resolved) != 1 {
		t.Errorf("expected 1 resolved, got %d", len(resolved))
	} else if resolved[0].Message != "error A" {
		t.Errorf("expected resolved diag 'error A', got '%s'", resolved[0].Message)
	}
}

func TestComputeDiagnosticDiff_Identical(t *testing.T) {
	diags := []protocol.Diagnostic{
		makeDiffDiag(1, 0, "error one", "gopls", "E001"),
		makeDiffDiag(5, 3, "error two", "gopls", "E002"),
	}

	newDiags, resolved := lsp.ComputeDiagnosticDiff(diags, diags)
	if len(newDiags) != 0 {
		t.Errorf("expected 0 new, got %d", len(newDiags))
	}
	if len(resolved) != 0 {
		t.Errorf("expected 0 resolved, got %d", len(resolved))
	}
}

func TestComputeDiagnosticDiff_SameMessageDifferentLine(t *testing.T) {
	diagLine1 := makeDiffDiag(1, 0, "undeclared variable", "gopls", "UndeclaredName")
	diagLine5 := makeDiffDiag(5, 0, "undeclared variable", "gopls", "UndeclaredName")

	previous := []protocol.Diagnostic{diagLine1}
	current := []protocol.Diagnostic{diagLine5}

	newDiags, resolved := lsp.ComputeDiagnosticDiff(previous, current)
	if len(newDiags) != 1 {
		t.Errorf("expected 1 new, got %d", len(newDiags))
	}
	if len(resolved) != 1 {
		t.Errorf("expected 1 resolved, got %d", len(resolved))
	}
}

func TestComputeDiagnosticDiff_WithFilter(t *testing.T) {
	diagA := makeDiffDiag(1, 0, "unused import", "gopls", "UnusedImport")
	diagB := makeDiffDiag(5, 0, "undeclared name", "gopls", "UndeclaredName")

	// Both are new
	newDiags, _ := lsp.ComputeDiagnosticDiff(nil, []protocol.Diagnostic{diagA, diagB})

	// Apply filter to exclude UnusedImport
	filtered := FilterDiagnostics(newDiags, DiagnosticFilter{Exclude: []string{"UnusedImport"}})
	if len(filtered) != 1 {
		t.Errorf("expected 1 after filter, got %d", len(filtered))
	} else if filtered[0].Message != "undeclared name" {
		t.Errorf("expected 'undeclared name', got '%s'", filtered[0].Message)
	}
}

func TestCollectFilesFromDirectory(t *testing.T) {
	// Create a temp directory tree with mixed file types
	tmp := t.TempDir()
	SetWorkspaceRoot(tmp)
	defer SetWorkspaceRoot("")

	// Create source files with supported extensions
	mkFile := func(rel string) {
		abs := filepath.Join(tmp, rel)
		_ = os.MkdirAll(filepath.Dir(abs), 0o755)
		_ = os.WriteFile(abs, []byte("// content"), 0o644)
	}

	mkFile("main.go")
	mkFile("lib/helper.py")
	mkFile("lib/util.rs")
	mkFile("lib/deep/nested.ts")

	// Create files that should be excluded
	mkFile("build/out.go")      // excluded dir
	mkFile("node_modules/x.js") // excluded dir
	mkFile(".hidden/secret.go") // dot-dir
	mkFile("data/file.log")     // excluded extension
	mkFile("images/pic.png")    // large binary extension
	mkFile("readme.txt")        // no LSP support

	files, err := CollectFilesFromDirectory(tmp)
	if err != nil {
		t.Fatalf("CollectFilesFromDirectory() error: %v", err)
	}

	// Should find exactly the 4 supported source files
	if len(files) != 4 {
		t.Errorf("expected 4 files, got %d: %v", len(files), files)
	}

	// Verify all returned paths are absolute
	for _, f := range files {
		if !filepath.IsAbs(f) {
			t.Errorf("expected absolute path, got %s", f)
		}
	}

	// Verify sorted order
	for i := 1; i < len(files); i++ {
		if files[i] < files[i-1] {
			t.Errorf("files not sorted: %s < %s", files[i], files[i-1])
		}
	}
}

func TestCollectFilesFromDirectory_NotADir(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "file.go")
	_ = os.WriteFile(f, []byte("package main"), 0o644)

	_, err := CollectFilesFromDirectory(f)
	if err == nil {
		t.Fatal("expected error for non-directory path")
	}
}

func TestCollectFilesFromDirectory_Empty(t *testing.T) {
	tmp := t.TempDir()
	files, err := CollectFilesFromDirectory(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected 0 files, got %d", len(files))
	}
}

func TestConvertDiagnostic(t *testing.T) {
	href := protocol.URI("https://example.com/E001")
	diag := protocol.Diagnostic{
		Range: protocol.Range{
			Start: protocol.Position{Line: 9, Character: 4},
			End:   protocol.Position{Line: 9, Character: 20},
		},
		Severity: protocol.SeverityWarning,
		Message:  "unused variable",
		Source:   "gopls",
		Code:     "UnusedVar",
		CodeDescription: &protocol.CodeDescription{
			Href: href,
		},
		Tags: []protocol.DiagnosticTag{protocol.Unnecessary},
	}

	fixableSet := map[string]bool{
		"9:4:unused variable": true,
	}

	jd := convertDiagnostic(diag, "/home/user/project/main.go", fixableSet)

	if jd.Line != 10 {
		t.Errorf("Line: got %d, want 10 (1-indexed)", jd.Line)
	}
	if jd.Column != 5 {
		t.Errorf("Column: got %d, want 5 (1-indexed)", jd.Column)
	}
	if jd.EndLine != 10 {
		t.Errorf("EndLine: got %d, want 10", jd.EndLine)
	}
	if jd.EndColumn != 21 {
		t.Errorf("EndColumn: got %d, want 21", jd.EndColumn)
	}
	if jd.Severity != "warning" {
		t.Errorf("Severity: got %q, want %q", jd.Severity, "warning")
	}
	if jd.Message != "unused variable" {
		t.Errorf("Message: got %q, want %q", jd.Message, "unused variable")
	}
	if jd.Source != "gopls" {
		t.Errorf("Source: got %q, want %q", jd.Source, "gopls")
	}
	if jd.Code != "UnusedVar" {
		t.Errorf("Code: got %q, want %q", jd.Code, "UnusedVar")
	}
	if jd.CodeHref != "https://example.com/E001" {
		t.Errorf("CodeHref: got %q, want %q", jd.CodeHref, "https://example.com/E001")
	}
	if len(jd.Tags) != 1 || jd.Tags[0] != "unnecessary" {
		t.Errorf("Tags: got %v, want [unnecessary]", jd.Tags)
	}
	if !jd.Fixable {
		t.Error("Fixable: got false, want true")
	}
}

func TestConvertDiagnostic_NilCode(t *testing.T) {
	diag := protocol.Diagnostic{
		Range: protocol.Range{
			Start: protocol.Position{Line: 0, Character: 0},
			End:   protocol.Position{Line: 0, Character: 5},
		},
		Severity: protocol.SeverityError,
		Message:  "syntax error",
	}

	jd := convertDiagnostic(diag, "/tmp/test.go", nil)

	if jd.Code != "" {
		t.Errorf("Code should be empty for nil code, got %q", jd.Code)
	}
	if jd.Severity != "error" {
		t.Errorf("Severity: got %q, want %q", jd.Severity, "error")
	}
	if jd.Fixable {
		t.Error("Fixable should be false when fixableSet is nil")
	}
}

func TestJSONDiagnosticsResult_EmptyFiles(t *testing.T) {
	result := JSONDiagnosticsResult{
		Files: []JSONFileDiagnostics{},
		Total: 0,
	}

	b, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	got := string(b)
	expected := `{"files":[],"total":0}`
	if got != expected {
		t.Errorf("got %s, want %s", got, expected)
	}

	// Verify it round-trips
	var parsed JSONDiagnosticsResult
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if len(parsed.Files) != 0 {
		t.Errorf("parsed.Files should be empty, got %d", len(parsed.Files))
	}
	if parsed.Total != 0 {
		t.Errorf("parsed.Total should be 0, got %d", parsed.Total)
	}
}

func TestGetFixableDiagnosticKey(t *testing.T) {
	diag := protocol.Diagnostic{
		Range: protocol.Range{
			Start: protocol.Position{Line: 5, Character: 10},
		},
		Message: "unused import",
	}

	key := diagnosticKey(diag)
	expected := "5:10:unused import"
	if key != expected {
		t.Errorf("diagnosticKey: got %q, want %q", key, expected)
	}

	// Same position and message produces same key
	diag2 := protocol.Diagnostic{
		Range: protocol.Range{
			Start: protocol.Position{Line: 5, Character: 10},
		},
		Message: "unused import",
	}
	if diagnosticKey(diag2) != key {
		t.Error("identical diagnostics should produce the same key")
	}

	// Different position produces different key
	diag3 := protocol.Diagnostic{
		Range: protocol.Range{
			Start: protocol.Position{Line: 6, Character: 10},
		},
		Message: "unused import",
	}
	if diagnosticKey(diag3) == key {
		t.Error("different position should produce different key")
	}
}

func TestConvertDiagnostic_NumericCode(t *testing.T) {
	diag := protocol.Diagnostic{
		Range: protocol.Range{
			Start: protocol.Position{Line: 0, Character: 0},
			End:   protocol.Position{Line: 0, Character: 1},
		},
		Severity: protocol.SeverityWarning,
		Message:  "some warning",
		Code:     6133,
	}

	jd := convertDiagnostic(diag, "/tmp/test.ts", nil)
	if jd.Code != "6133" {
		t.Errorf("Code: got %q, want %q", jd.Code, "6133")
	}
}

func TestConvertDiagnostic_DeprecatedTag(t *testing.T) {
	diag := protocol.Diagnostic{
		Range: protocol.Range{
			Start: protocol.Position{Line: 0, Character: 0},
			End:   protocol.Position{Line: 0, Character: 1},
		},
		Severity: protocol.SeverityHint,
		Message:  "deprecated API",
		Tags:     []protocol.DiagnosticTag{protocol.Deprecated},
	}

	jd := convertDiagnostic(diag, "/tmp/test.go", nil)
	if jd.Severity != "hint" {
		t.Errorf("Severity: got %q, want %q", jd.Severity, "hint")
	}
	if len(jd.Tags) != 1 || jd.Tags[0] != "deprecated" {
		t.Errorf("Tags: got %v, want [deprecated]", jd.Tags)
	}
}

func TestJSONDiagnosticsResult_IndexingTrue(t *testing.T) {
	result := JSONDiagnosticsResult{
		Files:    []JSONFileDiagnostics{},
		Total:    0,
		Indexing: true,
	}

	b, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	got := string(b)
	if !strings.Contains(got, `"indexing":true`) {
		t.Errorf("expected indexing:true in output, got %s", got)
	}
}

func TestJSONDiagnosticsResult_IndexingFalseOmitted(t *testing.T) {
	result := JSONDiagnosticsResult{
		Files: []JSONFileDiagnostics{},
		Total: 0,
	}

	b, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	got := string(b)
	if strings.Contains(got, "indexing") {
		t.Errorf("expected no indexing field when false, got %s", got)
	}
}

func TestJSONDiagnosticDiffResult_IndexingTrue(t *testing.T) {
	result := JSONDiagnosticDiffResult{
		Files:    []JSONFileDiagnosticDiff{},
		Indexing: true,
	}

	b, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	got := string(b)
	if !strings.Contains(got, `"indexing":true`) {
		t.Errorf("expected indexing:true in output, got %s", got)
	}
}

func TestJSONDiagnosticDiffResult_IndexingFalseOmitted(t *testing.T) {
	result := JSONDiagnosticDiffResult{
		Files: []JSONFileDiagnosticDiff{},
	}

	b, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	got := string(b)
	if strings.Contains(got, "indexing") {
		t.Errorf("expected no indexing field when false, got %s", got)
	}
}

func TestGetSeverityStringLower(t *testing.T) {
	tests := []struct {
		sev  protocol.DiagnosticSeverity
		want string
	}{
		{protocol.SeverityError, "error"},
		{protocol.SeverityWarning, "warning"},
		{protocol.SeverityInformation, "info"},
		{protocol.SeverityHint, "hint"},
		{0, "unknown"},
	}
	for _, tt := range tests {
		got := getSeverityStringLower(tt.sev)
		if got != tt.want {
			t.Errorf("getSeverityStringLower(%d) = %q, want %q", tt.sev, got, tt.want)
		}
	}
}
