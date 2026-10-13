package main

import (
	"context"
	"time"
)

// executeNativeToolRound owns the side-effect boundary for model tool calls.
// Its journal is written before execution and after each result; a resumed
// process must never blindly replay a call whose outcome may be unknown.
func (r *nativeAgentRuntime) executeNativeToolRound(
	ctx context.Context,
	directory, sessionID string,
	calls []nativeModelToolCall,
	tools []nativeModelToolDefinition,
	semantic *sessionMessageView,
	conversation *[]nativeConversationMessage,
) (observedWrite bool, err error) {
	for _, modelCall := range calls {
		descriptor, _ := r.tools.Descriptor(directory, modelCall.Name)
		decodedInput, _ := decodeNativeToolArguments(modelCall.Arguments)
		started := time.Now().UnixMilli()
		// Record pending tool intent before dispatch; its outcome may be unknown
		// if the process stops after the side effect but before recording success.
		semantic.Activities = append(semantic.Activities, sessionActivityView{
			Kind: "tool", Status: "running", Title: descriptor.Name,
			ToolID: descriptor.ID, RuntimeToolID: descriptor.ID,
			ToolName: descriptor.Name, Category: descriptor.Category,
			PermissionClass: descriptor.PermissionClass,
			Input:           decodedInput, StartAt: started,
		})
		if err := r.store.putNativeMessage(sessionID, directory, *semantic); err != nil {
			return false, err
		}
		r.publish(liveEventView{Type: "message.changed", Action: "content", SessionID: sessionID})
		allowed := nativeToolCallAllowed(tools, modelCall.Name)
		var result nativeToolResult
		if !allowed {
			result = nativeToolResult{
				ToolID: modelCall.Name, CallID: modelCall.ID,
				Error: "tool blocked by TL Studio: this tool is not available in the current turn; use advertised tools",
			}
		} else {
			result = r.tools.Execute(ctx, sessionID, directory, nativeToolCall{
				ID: modelCall.Name, CallID: modelCall.ID, Arguments: modelCall.Arguments,
			})
		}
		ended := time.Now().UnixMilli()

		activity := sessionActivityView{
			Kind:            "tool",
			Status:          "completed",
			Title:           descriptor.Name,
			ToolID:          descriptor.ID,
			RuntimeToolID:   descriptor.ID,
			ToolName:        descriptor.Name,
			Category:        descriptor.Category,
			PermissionClass: descriptor.PermissionClass,
			Input:           decodedInput,
			Output:          result.Output,
			Changes:         append([]sessionChangeView(nil), result.Changes...),
			StartAt:         started,
			EndAt:           ended,
			Elapsed:         ended - started,
		}
		if result.Error != "" {
			activity.Status = "failed"
			activity.Error = &sessionErrorView{Type: "tool", Message: result.Error}
		} else if descriptor.Capabilities.Write || descriptor.Capabilities.Execute {
			observedWrite = true
		}
		semantic.Activities[len(semantic.Activities)-1] = activity
		semantic.Changes = append(semantic.Changes, result.Changes...)
		// Persist each completed result before starting the next operation.
		if err := r.store.putNativeMessage(sessionID, directory, *semantic); err != nil {
			return false, err
		}
		for _, change := range result.Changes {
			r.publish(liveEventView{Type: "workspace.changed", Action: "changed", SessionID: sessionID, Path: change.File})
		}
		// Shell commands and MCP/plugin write-capable tools may mutate project files
		// without returning structured file-change metadata. Conservatively invalidate
		// the workspace after those calls so Explorer/Editor/Preview reconcile from disk.
		if allowed && (descriptor.ID == "terminal.command" || (descriptor.Source == "mcp" && descriptor.Capabilities.Write)) {
			r.publish(liveEventView{Type: "workspace.changed", Action: "changed", SessionID: sessionID})
		}
		*conversation = append(*conversation, nativeConversationMessage{
			Role:       "tool",
			ToolCallID: modelCall.ID,
			ToolName:   modelCall.Name,
			Text:       nativeToolResultMessage(result),
		})
	}

	return observedWrite, nil
}
