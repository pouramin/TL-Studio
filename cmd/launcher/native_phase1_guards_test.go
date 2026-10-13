package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPhase1ToolCallIDNormalization(t *testing.T) {
	calls := []nativeModelToolCall{
		{Name: "files.read"},
		{Name: "files.read", ID: "provider-id"},
		{Name: "files.read", ID: "provider-id"},
		{Name: "files.read", ID: "native-call-2-1"},
	}
	nativeNormalizeTurnToolCallIDs(calls, 2)
	seen := map[string]bool{}
	for _, call := range calls {
		if call.ID == "" || seen[call.ID] {
			t.Fatalf("duplicate or missing call ID %q", call.ID)
		}
		seen[call.ID] = true
	}
	if calls[1].ID != "provider-id" {
		t.Fatal("valid provider ID was not preserved")
	}
}

func TestPhase1ToolRoundRepeatGuardNoFalsePositive(t *testing.T) {
	a := nativeModelToolCall{Name: "files.read", Arguments: json.RawMessage("{\"path\":\"a.txt\"}")}
	b := nativeModelToolCall{Name: "files.read", Arguments: json.RawMessage("{\"path\":\"b.txt\"}")}
	prev := map[string]int{}
	for i := 0; i < 20; i++ {
		call := a
		if i%2 == 1 {
			call = b
		}
		var err error
		prev, err = nativeNextToolRepeatStreaks(prev, []nativeModelToolCall{call}, 4)
		if err != nil {
			t.Fatalf("nonconsecutive repeat blocked round %d: %v", i, err)
		}
	}
}

func TestPhase1ToolRoundRepeatGuardPreflightsWholeRound(t *testing.T) {
	call := nativeModelToolCall{Name: "files.read", Arguments: json.RawMessage("{\"path\":\"a.txt\"}")}
	prev := map[string]int{}
	for i := 0; i < 4; i++ {
		var err error
		prev, err = nativeNextToolRepeatStreaks(prev, []nativeModelToolCall{call}, 4)
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err := nativeNextToolRepeatStreaks(prev, []nativeModelToolCall{
		{Name: "files.read", Arguments: json.RawMessage("{\"path\":\"other.txt\"}")},
		call,
	}, 4)
	if err == nil || !strings.Contains(err.Error(), "consecutive times") {
		t.Fatalf("expected preflight guard: %v", err)
	}
}

func TestPhase1ToolAllowlist(t *testing.T) {
	tools := []nativeModelToolDefinition{{ID: "files.read"}, {ID: "search.content"}}
	if !nativeToolCallAllowed(tools, "files.read") {
		t.Fatal("read tool denied")
	}
	if nativeToolCallAllowed(tools, "files.write") {
		t.Fatal("write tool allowed")
	}
}
