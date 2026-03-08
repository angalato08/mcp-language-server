package protocol

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestURIFromPathAndBack(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("POSIX-only test")
	}

	tests := []struct {
		name string
		path string
	}{
		{"simple absolute path", "/home/user/project/main.go"},
		{"root path", "/"},
		{"nested path", "/a/b/c/d/e/f.txt"},
		{"path with dots", "/home/user/project/../other/file.go"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			uri := URIFromPath(tc.path)
			assert.NotEmpty(t, uri)
			assert.Contains(t, string(uri), "file:///")

			// Path() should return a valid path (may be cleaned)
			gotPath := uri.Path()
			assert.NotEmpty(t, gotPath)
		})
	}
}

func TestURIFromPathEmpty(t *testing.T) {
	t.Parallel()

	uri := URIFromPath("")
	assert.Equal(t, DocumentUri(""), uri)
}

func TestDocumentUriPathEmpty(t *testing.T) {
	t.Parallel()

	uri := DocumentUri("")
	assert.Equal(t, "", uri.Path())
}

func TestDocumentUriPath(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("POSIX-only test")
	}

	tests := []struct {
		name     string
		uri      DocumentUri
		expected string
	}{
		{"simple file URI", DocumentUri("file:///home/user/test.go"), "/home/user/test.go"},
		{"root file", DocumentUri("file:///test.go"), "/test.go"},
		{"nested path", DocumentUri("file:///a/b/c.txt"), "/a/b/c.txt"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, tc.uri.Path())
		})
	}
}

func TestDocumentUriDir(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("POSIX-only test")
	}

	uri := DocumentUri("file:///home/user/project/main.go")
	dir := uri.Dir()
	assert.Contains(t, string(dir), "file:///home/user/project")
	assert.NotContains(t, string(dir), "main.go")
}

func TestDocumentUriDirPath(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("POSIX-only test")
	}

	uri := DocumentUri("file:///home/user/project/main.go")
	assert.Equal(t, "/home/user/project", uri.DirPath())
}

func TestParseDocumentUri(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantErr   bool
		checkFunc func(t *testing.T, uri DocumentUri)
	}{
		{
			name:  "empty string",
			input: "",
			checkFunc: func(t *testing.T, uri DocumentUri) {
				assert.Equal(t, DocumentUri(""), uri)
			},
		},
		{
			name:  "valid three-slash URI",
			input: "file:///home/user/test.go",
			checkFunc: func(t *testing.T, uri DocumentUri) {
				assert.Contains(t, string(uri), "file:///home/user/test.go")
			},
		},
		{
			name:  "two-slash URI (VS Code bug) gets fixed",
			input: "file://home/user/test.go",
			checkFunc: func(t *testing.T, uri DocumentUri) {
				// Should be converted to three slashes
				assert.Contains(t, string(uri), "file:///")
			},
		},
		{
			name:    "non-file scheme rejected",
			input:   "https://example.com/file.go",
			wantErr: true,
		},
		{
			name:    "no scheme rejected",
			input:   "/just/a/path",
			wantErr: true,
		},
		{
			name:  "encoded colon in drive letter",
			input: "file:///C%3A/project/readme.md",
			checkFunc: func(t *testing.T, uri DocumentUri) {
				assert.NotEmpty(t, uri)
				// Should normalize - the path should be accessible
			},
		},
		{
			name:  "over-escaped URI (VS Code)",
			input: "file:///home/user/my%20project/test.go",
			checkFunc: func(t *testing.T, uri DocumentUri) {
				assert.NotEmpty(t, uri)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			uri, err := ParseDocumentUri(tc.input)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tc.checkFunc != nil {
				tc.checkFunc(t, uri)
			}
		})
	}
}

func TestParseDocumentUriWindowsDriveLetter(t *testing.T) {
	t.Parallel()

	// Lowercase drive letter should be uppercased
	uri, err := ParseDocumentUri("file:///c:/project/readme.md")
	require.NoError(t, err)
	assert.Contains(t, string(uri), "C:")
	assert.NotContains(t, string(uri), "c:")
}

func TestUnmarshalText(t *testing.T) {
	t.Parallel()

	var uri DocumentUri
	err := uri.UnmarshalText([]byte("file:///home/user/test.go"))
	require.NoError(t, err)
	assert.Contains(t, string(uri), "file:///home/user/test.go")
}

func TestUnmarshalTextEmpty(t *testing.T) {
	t.Parallel()

	var uri DocumentUri
	err := uri.UnmarshalText([]byte(""))
	require.NoError(t, err)
	assert.Equal(t, DocumentUri(""), uri)
}

func TestUnmarshalTextInvalid(t *testing.T) {
	t.Parallel()

	var uri DocumentUri
	err := uri.UnmarshalText([]byte("not-a-file-uri"))
	assert.Error(t, err)
}

func TestURIRoundtrip(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("POSIX-only test")
	}

	// Path -> URI -> Path should be identity (for clean paths)
	path := "/home/user/project/src/main.go"
	uri := URIFromPath(path)
	gotPath := uri.Path()
	assert.Equal(t, path, gotPath)
}

func TestIsWindowsDrivePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{"Windows drive C:", "C:/Users/test", true},
		{"Windows drive lowercase", "c:/Users/test", true},
		{"Unix path", "/home/user", false},
		{"too short", "C:", false},
		{"no colon", "C/test", false},
		{"digit prefix", "1:/test", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, isWindowsDrivePath(tc.path))
		})
	}
}

func TestIsWindowsDriveURIPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{"Windows URI path", "/C:/Users/test", true},
		{"lowercase drive", "/c:/Users/test", true},
		{"Unix path", "/home/user", false},
		{"too short", "/C:", false},
		{"no leading slash", "C:/test", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, isWindowsDriveURIPath(tc.path))
		})
	}
}
