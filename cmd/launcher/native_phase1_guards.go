package main

import (
	"fmt"
	"strings"
)

// IDs must be stable across assistant declarations and tool-result references.
// Provider-specific wire ID requirements belong in Provider Adapters.
func nativeNormalizeTurnToolCallIDs(calls []nativeModelToolCall, iteration int) {
	seen := make(map[string]struct{}, len(calls))
	for i := range calls {
		id := strings.TrimSpace(calls[i].ID)
		if id == "" {
			id = fmt.Sprintf("native-call-%d-%d", iteration, i+1)
		}
		if _, exists := seen[id]; exists {
			id = fmt.Sprintf("native-call-%d-%d", iteration, i+1)
		}
		for suffix := 1; ; suffix++ {
			if _, exists := seen[id]; !exists {
				break
			}
			id = fmt.Sprintf("native-call-%d-%d-%d", iteration, i+1, suffix)
		}
		calls[i].ID = id
		seen[id] = struct{}{}
	}
}

// Only consecutive tool rounds count; unrelated intervening rounds reset streaks.
func nativeNextToolRepeatStreaks(previous map[string]int, calls []nativeModelToolCall, max int) (map[string]int, error) {
	current := make(map[string]int, len(calls))
	for _, call := range calls {
		signature := nativeToolSignature(call)
		count, exists := current[signature]
		if !exists {
			count = previous[signature]
		}
		count++
		current[signature] = count
		if count > max {
			return nil, fmt.Errorf("native Agent repeated the same tool call more than %d consecutive times", max)
		}
	}
	return current, nil
}

// A provider may hallucinate tools not in the advertised schema. Fail closed.
func nativeToolCallAllowed(tools []nativeModelToolDefinition, toolID string) bool {
	for _, tool := range tools {
		if tool.ID == toolID {
			return true
		}
	}
	return false
}

// Detect repeated A/B/A/B patterns in addition to identical consecutive calls.
// Requiring six full alternations avoids blocking short intentional comparisons.
func nativeNextToolLoopHistory(history []string, calls []nativeModelToolCall) ([]string, error) {
    signature := ""
    for _, call := range calls {
        signature += nativeToolSignature(call) + "\x01"
    }
    next := append(append([]string(nil), history...), signature)
    if len(next) > 12 {
        next = next[len(next)-12:]
    }
    if len(next) == 12 && next[0] != next[1] {
        repeating := true
        for i := 2; i < len(next); i++ {
            if next[i] != next[i%2] {
                repeating = false
                break
            }
        }
        if repeating {
            return nil, fmt.Errorf("native Agent detected a repeated alternating tool-call loop (ABAB)")
        }
    }
    return next, nil
}
