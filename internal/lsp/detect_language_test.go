package lsp

import (
	"testing"

	"github.com/angalato08/mcp-language-server/internal/protocol"
	"github.com/stretchr/testify/assert"
)

func TestDetectLanguageID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		uri      string
		expected protocol.LanguageKind
	}{
		// Common languages
		{"Go", "file:///project/main.go", protocol.LangGo},
		{"Python", "file:///project/app.py", protocol.LangPython},
		{"Rust", "file:///project/main.rs", protocol.LangRust},
		{"TypeScript", "file:///project/index.ts", protocol.LangTypeScript},
		{"TypeScriptReact", "file:///project/App.tsx", protocol.LangTypeScriptReact},
		{"JavaScript", "file:///project/index.js", protocol.LangJavaScript},
		{"JavaScriptReact", "file:///project/App.jsx", protocol.LangJavaScriptReact},
		{"Java", "file:///project/Main.java", protocol.LangJava},
		{"C", "file:///project/main.c", protocol.LangC},
		{"C++", "file:///project/main.cpp", protocol.LangCPP},
		{"C++ .cc", "file:///project/main.cc", protocol.LangCPP},
		{"C++ .cxx", "file:///project/main.cxx", protocol.LangCPP},
		{"C header .h", "file:///project/helper.h", protocol.LangC},
		{"C++ header .hpp", "file:///project/helper.hpp", protocol.LangCPP},
		{"C++ header .hxx", "file:///project/helper.hxx", protocol.LangCPP},
		{"C#", "file:///project/Program.cs", protocol.LangCSharp},
		{"Ruby", "file:///project/app.rb", protocol.LangRuby},
		{"PHP", "file:///project/index.php", protocol.LangPHP},
		{"Swift", "file:///project/main.swift", protocol.LangSwift},
		{"Scala", "file:///project/Main.scala", protocol.LangScala},

		// Markup and config
		{"HTML", "file:///project/index.html", protocol.LangHTML},
		{"HTML .htm", "file:///project/index.htm", protocol.LangHTML},
		{"CSS", "file:///project/style.css", protocol.LangCSS},
		{"SCSS", "file:///project/style.scss", protocol.LangSCSS},
		{"JSON", "file:///project/package.json", protocol.LangJSON},
		{"YAML .yml", "file:///project/config.yml", protocol.LangYAML},
		{"YAML .yaml", "file:///project/config.yaml", protocol.LangYAML},
		{"XML", "file:///project/pom.xml", protocol.LangXML},
		{"Markdown", "file:///project/README.md", protocol.LangMarkdown},

		// Shell scripts
		{"Shell .sh", "file:///project/run.sh", protocol.LangShellScript},
		{"Shell .bash", "file:///project/run.bash", protocol.LangShellScript},
		{"Shell .zsh", "file:///project/run.zsh", protocol.LangShellScript},

		// Case insensitivity
		{"uppercase ext", "file:///project/Main.GO", protocol.LangGo},
		{"mixed case ext", "file:///project/Main.Py", protocol.LangPython},

		// Unknown
		{"unknown extension", "file:///project/data.xyz", protocol.LanguageKind("")},
		{"no extension", "file:///project/Makefile", protocol.LanguageKind("")},

		// Edge cases
		{"bare path", "/tmp/test.go", protocol.LangGo},
		{"dot file", ".gitignore", protocol.LanguageKind("")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := DetectLanguageID(tc.uri)
			assert.Equal(t, tc.expected, result)
		})
	}
}
