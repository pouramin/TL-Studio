package main

import (
	"encoding/json"
	"unicode/utf8"
)

// nativeToolContextMaxBytes bounds a single tool result sent back to the model.
// The complete tool output is still recorded as a session activity by the run loop.
const (
	nativeToolContextMaxBytes   = 32 * 1024
	nativeToolContextHeadBytes  = 12 * 1024
	nativeToolContextTailBytes  = 4 * 1024
	nativeToolContextMaxChanges = 16
)

// nativeBoundedToolResultMessage is a model-context projection, not a
// persistence or UI truncation policy. Large command outputs and file reads
// should not be replayed in full on every subsequent model request.
func nativeBoundedToolResultMessage(original []byte, result nativeToolResult) string {
	if len(original) <= nativeToolContextMaxBytes {
		return string(original)
	}

	// Preview the actual tool output, not the serialized activity envelope.
	// Otherwise a large diff in Changes could crowd out the useful output.
	previewSource := original
	if encodedOutput, err := json.Marshal(result.Output); err == nil {
		previewSource = encodedOutput
	}

	changes := make([]map[string]any, 0, min(len(result.Changes), nativeToolContextMaxChanges))
	for _, change := range result.Changes[:min(len(result.Changes), nativeToolContextMaxChanges)] {
		changes = append(changes, map[string]any{
			"file":      change.File,
			"additions": change.Additions,
			"deletions": change.Deletions,
		})
	}

	projected := map[string]any{
		"ok":              result.Error == "",
		"toolID":          result.ToolID,
		"callID":          result.CallID,
		"durationMs":      result.Duration,
		"outputTruncated": true,
		"originalBytes":   len(original),
		"changes":         changes,
		"omittedChanges":  len(result.Changes) - len(changes),
		"output": map[string]any{
			"head": nativeToolPreviewHead(previewSource, nativeToolContextHeadBytes),
			"tail": nativeToolPreviewTail(previewSource, nativeToolContextTailBytes),
			"note": "Model-context output was truncated; the complete result remains in the session activity. For files.read, request narrower startLine/endLine ranges. For terminal.command, use a targeted follow-up command.",
		},
	}
	if result.Error != "" {
		projected["error"] = nativeToolPreviewHead([]byte(result.Error), 1024)
	}
	encoded, err := json.Marshal(projected)
	if err != nil || len(encoded) > nativeToolContextMaxBytes {
		// Even untrusted tool metadata cannot make the model context unbounded.
		if result.Error != "" {
			return "{\"ok\":false,\"outputTruncated\":true,\"message\":\"tool result metadata exceeded the model-context budget\"}"
		}
		return "{\"ok\":true,\"outputTruncated\":true,\"message\":\"tool result metadata exceeded the model-context budget\"}"
	}
	return string(encoded)
}

func nativeToolPreviewHead(data []byte, limit int) string {
	if len(data) > limit {
		data = data[:limit]
	}
	for len(data) > 0 && !utf8.Valid(data) {
		data = data[:len(data)-1]
	}
	return string(data)
}

func nativeToolPreviewTail(data []byte, limit int) string {
	if len(data) > limit {
		data = data[len(data)-limit:]
	}
	for len(data) > 0 && !utf8.Valid(data) {
		data = data[1:]
	}
	return string(data)
}
