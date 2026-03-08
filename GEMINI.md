# Foundational Mandates

- **Tool Preference:** Always prioritize using the MCP Language Server tools (`definition`, `get_definition`, `hover`, `references`, `get_references`, `diagnostics`, `document_symbols`, `workspace_symbols`, `implementation`, `get_implementation`, `incoming_calls`, `outgoing_calls`, `format_file`) for code analysis, navigation, and understanding.
- **Context Efficiency:** Use `document_symbols` to understand file structure before reading large files. Use `definition` and `references` to navigate the codebase semantically rather than using `grep_search`.
- **Validation:** Use the `diagnostics` and `workspace_diagnostics` tools to verify code changes and identify syntax or type errors in real-time.
- **Avoid Raw Tools:** Only use `read_file` or `grep_search` if the language server is unavailable, if the task is purely textual, or if the language server fails to provide the necessary semantic information.
