package tools

import (
	"fmt"
	"strings"

	"github.com/angalato08/mcp-language-server/internal/lsp"
)

// ServerStatus returns a health report for all configured and active LSP servers.
func ServerStatus(router *lsp.Router) string {
	clients := router.ActiveClients()

	if len(clients) == 0 {
		return "No LSP servers are currently running.\n\nServers start lazily when you first open a file of that language."
	}

	var result strings.Builder
	result.WriteString(fmt.Sprintf("Active LSP servers: %d\n\n", len(clients)))

	for _, client := range clients {
		cmd := client.Cmd
		result.WriteString("---\n")
		result.WriteString(fmt.Sprintf("Command: %s\n", cmd.Path))
		result.WriteString(fmt.Sprintf("Args: %s\n", strings.Join(cmd.Args[1:], " ")))

		if cmd.ProcessState != nil {
			// Process has exited
			result.WriteString(fmt.Sprintf("Status: DEAD (exit: %s)\n", cmd.ProcessState.String()))
		} else if cmd.Process != nil {
			result.WriteString(fmt.Sprintf("Status: RUNNING (pid: %d)\n", cmd.Process.Pid))
		} else {
			result.WriteString("Status: NOT STARTED\n")
		}

		// Open files
		openFiles := client.GetOpenFiles()
		result.WriteString(fmt.Sprintf("Open files: %d\n", len(openFiles)))

		// Diagnostics summary
		allDiags := client.GetAllDiagnostics()
		totalDiags := 0
		for _, diags := range allDiags {
			totalDiags += len(diags)
		}
		result.WriteString(fmt.Sprintf("Files with diagnostics: %d (%d total issues)\n", len(allDiags), totalDiags))
		result.WriteString("\n")
	}

	return result.String()
}
