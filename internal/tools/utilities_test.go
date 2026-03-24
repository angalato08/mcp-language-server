package tools

import (
	"strings"
	"testing"

	"github.com/angalato08/mcp-language-server/internal/protocol"
	"github.com/stretchr/testify/assert"
)

func TestExtractTextFromLocation_SingleLine(t *testing.T) {
	t.Parallel()
	content := []byte("function testFunction() {\n  return 'test';\n}")

	location := protocol.Location{
		URI: "file:///path/to/file.js",
		Range: protocol.Range{
			Start: protocol.Position{Line: 0, Character: 9},
			End:   protocol.Position{Line: 0, Character: 21},
		},
	}

	result, err := extractTextFromContent(content, location)

	assert.NoError(t, err)
	assert.Equal(t, "testFunction", result)
}

func TestExtractTextFromLocation_MultiLine(t *testing.T) {
	t.Parallel()
	content := []byte("function testFunction() {\n  return 'test';\n}")

	location := protocol.Location{
		URI: "file:///path/to/file.js",
		Range: protocol.Range{
			Start: protocol.Position{Line: 0, Character: 9},
			End:   protocol.Position{Line: 1, Character: 15},
		},
	}

	result, err := extractTextFromContent(content, location)

	assert.NoError(t, err)
	assert.Equal(t, "testFunction() {\n  return 'test'", result)
}

func TestExtractTextFromLocation_InvalidRange(t *testing.T) {
	t.Parallel()
	content := []byte("function testFunction() {\n  return 'test';\n}")

	// Out of bounds line
	location := protocol.Location{
		URI: "file:///path/to/file.js",
		Range: protocol.Range{
			Start: protocol.Position{Line: 0, Character: 9},
			End:   protocol.Position{Line: 5, Character: 15},
		},
	}

	_, err := extractTextFromContent(content, location)
	assert.Error(t, err)

	// Out of bounds character on single line
	location = protocol.Location{
		URI: "file:///path/to/file.js",
		Range: protocol.Range{
			Start: protocol.Position{Line: 0, Character: 9},
			End:   protocol.Position{Line: 0, Character: 100},
		},
	}

	_, err = extractTextFromContent(content, location)
	assert.Error(t, err)
}

func TestExtractTextFromLocation_EmptyContent(t *testing.T) {
	t.Parallel()
	content := []byte("")

	location := protocol.Location{
		URI: "file:///path/to/file.js",
		Range: protocol.Range{
			Start: protocol.Position{Line: 0, Character: 0},
			End:   protocol.Position{Line: 0, Character: 0},
		},
	}

	result, err := extractTextFromContent(content, location)
	assert.NoError(t, err)
	assert.Equal(t, "", result)
}

func TestContainsPosition(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		r        protocol.Range
		p        protocol.Position
		expected bool
	}{
		{
			name: "Position inside range - middle",
			r: protocol.Range{
				Start: protocol.Position{Line: 5, Character: 10},
				End:   protocol.Position{Line: 10, Character: 20},
			},
			p:        protocol.Position{Line: 7, Character: 15},
			expected: true,
		},
		{
			name: "Position at range start line but after start character",
			r: protocol.Range{
				Start: protocol.Position{Line: 5, Character: 10},
				End:   protocol.Position{Line: 10, Character: 20},
			},
			p:        protocol.Position{Line: 5, Character: 15},
			expected: true,
		},
		{
			name: "Position at range start exact",
			r: protocol.Range{
				Start: protocol.Position{Line: 5, Character: 10},
				End:   protocol.Position{Line: 10, Character: 20},
			},
			p:        protocol.Position{Line: 5, Character: 10},
			expected: true,
		},
		{
			name: "Position at range end line but before end character",
			r: protocol.Range{
				Start: protocol.Position{Line: 5, Character: 10},
				End:   protocol.Position{Line: 10, Character: 20},
			},
			p:        protocol.Position{Line: 10, Character: 15},
			expected: true,
		},
		{
			name: "Position at range end exact",
			r: protocol.Range{
				Start: protocol.Position{Line: 5, Character: 10},
				End:   protocol.Position{Line: 10, Character: 20},
			},
			p:        protocol.Position{Line: 10, Character: 20},
			expected: false, // End position is exclusive
		},
		{
			name: "Position before range start line",
			r: protocol.Range{
				Start: protocol.Position{Line: 5, Character: 10},
				End:   protocol.Position{Line: 10, Character: 20},
			},
			p:        protocol.Position{Line: 4, Character: 15},
			expected: false,
		},
		{
			name: "Position after range end line",
			r: protocol.Range{
				Start: protocol.Position{Line: 5, Character: 10},
				End:   protocol.Position{Line: 10, Character: 20},
			},
			p:        protocol.Position{Line: 11, Character: 15},
			expected: false,
		},
		{
			name: "Position at start line but before start character",
			r: protocol.Range{
				Start: protocol.Position{Line: 5, Character: 10},
				End:   protocol.Position{Line: 10, Character: 20},
			},
			p:        protocol.Position{Line: 5, Character: 5},
			expected: false,
		},
		{
			name: "Position at end line but after end character",
			r: protocol.Range{
				Start: protocol.Position{Line: 5, Character: 10},
				End:   protocol.Position{Line: 10, Character: 20},
			},
			p:        protocol.Position{Line: 10, Character: 25},
			expected: false,
		},
		{
			name: "Same line range",
			r: protocol.Range{
				Start: protocol.Position{Line: 5, Character: 10},
				End:   protocol.Position{Line: 5, Character: 20},
			},
			p:        protocol.Position{Line: 5, Character: 15},
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := containsPosition(tc.r, tc.p)
			assert.Equal(t, tc.expected, result, "Expected containsPosition to return %v for range %v and position %v",
				tc.expected, tc.r, tc.p)
		})
	}
}

func TestAddLineNumbers(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name      string
		text      string
		startLine int
		expected  string
	}{
		{
			name:      "Single line",
			text:      "function test() {}",
			startLine: 1,
			expected:  "1|function test() {}\n",
		},
		{
			name:      "Multiple lines",
			text:      "function test() {\n  return true;\n}",
			startLine: 10,
			expected:  "10|function test() {\n11|  return true;\n12|}\n",
		},
		{
			name:      "Padding for large line numbers",
			text:      "line1\nline2\nline3",
			startLine: 998,
			expected:  " 998|line1\n 999|line2\n1000|line3\n",
		},
		{
			name:      "Empty string",
			text:      "",
			startLine: 1,
			expected:  "1|\n",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := addLineNumbers(tc.text, tc.startLine)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestConvertLinesToRanges(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name        string
		linesToShow map[int]bool
		totalLines  int
		expected    []LineRange
	}{
		{
			name:        "Empty map",
			linesToShow: map[int]bool{},
			totalLines:  10,
			expected:    nil, // The function returns nil for empty input
		},
		{
			name:        "Single line",
			linesToShow: map[int]bool{5: true},
			totalLines:  10,
			expected:    []LineRange{{Start: 5, End: 5}},
		},
		{
			name:        "Consecutive lines",
			linesToShow: map[int]bool{1: true, 2: true, 3: true},
			totalLines:  10,
			expected:    []LineRange{{Start: 1, End: 3}},
		},
		{
			name:        "Non-consecutive lines",
			linesToShow: map[int]bool{1: true, 3: true, 5: true},
			totalLines:  10,
			expected:    []LineRange{{Start: 1, End: 1}, {Start: 3, End: 3}, {Start: 5, End: 5}},
		},
		{
			name:        "Mixed consecutive and non-consecutive lines",
			linesToShow: map[int]bool{1: true, 2: true, 5: true, 6: true, 7: true, 9: true},
			totalLines:  10,
			expected:    []LineRange{{Start: 1, End: 2}, {Start: 5, End: 7}, {Start: 9, End: 9}},
		},
		{
			name:        "Lines outside range are filtered",
			linesToShow: map[int]bool{-1: true, 0: true, 9: true, 10: true},
			totalLines:  10,
			expected:    []LineRange{{Start: 0, End: 0}, {Start: 9, End: 9}},
		},
		{
			name:        "Unsorted input gets sorted",
			linesToShow: map[int]bool{5: true, 1: true, 3: true, 2: true},
			totalLines:  10,
			expected:    []LineRange{{Start: 1, End: 3}, {Start: 5, End: 5}},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := ConvertLinesToRanges(tc.linesToShow, tc.totalLines)
			assert.Equal(t, tc.expected, result, "Expected ranges to match")
		})
	}
}

func TestFormatLinesWithRanges(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		lines    []string
		ranges   []LineRange
		expected string
	}{
		{
			name:     "Empty ranges",
			lines:    []string{"line1", "line2", "line3"},
			ranges:   []LineRange{},
			expected: "",
		},
		{
			name:     "Single range",
			lines:    []string{"line1", "line2", "line3", "line4", "line5"},
			ranges:   []LineRange{{Start: 1, End: 3}},
			expected: "2|line2\n3|line3\n4|line4\n",
		},
		{
			name:     "Multiple ranges with gap",
			lines:    []string{"line1", "line2", "line3", "line4", "line5", "line6", "line7"},
			ranges:   []LineRange{{Start: 0, End: 1}, {Start: 4, End: 6}},
			expected: "1|line1\n2|line2\n...\n5|line5\n6|line6\n7|line7\n",
		},
		{
			name:     "Adjacent ranges get combined - no gap in output",
			lines:    []string{"line1", "line2", "line3", "line4", "line5"},
			ranges:   []LineRange{{Start: 0, End: 2}, {Start: 3, End: 4}},
			expected: "1|line1\n2|line2\n3|line3\n4|line4\n5|line5\n",
		},
		{
			name: "Real-world example",
			lines: []string{
				"package main",
				"",
				"import \"fmt\"",
				"",
				"func main() {",
				"    s := \"Hello, World!\"",
				"    fmt.Println(s)",
				"}",
			},
			ranges:   []LineRange{{Start: 4, End: 6}},
			expected: "5|func main() {\n6|    s := \"Hello, World!\"\n7|    fmt.Println(s)\n",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := FormatLinesWithRanges(tc.lines, tc.ranges)
			assert.Equal(t, tc.expected, result, "Expected formatted output to match")
		})
	}
}

func TestRelativePath(t *testing.T) {
	// Save and restore workspace root
	originalRoot := workspaceRoot
	defer func() { workspaceRoot = originalRoot }()

	testCases := []struct {
		name     string
		root     string
		absPath  string
		expected string
	}{
		{
			name:     "Empty root returns path as-is",
			root:     "",
			absPath:  "/home/user/project/src/main.go",
			expected: "/home/user/project/src/main.go",
		},
		{
			name:     "Path under workspace root",
			root:     "/home/user/project",
			absPath:  "/home/user/project/src/main.go",
			expected: "src/main.go",
		},
		{
			name:     "Path is workspace root itself",
			root:     "/home/user/project",
			absPath:  "/home/user/project",
			expected: ".",
		},
		{
			name:     "Path outside workspace root",
			root:     "/home/user/project",
			absPath:  "/other/path/file.go",
			expected: "../../../other/path/file.go",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			workspaceRoot = tc.root
			result := RelativePath(tc.absPath)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestEstimateTokens(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		input    string
		expected int
	}{
		{name: "Empty string", input: "", expected: 0},
		{name: "7 chars", input: "abcdefg", expected: 2},
		{name: "35 chars", input: strings.Repeat("a", 35), expected: 10},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, EstimateTokens(tc.input))
		})
	}
}

func TestTruncateResponse(t *testing.T) {
	t.Parallel()

	// Save and restore maxResponseSize
	origMax := maxResponseSize
	defer func() { maxResponseSize = origMax }()
	maxResponseSize = 100

	t.Run("Short response unchanged", func(t *testing.T) {
		result := TruncateResponse("hello world")
		assert.Equal(t, "hello world", result)
	})

	t.Run("Exactly at limit unchanged", func(t *testing.T) {
		input := strings.Repeat("a", 100)
		result := TruncateResponse(input)
		assert.Equal(t, input, result)
	})

	t.Run("Cuts at line boundary", func(t *testing.T) {
		// 90 chars + newline + 20 chars = 111 chars, over limit of 100
		input := strings.Repeat("a", 90) + "\n" + strings.Repeat("b", 20)
		result := TruncateResponse(input)
		assert.True(t, strings.HasPrefix(result, strings.Repeat("a", 90)))
		assert.Contains(t, result, "... (truncated")
		assert.NotContains(t, result, "bbb")
	})

	t.Run("Prefers block boundary over line boundary", func(t *testing.T) {
		// Block boundary at position 60 (>50% of 100), line boundary at 80
		input := strings.Repeat("a", 59) + "\n---\n" + strings.Repeat("b", 16) + "\n" + strings.Repeat("c", 30)
		result := TruncateResponse(input)
		// Should cut at \n---\n (position 59), not the later \n
		assert.True(t, strings.HasPrefix(result, strings.Repeat("a", 59)))
		assert.Contains(t, result, "... (truncated")
	})

	t.Run("Skips block boundary if too early", func(t *testing.T) {
		// Block boundary at position 20 (<50% of 100), line boundary at 90
		input := strings.Repeat("a", 19) + "\n---\n" + strings.Repeat("b", 66) + "\n" + strings.Repeat("c", 20)
		result := TruncateResponse(input)
		// Should skip block boundary (too early) and use line boundary at 90
		assert.True(t, len(result) > 50, "Should use line boundary, not block boundary")
		assert.Contains(t, result, "... (truncated")
	})

	t.Run("Falls back to char cut when no newline", func(t *testing.T) {
		input := strings.Repeat("a", 200)
		result := TruncateResponse(input)
		assert.Contains(t, result, "... (truncated")
		// First 100 chars preserved
		assert.True(t, strings.HasPrefix(result, strings.Repeat("a", 100)))
	})

	t.Run("Footer contains token estimates", func(t *testing.T) {
		input := strings.Repeat("a", 90) + "\n" + strings.Repeat("b", 20)
		result := TruncateResponse(input)
		assert.Contains(t, result, "estimated tokens")
		assert.Contains(t, result, "chars")
	})
}

func TestTrimResponse(t *testing.T) {
	t.Parallel()
	// TrimResponse delegates to TruncateResponse
	result := TrimResponse("hello")
	assert.Equal(t, "hello", result)
}
