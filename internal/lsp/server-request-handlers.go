package lsp

import (
	"encoding/json"
	"fmt"

	"github.com/angalato08/mcp-language-server/internal/protocol"
	"github.com/angalato08/mcp-language-server/internal/utilities"
)

// FileWatchHandler is called when file watchers are registered by the server
type FileWatchHandler func(id string, watchers []protocol.FileSystemWatcher)

// fileWatchHandler holds the current file watch handler
var fileWatchHandler FileWatchHandler

// RegisterFileWatchHandler registers a handler for file watcher registrations
func RegisterFileWatchHandler(handler FileWatchHandler) {
	fileWatchHandler = handler
}

// Requests

func HandleWorkspaceConfiguration(params json.RawMessage) (any, error) {
	return []map[string]any{{}}, nil
}

func HandleRegisterCapability(params json.RawMessage) (any, error) {
	var registerParams protocol.RegistrationParams
	if err := json.Unmarshal(params, &registerParams); err != nil {
		lspLogger.Error("Error unmarshaling registration params: %v", err)
		return nil, err
	}

	for _, reg := range registerParams.Registrations {
		lspLogger.Info("Registration received for method: %s, id: %s", reg.Method, reg.ID)

		// Special handling for file watcher registrations
		if reg.Method == "workspace/didChangeWatchedFiles" {
			// Parse the options into the appropriate type
			var opts protocol.DidChangeWatchedFilesRegistrationOptions
			optJson, err := json.Marshal(reg.RegisterOptions)
			if err != nil {
				lspLogger.Error("Error marshaling registration options: %v", err)
				continue
			}

			err = json.Unmarshal(optJson, &opts)
			if err != nil {
				lspLogger.Error("Error unmarshaling registration options: %v", err)
				continue
			}

			// Notify file watchers
			if fileWatchHandler != nil {
				fileWatchHandler(reg.ID, opts.Watchers)
			}
		}
	}

	return nil, nil
}

func HandleApplyEdit(params json.RawMessage) (any, error) {
	var workspaceEdit protocol.ApplyWorkspaceEditParams
	if err := json.Unmarshal(params, &workspaceEdit); err != nil {
		return protocol.ApplyWorkspaceEditResult{Applied: false}, err
	}

	// Apply the edits
	err := utilities.ApplyWorkspaceEdit(workspaceEdit.Edit)
	if err != nil {
		lspLogger.Error("Error applying workspace edit: %v", err)
		return protocol.ApplyWorkspaceEditResult{
			Applied:       false,
			FailureReason: workspaceEditFailure(err),
		}, nil
	}

	return protocol.ApplyWorkspaceEditResult{
		Applied: true,
	}, nil
}

func workspaceEditFailure(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// HandleWorkDoneProgressCreate handles the window/workDoneProgress/create request
// from the server, which asks us to create a progress token so the server can
// report progress via $/progress notifications.
func HandleWorkDoneProgressCreate(client *Client, params json.RawMessage) (any, error) {
	var createParams protocol.WorkDoneProgressCreateParams
	if err := json.Unmarshal(params, &createParams); err != nil {
		lspLogger.Error("Error unmarshaling workDoneProgress/create params: %v", err)
		return nil, err
	}

	token := progressTokenToString(createParams.Token)
	lspLogger.Debug("Progress token created: %s", token)

	client.progressMu.Lock()
	client.activeProgress[token] = &ProgressInfo{Token: token, Done: true}
	client.progressMu.Unlock()

	return nil, nil
}

// HandleProgress processes $/progress notifications which report
// begin/report/end for work-done progress (e.g. indexing).
func HandleProgress(client *Client, params json.RawMessage) {
	// $/progress has { token, value } where value is Begin/Report/End
	var raw struct {
		Token json.RawMessage `json:"token"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(params, &raw); err != nil {
		lspLogger.Error("Error unmarshaling $/progress params: %v", err)
		return
	}

	// Parse the token (can be string or number)
	var token string
	var strToken string
	if err := json.Unmarshal(raw.Token, &strToken); err == nil {
		token = strToken
	} else {
		// Try number
		var numToken float64
		if err := json.Unmarshal(raw.Token, &numToken); err == nil {
			token = fmt.Sprintf("%v", numToken)
		} else {
			token = string(raw.Token)
		}
	}

	// Determine kind
	var kind struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(raw.Value, &kind); err != nil {
		lspLogger.Error("Error parsing progress kind: %v", err)
		return
	}

	client.progressMu.Lock()
	defer client.progressMu.Unlock()

	info, exists := client.activeProgress[token]
	if !exists {
		// Token wasn't created via workDoneProgress/create — create it now
		// (some servers send $/progress without the create request)
		info = &ProgressInfo{Token: token, Done: true}
		client.activeProgress[token] = info
	}

	switch kind.Kind {
	case "begin":
		var begin protocol.WorkDoneProgressBegin
		if err := json.Unmarshal(raw.Value, &begin); err == nil {
			info.Title = begin.Title
			info.Message = begin.Message
			info.Percentage = begin.Percentage
			info.Done = false
			lspLogger.Info("Progress begin [%s]: %s — %s", token, begin.Title, begin.Message)
		}
	case "report":
		var report protocol.WorkDoneProgressReport
		if err := json.Unmarshal(raw.Value, &report); err == nil {
			info.Message = report.Message
			info.Percentage = report.Percentage
			lspLogger.Debug("Progress report [%s]: %s (%d%%)", token, report.Message, report.Percentage)
		}
	case "end":
		var end protocol.WorkDoneProgressEnd
		if err := json.Unmarshal(raw.Value, &end); err == nil {
			info.Done = true
			info.Message = end.Message
			lspLogger.Info("Progress end [%s]: %s", token, end.Message)
		}
	}
}

// progressTokenToString converts a ProgressToken (string | int32) to a string key.
func progressTokenToString(token protocol.ProgressToken) string {
	if s, ok := token.Value.(string); ok {
		return s
	}
	if n, ok := token.Value.(int32); ok {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%v", token.Value)
}

// Notifications

// HandleServerMessage processes window/showMessage notifications from the server
func HandleServerMessage(params json.RawMessage) {
	var msg protocol.ShowMessageParams
	if err := json.Unmarshal(params, &msg); err != nil {
		lspLogger.Error("Error unmarshaling server message: %v", err)
		return
	}

	// Log the message with appropriate level
	switch msg.Type {
	case protocol.Error:
		lspLogger.Error("Server error: %s", msg.Message)
	case protocol.Warning:
		lspLogger.Warn("Server warning: %s", msg.Message)
	case protocol.Info:
		lspLogger.Info("Server info: %s", msg.Message)
	default:
		lspLogger.Debug("Server message: %s", msg.Message)
	}
}

// HandleDiagnostics processes textDocument/publishDiagnostics notifications
func HandleDiagnostics(client *Client, params json.RawMessage) {
	var diagParams protocol.PublishDiagnosticsParams
	if err := json.Unmarshal(params, &diagParams); err != nil {
		lspLogger.Error("Error unmarshaling diagnostic params: %v", err)
		return
	}

	// Rotate previous snapshot, then save current diagnostics
	client.diagnosticsMu.Lock()
	client.previousDiagnostics[diagParams.URI] = client.diagnostics[diagParams.URI]
	client.diagnostics[diagParams.URI] = diagParams.Diagnostics
	client.diagnosticsMu.Unlock()

	// Signal that diagnostics are ready for this URI
	client.SignalDiagnostics(diagParams.URI)

	lspLogger.Info("Received diagnostics for %s: %d items", diagParams.URI, len(diagParams.Diagnostics))
}
