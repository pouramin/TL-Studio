package main

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNativeToolContextProjectionPreservesSmallResults(t *testing.T) {
	result := nativeToolResult{
		ToolID:   "files.read",
		CallID:   "call-small",
		Output:   map[string]any{"content": "hello", "startLine": 1},
		Duration: 10,
	}
	text := nativeToolResultMessage(result)
	var payload map[string]any
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("small result was not valid JSON: %v", err)
	}
	if payload["outputTruncated"] != nil {
		t.Fatalf("small result was unexpectedly truncated: %s", text)
	}
	output, ok := payload["output"].(map[string]any)
	if !ok || output["content"] != "hello" {
		t.Fatalf("small result changed: %#v", payload["output"])
	}
}

func TestNativeToolContextProjectionBoundsLargeResultsWithoutMutatingActivity(t *testing.T) {
	content := strings.Repeat("line of terminal output\n", 30000)
	result := nativeToolResult{
		ToolID:   "terminal.command",
		CallID:   "call-large",
		Output:   map[string]any{"output": content, "exitCode": 0},
		Changes:  []sessionChangeView{{File: "main.go", Patch: strings.Repeat("diff", 20000), Additions: 2}},
		Duration: 120,
	}
	projected := nativeToolResultMessage(result)
	if len(projected) > nativeToolContextMaxBytes {
		t.Fatalf("model-facing result is %d bytes; maximum is %d", len(projected), nativeToolContextMaxBytes)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(projected), &payload); err != nil {
		t.Fatalf("bounded result is not JSON: %v", err)
	}
	if payload["outputTruncated"] != true {
		t.Fatalf("expected truncation metadata: %#v", payload)
	}
	if payload["callID"] != "call-large" || payload["toolID"] != "terminal.command" {
		t.Fatalf("tool identity lost: %#v", payload)
	}
	if payload["originalBytes"].(float64) <= float64(nativeToolContextMaxBytes) {
		t.Fatalf("original size missing: %#v", payload)
	}
	out := payload["output"].(map[string]any)
	if out["head"] == "" || out["tail"] == "" {
		t.Fatalf("head/tail previews missing: %#v", out)
	}
	if !strings.Contains(out["head"].(string), "line of terminal output") {
		t.Fatal("large diff displaced the model-facing tool output preview")
	}
	if result.Output.(map[string]any)["output"] != content {
		t.Fatal("projection modified the original output stored as the session activity")
	}
	if result.Changes[0].Patch != strings.Repeat("diff", 20000) {
		t.Fatal("projection modified the original diff")
	}
	if strings.Contains(projected, strings.Repeat("diff", 100)) {
		t.Fatal("large diff was embedded into model context")
	}
}

func TestNativeToolContextProjectionPreservesValidUnicode(t *testing.T) {
	content := strings.Repeat("سلام 🌍\n", 20000)
	result := nativeToolResult{ToolID: "files.read", CallID: "unicode", Output: content}
	projected := nativeToolResultMessage(result)
	if len(projected) > nativeToolContextMaxBytes {
		t.Fatalf("model-facing Unicode result is %d bytes", len(projected))
	}
	if !utf8.ValidString(projected) || !json.Valid([]byte(projected)) {
		t.Fatal("truncation produced invalid UTF-8 or JSON")
	}
}

func TestNativeToolContextProjectionBoundsMassiveError(t *testing.T) {
	result := nativeToolResult{ToolID: "mcp.large", CallID: "error", Error: strings.Repeat("provider error ", 5000)}
	projected := nativeToolResultMessage(result)
	if len(projected) > nativeToolContextMaxBytes || !json.Valid([]byte(projected)) {
		t.Fatal("error output exceeded budget or produced invalid JSON")
	}
	var payload map[string]any
	_ = json.Unmarshal([]byte(projected), &payload)
	if payload["outputTruncated"] != true || payload["ok"] != false {
		t.Fatalf("error and truncation flags lost: %#v", payload)
	}
}

func TestNativeToolContextProjectionNeverInventsFailureForOversizedMetadata(t *testing.T) {
	result := nativeToolResult{
		ToolID: "terminal.command",
		CallID: strings.Repeat("a", 100000),
		Output: strings.Repeat("done", 100000),
	}
	text := nativeToolResultMessage(result)
	if len(text) > nativeToolContextMaxBytes {
		t.Fatal("oversized metadata escaped the context bound")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if payload["ok"] != true {
		t.Fatal("model-context truncation must not turn a successful tool call into a failure")
	}
}
