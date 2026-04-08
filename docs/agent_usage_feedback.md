# Agent Usage Feedback — mcp-language-server Instructions

Real-world feedback from an AI agent (Claude) using mcp-language-server as a Read/Grep replacement during a codebase exploration task.

## What works well

- `api_overview` is excellent as a first step for understanding a file's public surface — struct fields, method signatures, doc comments, all in one call.
- The "Recommended Workflows" section in the server instructions is helpful and well-structured.
- `outgoing_calls` / `incoming_calls` are incredibly powerful for tracing code flow without reading implementation.

## Key issue: `get_definition` is undersold

The single biggest gap in the current instructions is that **`get_definition` returns the full source code of a symbol's implementation**, not just a location pointer. This makes it the direct replacement for `Read` on source files.

The current instructions say:
> `get_definition(filePath, line, column) → jump to source code`

"Jump to source code" sounds like navigation — it suggests you get a file path and line number back. In reality, you get the **complete function body / struct definition / implementation block**. This is the most important thing an agent needs to know to stop reaching for `Read`.

### Suggested rewording

```
get_definition(filePath, line, column) → read the full source implementation of a symbol
```

## Missing: "Instead of Read, do X" translation table

Agents default to `Read` because it's familiar. The instructions should include an explicit mapping:

| Instead of...                        | Use...                                      |
|--------------------------------------|---------------------------------------------|
| `Read` a whole file to understand it | `api_overview` for structure + signatures    |
| `Read` to see a function's code      | `get_definition` for the full implementation |
| `Grep` for a symbol name             | `workspace_symbols` or `references`          |
| `Read` to check a type/signature     | `hover`                                      |
| `Read` to follow function calls      | `outgoing_calls`                             |
| `Read` to find callers               | `incoming_calls`                             |

## `outgoing_calls` / `incoming_calls` deserve more emphasis

The current instructions list these under "Understanding call flow" but don't convey how powerful they are. During the exploration, `outgoing_calls` on a single function gave me the complete call graph — every function it invokes, with source locations — which would have required reading the entire function body and manually tracing calls otherwise.

Suggest adding a note like:
> `outgoing_calls` and `incoming_calls` are often more useful than reading a function's implementation directly, because they show you the call graph in structured form without needing to parse the code yourself.

## No fallback guidance for LSP failures

During the session, the LSP server restarted mid-exploration (3 consecutive calls failed), and some files returned "No hover information available" for all symbols. The instructions don't mention:
- What to do when the server restarts (retry after a moment)
- What to do when hover returns no data (fall back to `get_definition` or `api_overview`)
- How to check server health (`server_status`)

Suggest adding a "Troubleshooting" section.

## Summary of suggested changes to `serverInstructions`

1. Reword `get_definition` description to say it returns full source code, not just a location
2. Add a "Instead of Read/Grep" translation table
3. Emphasize `outgoing_calls` / `incoming_calls` as a primary exploration tool
4. Add a brief troubleshooting/fallback note
