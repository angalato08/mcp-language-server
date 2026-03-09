package main

import (
	"context"
	"fmt"

	"github.com/angalato08/mcp-language-server/internal/tools"
	"github.com/mark3labs/mcp-go/mcp"
)

func (s *mcpServer) registerTools() error {
	coreLogger.Debug("Registering MCP tools")

	// Helper function to extract int from map
	getInt := func(args map[string]any, key string, defaultVal int) int {
		if v, ok := args[key]; ok {
			switch val := v.(type) {
			case float64:
				return int(val)
			case int:
				return val
			}
		}
		return defaultVal
	}

	applyTextEditTool := mcp.NewTool("edit_file",
		mcp.WithDescription("Apply multiple text edits to a file."),
		mcp.WithArray("edits",
			mcp.Required(),
			mcp.Description("List of edits to apply"),
			mcp.Items(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"startLine": map[string]any{
						"type":        "number",
						"description": "Start line to replace, inclusive, one-indexed",
					},
					"endLine": map[string]any{
						"type":        "number",
						"description": "End line to replace, inclusive, one-indexed",
					},
					"newText": map[string]any{
						"type":        "string",
						"description": "Replacement text. Replace with the new text. Leave blank to remove lines.",
					},
				},
				"required": []string{"startLine", "endLine"},
			}),
		),
		mcp.WithString("filePath",
			mcp.Required(),
			mcp.Description("Path to the file to edit"),
		),
	)

	s.mcpServer.AddTool(applyTextEditTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filePath, ok := request.Params.Arguments["filePath"].(string)
		if !ok {
			return mcp.NewToolResultError("filePath must be a string"), nil
		}
		filePath = tools.ResolveFilePath(filePath)

		editsArg, ok := request.Params.Arguments["edits"]
		if !ok {
			return mcp.NewToolResultError("edits is required"), nil
		}

		editsArray, ok := editsArg.([]any)
		if !ok {
			return mcp.NewToolResultError("edits must be an array"), nil
		}

		var edits []tools.TextEdit
		for _, editItem := range editsArray {
			editMap, ok := editItem.(map[string]any)
			if !ok {
				return mcp.NewToolResultError("each edit must be an object"), nil
			}

			startLine := getInt(editMap, "startLine", 0)
			endLine := getInt(editMap, "endLine", 0)
			newText, _ := editMap["newText"].(string)

			edits = append(edits, tools.TextEdit{
				StartLine: startLine,
				EndLine:   endLine,
				NewText:   newText,
			})
		}

		coreLogger.Debug("Executing edit_file for file: %s", filePath)
		client, err := s.router.ClientForFile(ctx, filePath)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		response, err := tools.ApplyTextEdits(ctx, client, filePath, edits)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to apply edits: %v", err)), nil
		}
		return mcp.NewToolResultText(tools.TrimResponse(response)), nil
	})

	readDefinitionTool := mcp.NewTool("definition",
		mcp.WithDescription("Read the source code definition of a symbol (function, type, constant, etc.) from the codebase. Returns the complete implementation code where the symbol is defined."),
		mcp.WithString("symbolName",
			mcp.Required(),
			mcp.Description("The name of the symbol whose definition you want to find (e.g. 'mypackage.MyFunction', 'MyType.MyMethod')"),
		),
	)

	s.mcpServer.AddTool(readDefinitionTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		symbolName, ok := request.Params.Arguments["symbolName"].(string)
		if !ok {
			return mcp.NewToolResultError("symbolName must be a string"), nil
		}

		coreLogger.Debug("Executing definition for symbol: %s", symbolName)
		clients := s.router.ActiveClients()
		if len(clients) == 0 {
			return mcp.NewToolResultError("no LSP servers are running; make a file-based request first to start a server"), nil
		}
		var lastErr error
		for _, client := range clients {
			text, err := tools.ReadDefinition(ctx, client, symbolName)
			if err == nil {
				return mcp.NewToolResultText(tools.TrimResponse(text)), nil
			}
			lastErr = err
		}
		return mcp.NewToolResultError(fmt.Sprintf("failed to get definition: %v", lastErr)), nil
	})

	typeDefinitionTool := mcp.NewTool("type_definition",
		mcp.WithDescription("Read the type definition of a symbol from the codebase. Returns the complete implementation code where the symbol's type is defined."),
		mcp.WithString("symbolName",
			mcp.Required(),
			mcp.Description("The name of the symbol whose type definition you want to find"),
		),
	)

	s.mcpServer.AddTool(typeDefinitionTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		symbolName, ok := request.Params.Arguments["symbolName"].(string)
		if !ok {
			return mcp.NewToolResultError("symbolName must be a string"), nil
		}

		coreLogger.Debug("Executing type_definition for symbol: %s", symbolName)
		clients := s.router.ActiveClients()
		if len(clients) == 0 {
			return mcp.NewToolResultError("no LSP servers are running; make a file-based request first to start a server"), nil
		}
		var lastErr error
		for _, client := range clients {
			text, err := tools.ReadTypeDefinition(ctx, client, symbolName)
			if err == nil {
				return mcp.NewToolResultText(tools.TrimResponse(text)), nil
			}
			lastErr = err
		}
		return mcp.NewToolResultError(fmt.Sprintf("failed to get type definition: %v", lastErr)), nil
	})

	getDefinitionTool := mcp.NewTool("get_definition",
		mcp.WithDescription("Read the source code definition of a symbol at the specified position. Returns the complete implementation code where the symbol is defined."),
		mcp.WithString("filePath",
			mcp.Required(),
			mcp.Description("The path to the file to get definition for"),
		),
		mcp.WithNumber("line",
			mcp.Required(),
			mcp.Description("The line number (1-indexed)"),
		),
		mcp.WithNumber("column",
			mcp.Required(),
			mcp.Description("The column number (1-indexed)"),
		),
	)

	s.mcpServer.AddTool(getDefinitionTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filePath, ok := request.Params.Arguments["filePath"].(string)
		if !ok {
			return mcp.NewToolResultError("filePath must be a string"), nil
		}
		filePath = tools.ResolveFilePath(filePath)
		line := getInt(request.Params.Arguments, "line", 0)
		column := getInt(request.Params.Arguments, "column", 0)

		coreLogger.Debug("Executing get_definition for file: %s line: %d column: %d", filePath, line, column)
		client, err := s.router.ClientForFile(ctx, filePath)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		text, err := tools.ReadDefinitionAtPosition(ctx, client, filePath, line, column)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to get definition: %v", err)), nil
		}
		return mcp.NewToolResultText(tools.TrimResponse(text)), nil
	})

	getTypeDefinitionTool := mcp.NewTool("get_type_definition",
		mcp.WithDescription("Read the type definition of a symbol at the specified position. Returns the complete implementation code where the symbol's type is defined."),
		mcp.WithString("filePath",
			mcp.Required(),
			mcp.Description("The path to the file to get type definition for"),
		),
		mcp.WithNumber("line",
			mcp.Required(),
			mcp.Description("The line number (1-indexed)"),
		),
		mcp.WithNumber("column",
			mcp.Required(),
			mcp.Description("The column number (1-indexed)"),
		),
	)

	s.mcpServer.AddTool(getTypeDefinitionTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filePath, ok := request.Params.Arguments["filePath"].(string)
		if !ok {
			return mcp.NewToolResultError("filePath must be a string"), nil
		}
		filePath = tools.ResolveFilePath(filePath)
		line := getInt(request.Params.Arguments, "line", 0)
		column := getInt(request.Params.Arguments, "column", 0)

		coreLogger.Debug("Executing get_type_definition for file: %s line: %d column: %d", filePath, line, column)
		client, err := s.router.ClientForFile(ctx, filePath)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		text, err := tools.ReadTypeDefinitionAtPosition(ctx, client, filePath, line, column)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to get type definition: %v", err)), nil
		}
		return mcp.NewToolResultText(tools.TrimResponse(text)), nil
	})

	findReferencesTool := mcp.NewTool("references",
		mcp.WithDescription("Find all usages and references of a symbol throughout the codebase. Returns a list of all files and locations where the symbol appears."),
		mcp.WithString("symbolName",
			mcp.Required(),
			mcp.Description("The name of the symbol to search for (e.g. 'mypackage.MyFunction', 'MyType')"),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of references to return. Default 30. Use -1 for all."),
		),
	)

	s.mcpServer.AddTool(findReferencesTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		symbolName, ok := request.Params.Arguments["symbolName"].(string)
		if !ok {
			return mcp.NewToolResultError("symbolName must be a string"), nil
		}
		limit := getInt(request.Params.Arguments, "limit", 30)

		coreLogger.Debug("Executing references for symbol: %s", symbolName)
		clients := s.router.ActiveClients()
		if len(clients) == 0 {
			return mcp.NewToolResultError("no LSP servers are running; make a file-based request first to start a server"), nil
		}
		var lastErr error
		for _, client := range clients {
			text, err := tools.FindReferences(ctx, client, symbolName, limit)
			if err == nil {
				return mcp.NewToolResultText(tools.TrimResponse(text)), nil
			}
			lastErr = err
		}
		return mcp.NewToolResultError(fmt.Sprintf("failed to find references: %v", lastErr)), nil
	})

	getReferencesTool := mcp.NewTool("get_references",
		mcp.WithDescription("Find all usages and references of a symbol at the specified position."),
		mcp.WithString("filePath",
			mcp.Required(),
			mcp.Description("The path to the file containing the symbol"),
		),
		mcp.WithNumber("line",
			mcp.Required(),
			mcp.Description("The line number (1-indexed)"),
		),
		mcp.WithNumber("column",
			mcp.Required(),
			mcp.Description("The column number (1-indexed)"),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of references to return. Default 30. Use -1 for all."),
		),
	)

	s.mcpServer.AddTool(getReferencesTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filePath, ok := request.Params.Arguments["filePath"].(string)
		if !ok {
			return mcp.NewToolResultError("filePath must be a string"), nil
		}
		filePath = tools.ResolveFilePath(filePath)
		line := getInt(request.Params.Arguments, "line", 0)
		column := getInt(request.Params.Arguments, "column", 0)
		limit := getInt(request.Params.Arguments, "limit", 30)

		coreLogger.Debug("Executing get_references for file: %s line: %d column: %d", filePath, line, column)
		client, err := s.router.ClientForFile(ctx, filePath)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		text, err := tools.FindReferencesAtPosition(ctx, client, filePath, line, column, limit)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to find references: %v", err)), nil
		}
		return mcp.NewToolResultText(tools.TrimResponse(text)), nil
	})

	getDiagnosticsTool := mcp.NewTool("diagnostics",
		mcp.WithDescription("Get diagnostic information for a specific file from the language server."),
		mcp.WithString("filePath",
			mcp.Required(),
			mcp.Description("The path to the file to get diagnostics for"),
		),
		mcp.WithNumber("contextLines",
			mcp.Description("Lines to include around each diagnostic."),
			mcp.DefaultNumber(5),
		),
		mcp.WithBoolean("showLineNumbers",
			mcp.Description("If true, adds line numbers to the output"),
			mcp.DefaultBool(true),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of diagnostics to return. Default 20. Use -1 for all."),
		),
	)

	s.mcpServer.AddTool(getDiagnosticsTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filePath, ok := request.Params.Arguments["filePath"].(string)
		if !ok {
			return mcp.NewToolResultError("filePath must be a string"), nil
		}
		filePath = tools.ResolveFilePath(filePath)
		contextLines := getInt(request.Params.Arguments, "contextLines", 5)
		showLineNumbers := true
		if v, ok := request.Params.Arguments["showLineNumbers"].(bool); ok {
			showLineNumbers = v
		}
		limit := getInt(request.Params.Arguments, "limit", 20)

		coreLogger.Debug("Executing diagnostics for file: %s", filePath)
		client, err := s.router.ClientForFile(ctx, filePath)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		text, err := tools.GetDiagnosticsForFile(ctx, client, filePath, contextLines, showLineNumbers, limit)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to get diagnostics: %v", err)), nil
		}
		return mcp.NewToolResultText(tools.TrimResponse(text)), nil
	})

	workspaceDiagnosticsTool := mcp.NewTool("workspace_diagnostics",
		mcp.WithDescription("Get diagnostic information for all active files in the workspace."),
	)

	s.mcpServer.AddTool(workspaceDiagnosticsTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		coreLogger.Debug("Executing workspace_diagnostics")
		clients := s.router.ActiveClients()
		if len(clients) == 0 {
			return mcp.NewToolResultError("no LSP servers are running; make a file-based request first to start a server"), nil
		}
		text, err := tools.GetAllDiagnostics(ctx, clients)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to get workspace diagnostics: %v", err)), nil
		}
		return mcp.NewToolResultText(tools.TrimResponse(text)), nil
	})

	hoverTool := mcp.NewTool("hover",
		mcp.WithDescription("Get hover information (type, documentation) for a symbol at the specified position."),
		mcp.WithString("filePath",
			mcp.Required(),
			mcp.Description("The path to the file to get hover information for"),
		),
		mcp.WithNumber("line",
			mcp.Required(),
			mcp.Description("The line number where the hover is requested (1-indexed)"),
		),
		mcp.WithNumber("column",
			mcp.Required(),
			mcp.Description("The column number where the hover is requested (1-indexed)"),
		),
	)

	s.mcpServer.AddTool(hoverTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filePath, ok := request.Params.Arguments["filePath"].(string)
		if !ok {
			return mcp.NewToolResultError("filePath must be a string"), nil
		}
		filePath = tools.ResolveFilePath(filePath)
		line := getInt(request.Params.Arguments, "line", 0)
		column := getInt(request.Params.Arguments, "column", 0)

		coreLogger.Debug("Executing hover for file: %s line: %d column: %d", filePath, line, column)
		client, err := s.router.ClientForFile(ctx, filePath)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		text, err := tools.GetHoverInfo(ctx, client, filePath, line, column)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to get hover information: %v", err)), nil
		}
		return mcp.NewToolResultText(tools.TrimResponse(text)), nil
	})

	renameSymbolTool := mcp.NewTool("rename_symbol",
		mcp.WithDescription("Rename a symbol (variable, function, class, etc.) at the specified position and update all references throughout the codebase."),
		mcp.WithString("filePath",
			mcp.Required(),
			mcp.Description("The path to the file containing the symbol to rename"),
		),
		mcp.WithNumber("line",
			mcp.Required(),
			mcp.Description("The line number where the symbol is located (1-indexed)"),
		),
		mcp.WithNumber("column",
			mcp.Required(),
			mcp.Description("The column number where the symbol is located (1-indexed)"),
		),
		mcp.WithString("newName",
			mcp.Required(),
			mcp.Description("The new name for the symbol"),
		),
	)

	s.mcpServer.AddTool(renameSymbolTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filePath, ok := request.Params.Arguments["filePath"].(string)
		if !ok {
			return mcp.NewToolResultError("filePath must be a string"), nil
		}
		filePath = tools.ResolveFilePath(filePath)
		newName, ok := request.Params.Arguments["newName"].(string)
		if !ok {
			return mcp.NewToolResultError("newName must be a string"), nil
		}
		line := getInt(request.Params.Arguments, "line", 0)
		column := getInt(request.Params.Arguments, "column", 0)

		coreLogger.Debug("Executing rename_symbol for file: %s line: %d column: %d newName: %s", filePath, line, column, newName)
		client, err := s.router.ClientForFile(ctx, filePath)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		text, err := tools.RenameSymbol(ctx, client, filePath, line, column, newName)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to rename symbol: %v", err)), nil
		}
		return mcp.NewToolResultText(tools.TrimResponse(text)), nil
	})

	documentSymbolsTool := mcp.NewTool("document_symbols",
		mcp.WithDescription("List all symbols (functions, types, methods, constants, etc.) in a file with hierarchy. Useful for understanding file structure at a glance."),
		mcp.WithString("filePath",
			mcp.Required(),
			mcp.Description("Path to the file to list symbols for"),
		),
	)

	s.mcpServer.AddTool(documentSymbolsTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filePath, ok := request.Params.Arguments["filePath"].(string)
		if !ok {
			return mcp.NewToolResultError("filePath must be a string"), nil
		}
		filePath = tools.ResolveFilePath(filePath)

		coreLogger.Debug("Executing document_symbols for file: %s", filePath)
		client, err := s.router.ClientForFile(ctx, filePath)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		text, err := tools.GetDocumentSymbols(ctx, client, filePath)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to get document symbols: %v", err)), nil
		}
		return mcp.NewToolResultText(tools.TrimResponse(text)), nil
	})

	workspaceSymbolsTool := mcp.NewTool("workspace_symbols",
		mcp.WithDescription("Search for symbols across the entire project by name. Returns a concise listing of matching symbols with their locations, unlike 'definition' which returns full source code."),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("The symbol name or partial name to search for"),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of symbols to return. Default 20. Use -1 for all."),
		),
	)

	s.mcpServer.AddTool(workspaceSymbolsTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query, ok := request.Params.Arguments["query"].(string)
		if !ok {
			return mcp.NewToolResultError("query must be a string"), nil
		}
		limit := getInt(request.Params.Arguments, "limit", 20)

		coreLogger.Debug("Executing workspace_symbols for query: %s", query)
		clients := s.router.ActiveClients()
		if len(clients) == 0 {
			return mcp.NewToolResultError("no LSP servers are running; make a file-based request first to start a server"), nil
		}
		var lastErr error
		for _, client := range clients {
			text, err := tools.SearchWorkspaceSymbols(ctx, client, query, limit)
			if err == nil {
				return mcp.NewToolResultText(tools.TrimResponse(text)), nil
			}
			lastErr = err
		}
		return mcp.NewToolResultError(fmt.Sprintf("failed to search workspace symbols: %v", lastErr)), nil
	})

	formatFileTool := mcp.NewTool("format_file",
		mcp.WithDescription("Format a file using the language server's built-in formatter (e.g., gofmt for Go, rustfmt for Rust, clang-format for C/C++). Returns an error if the language server does not support formatting."),
		mcp.WithString("filePath",
			mcp.Required(),
			mcp.Description("The path to the file to format"),
		),
		mcp.WithNumber("tabSize",
			mcp.Description("Number of spaces per tab. Default 4."),
		),
		mcp.WithBoolean("insertSpaces",
			mcp.Description("Use spaces instead of tabs. Default true."),
		),
	)

	s.mcpServer.AddTool(formatFileTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filePath, ok := request.Params.Arguments["filePath"].(string)
		if !ok {
			return mcp.NewToolResultError("filePath must be a string"), nil
		}
		filePath = tools.ResolveFilePath(filePath)
		tabSize := getInt(request.Params.Arguments, "tabSize", 4)
		insertSpaces := true
		if v, ok := request.Params.Arguments["insertSpaces"].(bool); ok {
			insertSpaces = v
		}

		coreLogger.Debug("Executing format_file for file: %s", filePath)
		client, err := s.router.ClientForFile(ctx, filePath)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		text, err := tools.FormatFile(ctx, client, filePath, tabSize, insertSpaces)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to format file: %v", err)), nil
		}
		return mcp.NewToolResultText(tools.TrimResponse(text)), nil
	})

	implementationTool := mcp.NewTool("implementation",
		mcp.WithDescription("Find concrete implementations of an interface, abstract class, or trait. Unlike 'definition' which shows where a symbol is declared, this shows the actual code that implements it."),
		mcp.WithString("symbolName",
			mcp.Required(),
			mcp.Description("The name of the interface or abstract symbol to find implementations for (e.g. 'MyInterface', 'MyTrait')"),
		),
	)

	s.mcpServer.AddTool(implementationTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		symbolName, ok := request.Params.Arguments["symbolName"].(string)
		if !ok {
			return mcp.NewToolResultError("symbolName must be a string"), nil
		}

		coreLogger.Debug("Executing implementation for symbol: %s", symbolName)
		clients := s.router.ActiveClients()
		if len(clients) == 0 {
			return mcp.NewToolResultError("no LSP servers are running; make a file-based request first to start a server"), nil
		}
		var lastErr error
		for _, client := range clients {
			text, err := tools.FindImplementation(ctx, client, symbolName)
			if err == nil {
				return mcp.NewToolResultText(tools.TrimResponse(text)), nil
			}
			lastErr = err
		}
		return mcp.NewToolResultError(fmt.Sprintf("failed to find implementations: %v", lastErr)), nil
	})

	getImplementationTool := mcp.NewTool("get_implementation",
		mcp.WithDescription("Find concrete implementations of a symbol at the specified position. Useful for jumping from an interface method to its concrete implementations."),
		mcp.WithString("filePath",
			mcp.Required(),
			mcp.Description("The path to the file containing the symbol"),
		),
		mcp.WithNumber("line",
			mcp.Required(),
			mcp.Description("The line number (1-indexed)"),
		),
		mcp.WithNumber("column",
			mcp.Required(),
			mcp.Description("The column number (1-indexed)"),
		),
	)

	s.mcpServer.AddTool(getImplementationTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filePath, ok := request.Params.Arguments["filePath"].(string)
		if !ok {
			return mcp.NewToolResultError("filePath must be a string"), nil
		}
		filePath = tools.ResolveFilePath(filePath)
		line := getInt(request.Params.Arguments, "line", 0)
		column := getInt(request.Params.Arguments, "column", 0)

		coreLogger.Debug("Executing get_implementation for file: %s line: %d column: %d", filePath, line, column)
		client, err := s.router.ClientForFile(ctx, filePath)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		text, err := tools.FindImplementationAtPosition(ctx, client, filePath, line, column)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to find implementations: %v", err)), nil
		}
		return mcp.NewToolResultText(tools.TrimResponse(text)), nil
	})

	serverStatusTool := mcp.NewTool("server_status",
		mcp.WithDescription("Check the health and status of all active LSP servers. Shows which servers are running, their PIDs, open file counts, and diagnostic summaries."),
	)

	s.mcpServer.AddTool(serverStatusTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		coreLogger.Debug("Executing server_status")
		text := tools.ServerStatus(s.router)
		return mcp.NewToolResultText(tools.TrimResponse(text)), nil
	})

	incomingCallsTool := mcp.NewTool("incoming_calls",
		mcp.WithDescription("Find all callers of a function or method at the specified position. Shows which functions call the target, forming an upward view of the call hierarchy."),
		mcp.WithString("filePath",
			mcp.Required(),
			mcp.Description("The path to the file containing the function/method"),
		),
		mcp.WithNumber("line",
			mcp.Required(),
			mcp.Description("The line number (1-indexed)"),
		),
		mcp.WithNumber("column",
			mcp.Required(),
			mcp.Description("The column number (1-indexed)"),
		),
	)

	s.mcpServer.AddTool(incomingCallsTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filePath, ok := request.Params.Arguments["filePath"].(string)
		if !ok {
			return mcp.NewToolResultError("filePath must be a string"), nil
		}
		filePath = tools.ResolveFilePath(filePath)
		line := getInt(request.Params.Arguments, "line", 0)
		column := getInt(request.Params.Arguments, "column", 0)

		coreLogger.Debug("Executing incoming_calls for file: %s line: %d column: %d", filePath, line, column)
		client, err := s.router.ClientForFile(ctx, filePath)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		text, err := tools.GetIncomingCalls(ctx, client, filePath, line, column)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to get incoming calls: %v", err)), nil
		}
		return mcp.NewToolResultText(tools.TrimResponse(text)), nil
	})

	outgoingCallsTool := mcp.NewTool("outgoing_calls",
		mcp.WithDescription("Find all functions and methods called by the function at the specified position. Shows what the target calls, forming a downward view of the call hierarchy."),
		mcp.WithString("filePath",
			mcp.Required(),
			mcp.Description("The path to the file containing the function/method"),
		),
		mcp.WithNumber("line",
			mcp.Required(),
			mcp.Description("The line number (1-indexed)"),
		),
		mcp.WithNumber("column",
			mcp.Required(),
			mcp.Description("The column number (1-indexed)"),
		),
	)

	s.mcpServer.AddTool(outgoingCallsTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filePath, ok := request.Params.Arguments["filePath"].(string)
		if !ok {
			return mcp.NewToolResultError("filePath must be a string"), nil
		}
		filePath = tools.ResolveFilePath(filePath)
		line := getInt(request.Params.Arguments, "line", 0)
		column := getInt(request.Params.Arguments, "column", 0)

		coreLogger.Debug("Executing outgoing_calls for file: %s line: %d column: %d", filePath, line, column)
		client, err := s.router.ClientForFile(ctx, filePath)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		text, err := tools.GetOutgoingCalls(ctx, client, filePath, line, column)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to get outgoing calls: %v", err)), nil
		}
		return mcp.NewToolResultText(tools.TrimResponse(text)), nil
	})

	dependencyGraphTool := mcp.NewTool("dependency_graph",
		mcp.WithDescription("Generate a Mermaid dependency graph starting from a function or method. Uses the LSP call hierarchy to walk callers and/or callees recursively and returns a Mermaid diagram."),
		mcp.WithString("filePath",
			mcp.Required(),
			mcp.Description("The path to the file containing the function/method"),
		),
		mcp.WithNumber("line",
			mcp.Required(),
			mcp.Description("The line number (1-indexed)"),
		),
		mcp.WithNumber("column",
			mcp.Required(),
			mcp.Description("The column number (1-indexed)"),
		),
		mcp.WithNumber("depth",
			mcp.Description("How many levels deep to recurse. Default 3."),
		),
		mcp.WithString("direction",
			mcp.Description("Direction to traverse: \"outgoing\" (callees), \"incoming\" (callers), or \"both\". Default \"outgoing\"."),
		),
	)

	s.mcpServer.AddTool(dependencyGraphTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filePath, ok := request.Params.Arguments["filePath"].(string)
		if !ok {
			return mcp.NewToolResultError("filePath must be a string"), nil
		}
		filePath = tools.ResolveFilePath(filePath)
		line := getInt(request.Params.Arguments, "line", 0)
		column := getInt(request.Params.Arguments, "column", 0)
		depth := getInt(request.Params.Arguments, "depth", 3)
		direction := "outgoing"
		if v, ok := request.Params.Arguments["direction"].(string); ok && v != "" {
			direction = v
		}

		coreLogger.Debug("Executing dependency_graph for file: %s line: %d column: %d depth: %d direction: %s", filePath, line, column, depth, direction)
		client, err := s.router.ClientForFile(ctx, filePath)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		text, err := tools.GenerateCallGraph(ctx, client, filePath, line, column, depth, direction)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to generate dependency graph: %v", err)), nil
		}
		return mcp.NewToolResultText(tools.TrimResponse(text)), nil
	})

	coreLogger.Info("Successfully registered all MCP tools")
	return nil
}
