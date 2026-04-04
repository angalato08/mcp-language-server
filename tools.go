package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/angalato08/mcp-language-server/internal/protocol"
	"github.com/angalato08/mcp-language-server/internal/tools"
	"github.com/mark3labs/mcp-go/mcp"
)

func parseSeverity(s string) protocol.DiagnosticSeverity {
	switch strings.ToLower(s) {
	case "error":
		return protocol.SeverityError
	case "warning":
		return protocol.SeverityWarning
	case "info", "information":
		return protocol.SeverityInformation
	case "hint":
		return protocol.SeverityHint
	default:
		return 0
	}
}

func parseSymbolKind(s string) (protocol.SymbolKind, bool) {
	for kind, name := range protocol.TableKindMap {
		if strings.EqualFold(name, s) {
			return kind, true
		}
	}
	return 0, false
}

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
		mcp.WithBoolean("showDiagnostics",
			mcp.Description("If true, return diagnostics for the file after applying edits. Default true."),
			mcp.DefaultBool(true),
		),
		mcp.WithBoolean("showDiff",
			mcp.Description("If true and showDiagnostics is true, show only new/resolved diagnostics instead of full list."),
			mcp.DefaultBool(false),
		),
		mcp.WithString("outputFormat",
			mcp.Description("Output format: 'text' (default) for human-readable or 'json' for structured JSON."),
			mcp.Enum("text", "json"),
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

		showDiagnostics := true
		if v, ok := request.Params.Arguments["showDiagnostics"].(bool); ok {
			showDiagnostics = v
		}
		if showDiagnostics {
			showDiff := false
			if v, ok := request.Params.Arguments["showDiff"].(bool); ok {
				showDiff = v
			}
			outputFormat := "text"
			if v, ok := request.Params.Arguments["outputFormat"].(string); ok && v != "" {
				outputFormat = v
			}

			if outputFormat == "json" {
				var diagJSON string
				var diagErr error
				if showDiff {
					diagJSON, diagErr = tools.GetDiagnosticDiffForFileJSON(ctx, client, filePath, 10, tools.DiagnosticFilter{})
				} else {
					diagJSON, diagErr = tools.GetDiagnosticsForFileJSON(ctx, client, filePath, 10, tools.DiagnosticFilter{})
				}
				if diagErr == nil {
					response += "\n\n---\n" + diagJSON
				} else {
					coreLogger.Warn("Failed to fetch post-edit diagnostics: %v", diagErr)
				}
				return mcp.NewToolResultText(response), nil
			}

			var diagText string
			var diagErr error
			if showDiff {
				diagText, diagErr = tools.GetDiagnosticDiffForFile(ctx, client, filePath, 3, true, 10, tools.DiagnosticFilter{})
			} else {
				diagText, diagErr = tools.GetDiagnosticsForFile(ctx, client, filePath, 3, true, 10, tools.DiagnosticFilter{})
			}
			if diagErr == nil {
				response += "\n\n---\n" + diagText
			} else {
				coreLogger.Warn("Failed to fetch post-edit diagnostics: %v", diagErr)
			}
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
		mcp.WithNumber("offset",
			mcp.Description("Number of references to skip before returning results. Use with limit for pagination. Default 0."),
		),
		mcp.WithString("outputFormat",
			mcp.Description("Output format: 'full' (default) with code context, or 'short' for a compact file:line summary."),
			mcp.Enum("full", "short"),
		),
	)

	s.mcpServer.AddTool(findReferencesTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		symbolName, ok := request.Params.Arguments["symbolName"].(string)
		if !ok {
			return mcp.NewToolResultError("symbolName must be a string"), nil
		}
		limit := getInt(request.Params.Arguments, "limit", 30)
		offset := getInt(request.Params.Arguments, "offset", 0)
		outputFormat := "full"
		if v, ok := request.Params.Arguments["outputFormat"].(string); ok && v != "" {
			outputFormat = v
		}

		coreLogger.Debug("Executing references for symbol: %s", symbolName)
		clients := s.router.ActiveClients()
		if len(clients) == 0 {
			return mcp.NewToolResultError("no LSP servers are running; make a file-based request first to start a server"), nil
		}
		var lastErr error
		for _, client := range clients {
			text, err := tools.FindReferences(ctx, client, symbolName, limit, offset, outputFormat)
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
		mcp.WithNumber("offset",
			mcp.Description("Number of references to skip before returning results. Use with limit for pagination. Default 0."),
		),
		mcp.WithString("outputFormat",
			mcp.Description("Output format: 'full' (default) with code context, or 'short' for a compact file:line summary."),
			mcp.Enum("full", "short"),
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
		offset := getInt(request.Params.Arguments, "offset", 0)
		outputFormat := "full"
		if v, ok := request.Params.Arguments["outputFormat"].(string); ok && v != "" {
			outputFormat = v
		}

		coreLogger.Debug("Executing get_references for file: %s line: %d column: %d", filePath, line, column)
		client, err := s.router.ClientForFile(ctx, filePath)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		text, err := tools.FindReferencesAtPosition(ctx, client, filePath, line, column, limit, offset, outputFormat)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to find references: %v", err)), nil
		}
		return mcp.NewToolResultText(tools.TrimResponse(text)), nil
	})

	getDiagnosticsTool := mcp.NewTool("diagnostics",
		mcp.WithDescription("Get diagnostic information for files from the language server. Provide exactly one of filePath, files, or directory."),
		mcp.WithString("filePath",
			mcp.Description("Path to a single file to get diagnostics for."),
		),
		mcp.WithArray("files",
			mcp.Description("Array of file paths to scan for diagnostics."),
			mcp.Items(map[string]any{"type": "string"}),
		),
		mcp.WithString("directory",
			mcp.Description("Directory to scan recursively for source files with diagnostics."),
		),
		mcp.WithNumber("contextLines",
			mcp.Description("Lines to include around each diagnostic. Default 5 for single file, 0 for batch."),
		),
		mcp.WithBoolean("showLineNumbers",
			mcp.Description("If true, adds line numbers to the output"),
			mcp.DefaultBool(true),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of diagnostics to return. Default 20. Use -1 for all."),
		),
		mcp.WithArray("exclude",
			mcp.Description("Exclude diagnostics whose code contains any of these substrings"),
			mcp.Items(map[string]any{"type": "string"}),
		),
		mcp.WithString("severity",
			mcp.Description("Minimum severity to include"),
			mcp.Enum("error", "warning", "info", "hint"),
		),
		mcp.WithBoolean("showDiff",
			mcp.Description("If true, show only new and resolved diagnostics since the last update."),
			mcp.DefaultBool(false),
		),
		mcp.WithString("outputFormat",
			mcp.Description("Output format: 'text' (default) for human-readable or 'json' for structured JSON."),
			mcp.Enum("text", "json"),
		),
	)

	s.mcpServer.AddTool(getDiagnosticsTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Parse the three mutually exclusive input modes
		filePathStr, hasFilePath := request.Params.Arguments["filePath"].(string)
		filesArg, hasFiles := request.Params.Arguments["files"]
		directoryStr, hasDirectory := request.Params.Arguments["directory"].(string)

		// Validate: check which modes are actually provided
		var filesArr []any
		if hasFiles {
			filesArr, hasFiles = filesArg.([]any)
		}

		modeCount := 0
		if hasFilePath && filePathStr != "" {
			modeCount++
		}
		if hasFiles {
			modeCount++
		}
		if hasDirectory && directoryStr != "" {
			modeCount++
		}
		if modeCount == 0 {
			return mcp.NewToolResultError("One of filePath, files, or directory is required"), nil
		}
		if modeCount > 1 {
			return mcp.NewToolResultError("Specify exactly one of filePath, files, or directory"), nil
		}

		// Determine if this is batch mode (files or directory)
		isBatch := !hasFilePath || filePathStr == ""

		// Default contextLines: 5 for single file, 0 for batch
		defaultContextLines := 5
		if isBatch {
			defaultContextLines = 0
		}
		contextLines := getInt(request.Params.Arguments, "contextLines", defaultContextLines)

		showLineNumbers := true
		if v, ok := request.Params.Arguments["showLineNumbers"].(bool); ok {
			showLineNumbers = v
		}
		limit := getInt(request.Params.Arguments, "limit", 20)
		showDiff := false
		if v, ok := request.Params.Arguments["showDiff"].(bool); ok {
			showDiff = v
		}

		// Build diagnostic filter
		var filter tools.DiagnosticFilter
		if excludeArg, ok := request.Params.Arguments["exclude"]; ok {
			if excludeArr, ok := excludeArg.([]any); ok {
				for _, item := range excludeArr {
					if s, ok := item.(string); ok {
						filter.Exclude = append(filter.Exclude, s)
					}
				}
			}
		}
		if sevStr, ok := request.Params.Arguments["severity"].(string); ok {
			filter.MinSeverity = parseSeverity(sevStr)
		}
		outputFormat := "text"
		if v, ok := request.Params.Arguments["outputFormat"].(string); ok && v != "" {
			outputFormat = v
		}

		// JSON output mode
		if outputFormat == "json" {
			if hasFilePath && filePathStr != "" {
				filePath := tools.ResolveFilePath(filePathStr)
				coreLogger.Debug("Executing diagnostics (JSON) for file: %s (showDiff=%v)", filePath, showDiff)
				client, err := s.router.ClientForFile(ctx, filePath)
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				var jsonStr string
				if showDiff {
					jsonStr, err = tools.GetDiagnosticDiffForFileJSON(ctx, client, filePath, limit, filter)
				} else {
					jsonStr, err = tools.GetDiagnosticsForFileJSON(ctx, client, filePath, limit, filter)
				}
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("failed to get diagnostics: %v", err)), nil
				}
				return mcp.NewToolResultText(jsonStr), nil
			}

			// Batch mode: collect file paths
			var filePaths []string
			if hasFiles {
				if len(filesArr) == 0 {
					return mcp.NewToolResultError("No files specified for batch diagnostics"), nil
				}
				for _, item := range filesArr {
					if fp, ok := item.(string); ok {
						filePaths = append(filePaths, tools.ResolveFilePath(fp))
					}
				}
			} else {
				dirPath := tools.ResolveFilePath(directoryStr)
				var err error
				filePaths, err = tools.CollectFilesFromDirectory(dirPath)
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("failed to scan directory: %v", err)), nil
				}
				if len(filePaths) == 0 {
					return mcp.NewToolResultText(fmt.Sprintf("No supported source files found in %s", directoryStr)), nil
				}
			}

			coreLogger.Debug("Executing batch diagnostics (JSON) for %d files (showDiff=%v)", len(filePaths), showDiff)
			jsonStr, err := tools.GetDiagnosticsForFilesJSON(ctx, s.router, filePaths, limit, showDiff, filter)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("failed to get batch diagnostics: %v", err)), nil
			}
			return mcp.NewToolResultText(jsonStr), nil
		}

		// Single file mode (original behavior)
		if hasFilePath && filePathStr != "" {
			filePath := tools.ResolveFilePath(filePathStr)
			coreLogger.Debug("Executing diagnostics for file: %s (showDiff=%v)", filePath, showDiff)
			client, err := s.router.ClientForFile(ctx, filePath)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			var text string
			if showDiff {
				text, err = tools.GetDiagnosticDiffForFile(ctx, client, filePath, contextLines, showLineNumbers, limit, filter)
			} else {
				text, err = tools.GetDiagnosticsForFile(ctx, client, filePath, contextLines, showLineNumbers, limit, filter)
			}
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("failed to get diagnostics: %v", err)), nil
			}
			return mcp.NewToolResultText(tools.TrimResponse(text)), nil
		}

		// Batch mode: files array
		if hasFiles {
			if len(filesArr) == 0 {
				return mcp.NewToolResultError("No files specified for batch diagnostics"), nil
			}
			var filePaths []string
			for _, item := range filesArr {
				if fp, ok := item.(string); ok {
					filePaths = append(filePaths, tools.ResolveFilePath(fp))
				}
			}
			coreLogger.Debug("Executing batch diagnostics for %d files (showDiff=%v)", len(filePaths), showDiff)
			text, err := tools.GetDiagnosticsForFiles(ctx, s.router, filePaths, contextLines, showLineNumbers, limit, showDiff, filter)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("failed to get batch diagnostics: %v", err)), nil
			}
			return mcp.NewToolResultText(tools.TrimResponse(text)), nil
		}

		// Batch mode: directory
		dirPath := tools.ResolveFilePath(directoryStr)
		coreLogger.Debug("Executing directory diagnostics for: %s (showDiff=%v)", dirPath, showDiff)
		filePaths, err := tools.CollectFilesFromDirectory(dirPath)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to scan directory: %v", err)), nil
		}
		if len(filePaths) == 0 {
			return mcp.NewToolResultText(fmt.Sprintf("No supported source files found in %s", directoryStr)), nil
		}
		text, err := tools.GetDiagnosticsForFiles(ctx, s.router, filePaths, contextLines, showLineNumbers, limit, showDiff, filter)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to get directory diagnostics: %v", err)), nil
		}
		return mcp.NewToolResultText(tools.TrimResponse(text)), nil
	})

	workspaceDiagnosticsTool := mcp.NewTool("workspace_diagnostics",
		mcp.WithDescription("Get diagnostic information for all active files in the workspace."),
		mcp.WithNumber("contextLines",
			mcp.Description("Lines of source code context to include around each diagnostic. Default 0 (no context)."),
			mcp.DefaultNumber(0),
		),
		mcp.WithBoolean("showLineNumbers",
			mcp.Description("If true, adds line numbers to context output. Default true."),
			mcp.DefaultBool(true),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of diagnostics per file. Default 20. Use -1 for all."),
		),
		mcp.WithArray("exclude",
			mcp.Description("Exclude diagnostics whose code contains any of these substrings"),
			mcp.Items(map[string]any{"type": "string"}),
		),
		mcp.WithString("severity",
			mcp.Description("Minimum severity to include"),
			mcp.Enum("error", "warning", "info", "hint"),
		),
		mcp.WithBoolean("showDiff",
			mcp.Description("If true, show only new and resolved diagnostics since the last update."),
			mcp.DefaultBool(false),
		),
		mcp.WithString("outputFormat",
			mcp.Description("Output format: 'text' (default) for human-readable or 'json' for structured JSON."),
			mcp.Enum("text", "json"),
		),
	)

	s.mcpServer.AddTool(workspaceDiagnosticsTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		contextLines := getInt(request.Params.Arguments, "contextLines", 0)
		showLineNumbers := true
		if v, ok := request.Params.Arguments["showLineNumbers"].(bool); ok {
			showLineNumbers = v
		}
		limit := getInt(request.Params.Arguments, "limit", 20)

		// Build diagnostic filter
		var filter tools.DiagnosticFilter
		if excludeArg, ok := request.Params.Arguments["exclude"]; ok {
			if excludeArr, ok := excludeArg.([]any); ok {
				for _, item := range excludeArr {
					if s, ok := item.(string); ok {
						filter.Exclude = append(filter.Exclude, s)
					}
				}
			}
		}
		if sevStr, ok := request.Params.Arguments["severity"].(string); ok {
			filter.MinSeverity = parseSeverity(sevStr)
		}
		showDiff := false
		if v, ok := request.Params.Arguments["showDiff"].(bool); ok {
			showDiff = v
		}

		outputFormat := "text"
		if v, ok := request.Params.Arguments["outputFormat"].(string); ok && v != "" {
			outputFormat = v
		}

		coreLogger.Debug("Executing workspace_diagnostics (showDiff=%v, contextLines=%d, outputFormat=%s)", showDiff, contextLines, outputFormat)
		clients := s.router.ActiveClients()
		if len(clients) == 0 {
			return mcp.NewToolResultError("no LSP servers are running; make a file-based request first to start a server"), nil
		}

		if outputFormat == "json" {
			var jsonStr string
			var err error
			if showDiff {
				jsonStr, err = tools.GetAllDiagnosticDiffsJSON(ctx, clients, limit, filter)
			} else {
				jsonStr, err = tools.GetAllDiagnosticsJSON(ctx, clients, limit, filter)
			}
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("failed to get workspace diagnostics: %v", err)), nil
			}
			return mcp.NewToolResultText(jsonStr), nil
		}

		var text string
		var err error
		if showDiff {
			text, err = tools.GetAllDiagnosticDiffs(ctx, clients, contextLines, showLineNumbers, limit, filter)
		} else {
			text, err = tools.GetAllDiagnostics(ctx, clients, contextLines, showLineNumbers, limit, filter)
		}
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

	batchHoverTool := mcp.NewTool("batch_hover",
		mcp.WithDescription("Get hover information (types, documentation) for multiple positions in a single file. More efficient than calling hover repeatedly."),
		mcp.WithString("filePath", mcp.Required(), mcp.Description("The path to the file")),
		mcp.WithArray("positions", mcp.Required(),
			mcp.Description("Array of positions to hover on"),
			mcp.Items(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"line":   map[string]any{"type": "number", "description": "Line number (1-indexed)"},
					"column": map[string]any{"type": "number", "description": "Column number (1-indexed)"},
				},
				"required": []string{"line", "column"},
			}),
		),
	)

	s.mcpServer.AddTool(batchHoverTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filePath, ok := request.Params.Arguments["filePath"].(string)
		if !ok {
			return mcp.NewToolResultError("filePath must be a string"), nil
		}
		filePath = tools.ResolveFilePath(filePath)

		positionsRaw, ok := request.Params.Arguments["positions"].([]interface{})
		if !ok {
			return mcp.NewToolResultError("positions must be an array"), nil
		}

		if len(positionsRaw) == 0 {
			return mcp.NewToolResultText("No positions provided"), nil
		}

		positions := make([]tools.HoverPosition, 0, len(positionsRaw))
		for _, p := range positionsRaw {
			posMap, ok := p.(map[string]interface{})
			if !ok {
				return mcp.NewToolResultError("each position must be an object with line and column"), nil
			}
			line := getInt(posMap, "line", 0)
			column := getInt(posMap, "column", 0)
			if line <= 0 || column <= 0 {
				return mcp.NewToolResultError("line and column must be positive integers"), nil
			}
			positions = append(positions, tools.HoverPosition{Line: line, Column: column})
		}

		coreLogger.Debug("Executing batch_hover for file: %s with %d positions", filePath, len(positions))
		client, err := s.router.ClientForFile(ctx, filePath)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		results, err := tools.BatchHoverInfo(ctx, client, filePath, positions)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to get batch hover information: %v", err)), nil
		}

		text := tools.FormatBatchHoverResults(filePath, results)
		return mcp.NewToolResultText(tools.TrimResponse(text)), nil
	})

	apiOverviewTool := mcp.NewTool("api_overview",
		mcp.WithDescription("Get signatures and documentation for all symbols in a file. Combines document_symbols with hover to provide a complete API surface view in one call."),
		mcp.WithString("filePath", mcp.Required(), mcp.Description("Path to the file")),
		mcp.WithArray("symbolKinds",
			mcp.Description("Filter to specific symbol kinds (e.g. 'Function', 'Method', 'Class'). Default: all top-level symbols."),
			mcp.Items(map[string]any{"type": "string"}),
		),
		mcp.WithBoolean("includeChildren",
			mcp.Description("Include children of top-level symbols (methods, fields). Default true."),
			mcp.DefaultBool(true),
		),
	)

	s.mcpServer.AddTool(apiOverviewTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filePath, ok := request.Params.Arguments["filePath"].(string)
		if !ok {
			return mcp.NewToolResultError("filePath must be a string"), nil
		}
		filePath = tools.ResolveFilePath(filePath)

		var symbolKinds []protocol.SymbolKind
		if kindsRaw, ok := request.Params.Arguments["symbolKinds"].([]interface{}); ok {
			for _, k := range kindsRaw {
				kindStr, ok := k.(string)
				if !ok {
					continue
				}
				kind, valid := parseSymbolKind(kindStr)
				if valid {
					symbolKinds = append(symbolKinds, kind)
				}
			}
		}

		includeChildren := true
		if v, ok := request.Params.Arguments["includeChildren"].(bool); ok {
			includeChildren = v
		}

		coreLogger.Debug("Executing api_overview for file: %s", filePath)
		client, err := s.router.ClientForFile(ctx, filePath)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		text, err := tools.GetAPIOverview(ctx, client, filePath, symbolKinds, includeChildren)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to get API overview: %v", err)), nil
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
		mcp.WithNumber("offset",
			mcp.Description("Number of symbols to skip before returning results. Use with limit for pagination. Default 0."),
		),
	)

	s.mcpServer.AddTool(workspaceSymbolsTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query, ok := request.Params.Arguments["query"].(string)
		if !ok {
			return mcp.NewToolResultError("query must be a string"), nil
		}
		limit := getInt(request.Params.Arguments, "limit", 20)
		offset := getInt(request.Params.Arguments, "offset", 0)

		coreLogger.Debug("Executing workspace_symbols for query: %s", query)
		clients := s.router.ActiveClients()
		if len(clients) == 0 {
			return mcp.NewToolResultError("no LSP servers are running; make a file-based request first to start a server"), nil
		}
		var lastErr error
		for _, client := range clients {
			text, err := tools.SearchWorkspaceSymbols(ctx, client, query, limit, offset)
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

	applyFixTool := mcp.NewTool("apply_fix",
		mcp.WithDescription("Apply a quick-fix code action for a diagnostic at the specified position. Use diagnostics tool first to find fixable diagnostics, then apply the fix."),
		mcp.WithString("filePath", mcp.Required(),
			mcp.Description("Path to the file containing the diagnostic to fix")),
		mcp.WithNumber("line", mcp.Required(),
			mcp.Description("The line number of the diagnostic (1-indexed)")),
		mcp.WithNumber("column", mcp.Required(),
			mcp.Description("The column number of the diagnostic (1-indexed)")),
		mcp.WithString("diagnosticCode",
			mcp.Description("Optional diagnostic code to disambiguate multiple diagnostics at the same position")),
		mcp.WithBoolean("applyAll",
			mcp.Description("If true, apply all preferred quick-fixes for the entire file. When true, line and column are ignored."),
			mcp.DefaultBool(false)),
	)

	s.mcpServer.AddTool(applyFixTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filePath, ok := request.Params.Arguments["filePath"].(string)
		if !ok {
			return mcp.NewToolResultError("filePath must be a string"), nil
		}
		filePath = tools.ResolveFilePath(filePath)

		applyAll := false
		if v, ok := request.Params.Arguments["applyAll"].(bool); ok {
			applyAll = v
		}

		client, err := s.router.ClientForFile(ctx, filePath)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		if applyAll {
			coreLogger.Debug("Executing apply_fix (applyAll) for file: %s", filePath)
			text, err := tools.ApplyAllFixes(ctx, client, filePath)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("failed to apply fixes: %v", err)), nil
			}
			return mcp.NewToolResultText(tools.TrimResponse(text)), nil
		}

		line := getInt(request.Params.Arguments, "line", 0)
		column := getInt(request.Params.Arguments, "column", 0)
		diagnosticCode, _ := request.Params.Arguments["diagnosticCode"].(string)

		coreLogger.Debug("Executing apply_fix for file: %s line: %d column: %d", filePath, line, column)
		text, err := tools.ApplyFix(ctx, client, filePath, line, column, diagnosticCode)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to apply fix: %v", err)), nil
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
		mcp.WithString("outputFormat",
			mcp.Description("Output format: 'full' (default) with detailed info, or 'short' for a compact one-line-per-caller summary."),
			mcp.Enum("full", "short"),
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
		outputFormat := "full"
		if v, ok := request.Params.Arguments["outputFormat"].(string); ok && v != "" {
			outputFormat = v
		}

		coreLogger.Debug("Executing incoming_calls for file: %s line: %d column: %d", filePath, line, column)
		client, err := s.router.ClientForFile(ctx, filePath)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		text, err := tools.GetIncomingCalls(ctx, client, filePath, line, column, outputFormat)
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
		mcp.WithString("outputFormat",
			mcp.Description("Output format: 'full' (default) with detailed info, or 'short' for a compact one-line-per-callee summary."),
			mcp.Enum("full", "short"),
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
		outputFormat := "full"
		if v, ok := request.Params.Arguments["outputFormat"].(string); ok && v != "" {
			outputFormat = v
		}

		coreLogger.Debug("Executing outgoing_calls for file: %s line: %d column: %d", filePath, line, column)
		client, err := s.router.ClientForFile(ctx, filePath)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		text, err := tools.GetOutgoingCalls(ctx, client, filePath, line, column, outputFormat)
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
