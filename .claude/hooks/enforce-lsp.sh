#!/bin/bash
# enforce-lsp.sh - Block direct source file access; force MCP language server usage.
# Exit code 2 = BLOCK (message fed back to Claude)
# Exit code 0 = ALLOW

INPUT=$(cat)

TOOL_NAME=$(echo "$INPUT" | jq -r '.tool_name // empty')
FILE_PATH=$(echo "$INPUT" | jq -r '.tool_input.file_path // .tool_input.path // empty')
OFFSET=$(echo "$INPUT" | jq -r '.tool_input.offset // empty')
PATTERN=$(echo "$INPUT" | jq -r '.tool_input.pattern // empty')
GLOB=$(echo "$INPUT" | jq -r '.tool_input.glob // empty')

# Source file extensions that have LSP support
SOURCE_EXT="go|py|rs|ts|tsx|js|jsx|c|cpp|cxx|cc|h|hpp|hxx"

is_source_file() {
  [[ "$1" =~ \.($SOURCE_EXT)$ ]]
}

is_source_glob() {
  local p="$1"
  [[ "$p" =~ \*\.($SOURCE_EXT) ]] || [[ "$p" == "**/*" ]]
}

case "$TOOL_NAME" in
  Read)
    # Allow targeted reads (with offset) — needed for editing
    if [ -n "$OFFSET" ]; then
      exit 0
    fi
    if [ -n "$FILE_PATH" ] && is_source_file "$FILE_PATH"; then
      echo "BLOCKED: Use MCP language server instead of reading source files directly." >&2
      echo "" >&2
      echo "Available tools:" >&2
      echo "  api_overview  - Get all symbols with signatures and docs (best first step)" >&2
      echo "  document_symbols - List symbols in a file" >&2
      echo "  hover         - Type signature + documentation at a position" >&2
      echo "  get_definition - Jump to source definition" >&2
      echo "  get_references - Find all usages" >&2
      echo "  diagnostics   - Check for errors after edits" >&2
      echo "" >&2
      echo "To read specific lines for editing, use Read with offset/limit parameters." >&2
      exit 2
    fi
    ;;
  Grep)
    # Check file path or glob filter for source extensions
    if [ -n "$FILE_PATH" ] && is_source_file "$FILE_PATH"; then
      echo "BLOCKED: Use MCP language server instead of grepping source files." >&2
      echo "  workspace_symbols - Search symbols by name" >&2
      echo "  references        - Find all usages of a symbol" >&2
      echo "  get_references    - Find references at a position" >&2
      exit 2
    fi
    if [ -n "$GLOB" ] && is_source_glob "$GLOB"; then
      echo "BLOCKED: Use MCP language server instead of grepping source files." >&2
      echo "  workspace_symbols - Search symbols by name" >&2
      echo "  references        - Find all usages of a symbol" >&2
      exit 2
    fi
    ;;
  Glob)
    if [ -n "$PATTERN" ] && is_source_glob "$PATTERN"; then
      echo "BLOCKED: Use MCP language server instead of globbing for source files." >&2
      echo "  workspace_symbols - Search symbols across the project" >&2
      echo "  document_symbols  - List symbols in a specific file" >&2
      exit 2
    fi
    ;;
esac

exit 0
