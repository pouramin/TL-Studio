package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNativeContextPlanKeepsConversationWhenWithinBudget(t *testing.T) {
	messages := []nativeConversationMessage{
		{Role: "user", Text: "Inspect the project."},
		{Role: "assistant", Text: "I inspected it."},
		{Role: "user", Text: "Now fix the tests."},
	}
	model := tlProviderModel{ID: "test", ContextLimit: 32768, OutputLimit: 4096}
	plan := buildNativeContextPlan(messages, "system", []nativeModelToolDefinition{nativeTestToolDefinition()}, model)

	if plan.CheckpointInserted || plan.OmittedMessages != 0 || plan.CompactedToolResults != 0 || plan.CompactedToolCalls != 0 {
		t.Fatalf("small conversation should remain untouched: %#v", plan)
	}
	if len(plan.Messages) != len(messages) {
		t.Fatalf("expected %d messages, got %d", len(messages), len(plan.Messages))
	}
	for i := range messages {
		if plan.Messages[i].Role != messages[i].Role || plan.Messages[i].Text != messages[i].Text {
			t.Fatalf("message %d changed unexpectedly: %#v", i, plan.Messages[i])
		}
	}
}

func TestNativeContextPlanWindowsOldTurnsAndKeepsLatestTurn(t *testing.T) {
	large := strings.Repeat("historical context ", 500)
	messages := []nativeConversationMessage{
		{Role: "user", Text: "old request one " + large},
		{Role: "assistant", Text: "old answer one " + large},
		{Role: "user", Text: "old request two " + large},
		{Role: "assistant", Text: "old answer two " + large},
		{Role: "user", Text: "LATEST USER REQUEST MUST SURVIVE"},
		{Role: "assistant", Text: "latest assistant state"},
	}
	model := tlProviderModel{ID: "small", ContextLimit: 4096, OutputLimit: 1024}
	plan := buildNativeContextPlan(messages, "system", nil, model)

	if !plan.CheckpointInserted || plan.OmittedMessages == 0 {
		t.Fatalf("expected old history to be windowed: %#v", plan)
	}
	joined := ""
	for _, message := range plan.Messages {
		joined += "\n" + message.Text
	}
	if !strings.Contains(joined, "[TL Studio context checkpoint]") {
		t.Fatalf("checkpoint marker missing: %#v", plan.Messages)
	}
	if !strings.Contains(joined, "LATEST USER REQUEST MUST SURVIVE") {
		t.Fatalf("latest user turn was dropped: %#v", plan.Messages)
	}
	if strings.Contains(joined, "old request one") {
		t.Fatalf("oldest turn should have been omitted: %#v", plan.Messages)
	}
	if plan.EstimatedMessageTokens > plan.MessageBudget {
		t.Fatalf("windowed context still exceeds message budget: %#v", plan)
	}
}

func TestNativeContextPlanCompactsOlderCompletedToolPayloadsAndPreservesNewest(t *testing.T) {
	hugeArguments, _ := json.Marshal(map[string]any{"content": strings.Repeat("A", 9000)})
	hugeResult, _ := json.Marshal(map[string]any{
		"ok":     true,
		"toolID": "files.read",
		"callID": "call-1",
		"output": map[string]any{
			"path": "big.txt", "startLine": 1, "endLine": 400, "totalLines": 800,
			"content": strings.Repeat("B", 9000),
		},
	})
	latestResult, _ := json.Marshal(map[string]any{
		"ok": true, "toolID": "files.read", "callID": "call-2",
		"output": map[string]any{"path": "latest.txt", "content": "LATEST_RESULT_MUST_SURVIVE"},
	})
	messages := []nativeConversationMessage{
		{Role: "user", Text: "Inspect the files."},
		{Role: "assistant", ToolCalls: []nativeModelToolCall{{
			ID: "call-1", Name: "files.read", Arguments: hugeArguments,
		}}},
		{Role: "tool", ToolCallID: "call-1", ToolName: "files.read", Text: string(hugeResult)},
		{Role: "assistant", ToolCalls: []nativeModelToolCall{{
			ID: "call-2", Name: "files.read", Arguments: json.RawMessage(`{"path":"latest.txt"}`),
		}}},
		{Role: "tool", ToolCallID: "call-2", ToolName: "files.read", Text: string(latestResult)},
	}
	model := tlProviderModel{ID: "small", ContextLimit: 4096, OutputLimit: 1024}
	plan := buildNativeContextPlan(messages, "system", nil, model)

	if plan.CompactedToolResults == 0 {
		t.Fatalf("expected older large tool output to be compacted: %#v", plan)
	}
	if plan.CompactedToolCalls == 0 {
		t.Fatalf("expected older completed large tool arguments to be compacted: %#v", plan)
	}
	var sawCompactedResult bool
	var sawCompactedMetadata bool
	var sawLatestVerbatim bool
	for _, message := range plan.Messages {
		if message.Role == "tool" && strings.Contains(message.Text, "\"compacted\":true") {
			sawCompactedResult = true
			if strings.Contains(message.Text, "\"path\":\"big.txt\"") &&
				strings.Contains(message.Text, "\"startLine\":1") &&
				strings.Contains(message.Text, "\"contentCompacted\":true") {
				sawCompactedMetadata = true
			}
		}
		if message.Role == "tool" && message.ToolCallID == "call-2" &&
			strings.Contains(message.Text, "LATEST_RESULT_MUST_SURVIVE") {
			sawLatestVerbatim = true
		}
	}
	if !sawCompactedResult || !sawCompactedMetadata || !sawLatestVerbatim {
		t.Fatalf("context compaction lost required state: %#v", plan.Messages)
	}
}

func TestNativeContextPlanCompactsBeforeHardLimit(t *testing.T) {
	result, _ := json.Marshal(map[string]any{
		"ok": true, "toolID": "files.read", "callID": "old-read",
		"output": map[string]any{"path": "old.txt", "content": strings.Repeat("x", 9000)},
	})
	messages := []nativeConversationMessage{
		{Role: "user", Text: strings.Repeat("history ", 600)},
		{Role: "assistant", ToolCalls: []nativeModelToolCall{{ID: "old-read", Name: "files.read", Arguments: json.RawMessage(`{"path":"old.txt"}`)}}},
		{Role: "tool", ToolCallID: "old-read", ToolName: "files.read", Text: string(result)},
		{Role: "assistant", Text: strings.Repeat("analysis ", 300)},
		{Role: "assistant", ToolCalls: []nativeModelToolCall{{ID: "latest", Name: "files.read", Arguments: json.RawMessage(`{"path":"latest.txt"}`)}}},
		{Role: "tool", ToolCallID: "latest", ToolName: "files.read", Text: `{"ok":true,"output":{"path":"latest.txt","content":"latest"}}`},
	}
	model := tlProviderModel{ID: "medium", ContextLimit: 12000, OutputLimit: 2000}
	plan := buildNativeContextPlan(messages, "system", nil, model)
	if plan.CompactedToolResults == 0 {
		t.Fatalf("expected proactive compaction above the soft budget: %#v", plan)
	}
	if plan.OverBudget {
		t.Fatalf("proactive compaction should keep this request under budget: %#v", plan)
	}
}

func TestNativeContextRunTokenBudgetScalesWithContextLimit(t *testing.T) {
	cases := []struct {
		model tlProviderModel
		want  int
	}{
		{model: tlProviderModel{ContextLimit: 32768}, want: nativeContextMinimumRunTokenBudget},
		{model: tlProviderModel{ContextLimit: 200000}, want: 800000},
		{model: tlProviderModel{ContextLimit: 1000000}, want: nativeContextMaximumRunTokenBudget},
	}
	for _, tc := range cases {
		if got := nativeContextRunTokenBudget(tc.model); got != tc.want {
			t.Fatalf("run token budget = %d, want %d for model %#v", got, tc.want, tc.model)
		}
	}
}

func TestNativeContextBudgetUsesAdvertisedModelLimits(t *testing.T) {
	model := tlProviderModel{ID: "bounded", ContextLimit: 16000, OutputLimit: 4000}
	contextLimit, budget, fixed := nativeContextMessageBudget("system prompt", []nativeModelToolDefinition{nativeTestToolDefinition()}, model)

	if contextLimit != 16000 {
		t.Fatalf("expected advertised context limit, got %d", contextLimit)
	}
	if budget <= 0 || budget >= contextLimit {
		t.Fatalf("invalid message budget %d for context limit %d", budget, contextLimit)
	}
	if fixed <= nativeContextSafetyReserve {
		t.Fatalf("fixed budget should include system/tools plus safety reserve, got %d", fixed)
	}
}

func TestNativeContextPlanFallsBackToConservativeDefaultLimit(t *testing.T) {
	plan := buildNativeContextPlan(
		[]nativeConversationMessage{{Role: "user", Text: "hello"}},
		"system",
		nil,
		tlProviderModel{ID: "unknown-limit"},
	)
	if plan.ContextLimit != nativeContextDefaultLimit {
		t.Fatalf("expected default context limit %d, got %d", nativeContextDefaultLimit, plan.ContextLimit)
	}
}

func TestNativeContextActivityReportsManagementMetadata(t *testing.T) {
	plan := nativeContextPlan{
		ContextLimit: 8192, MessageBudget: 4096, EstimatedMessageTokens: 3000,
		OmittedMessages: 6, OmittedTokens: 2200, CompactedToolResults: 2,
		CompactedToolCalls: 1, CheckpointInserted: true,
	}
	activity := nativeContextActivity(plan)
	if activity.Kind != "context" || activity.Status != "completed" {
		t.Fatalf("unexpected context activity %#v", activity)
	}
	if activity.Metadata["omittedMessages"] != 6 || activity.Metadata["compactedToolResults"] != 2 {
		t.Fatalf("context metadata missing: %#v", activity.Metadata)
	}
}
