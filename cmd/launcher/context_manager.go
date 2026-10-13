package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	nativeContextDefaultLimit         = 65536
	nativeContextDefaultOutputReserve = 8192
	nativeContextSafetyReserve        = 1024
	nativeContextMinimumMessageBudget = 2048
	nativeContextToolCompactThreshold = 1024
	nativeContextToolArgumentThreshold = 2048
	nativeContextSoftCompactPercent = 70
	nativeContextMinimumRunTokenBudget = 262144
	nativeContextMaximumRunTokenBudget = 2000000
)

type nativeContextPlan struct {
	Messages               []nativeConversationMessage
	ContextLimit           int
	MessageBudget          int
	EstimatedRequestTokens int
	EstimatedMessageTokens int
	OmittedMessages        int
	OmittedTokens          int
	CompactedToolResults   int
	CompactedToolCalls     int
	CheckpointInserted     bool
	OverBudget             bool
}

func nativeEstimateTextTokens(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	// Deliberately conservative. English prose is usually ~4 chars/token while
	// source code and non-Latin text can be denser. Two runes/token leaves
	// headroom without depending on a provider-specific tokenizer.
	runes := utf8.RuneCountInString(text)
	if runes <= 0 {
		return 0
	}
	return (runes + 1) / 2
}

func nativeEstimateToolDefinitionTokens(tool nativeModelToolDefinition) int {
	total := nativeEstimateTextTokens(tool.ID) +
		nativeEstimateTextTokens(tool.Name) +
		nativeEstimateTextTokens(tool.Description) + 12
	if len(tool.InputSchema) > 0 {
		if encoded, err := json.Marshal(tool.InputSchema); err == nil {
			total += nativeEstimateTextTokens(string(encoded))
		}
	}
	return total
}

func nativeEstimateToolDefinitionsTokens(tools []nativeModelToolDefinition) int {
	total := 0
	for _, tool := range tools {
		total += nativeEstimateToolDefinitionTokens(tool)
	}
	return total
}

func nativeEstimateConversationMessageTokens(message nativeConversationMessage) int {
	total := 8 + nativeEstimateTextTokens(message.Role) + nativeEstimateTextTokens(message.Text)
	if message.ToolCallID != "" {
		total += nativeEstimateTextTokens(message.ToolCallID)
	}
	if message.ToolName != "" {
		total += nativeEstimateTextTokens(message.ToolName)
	}
	for _, call := range message.ToolCalls {
		total += 12 +
			nativeEstimateTextTokens(call.ID) +
			nativeEstimateTextTokens(call.Name) +
			nativeEstimateTextTokens(string(call.Arguments)) +
			nativeEstimateTextTokens(string(call.ProviderState))
	}
	return total
}

func nativeEstimateConversationTokens(messages []nativeConversationMessage) int {
	total := 0
	for _, message := range messages {
		total += nativeEstimateConversationMessageTokens(message)
	}
	return total
}

func nativeContextLimitForModel(model tlProviderModel) int {
	if model.ContextLimit > 0 {
		return model.ContextLimit
	}
	return nativeContextDefaultLimit
}

func nativeContextOutputReserve(model tlProviderModel, contextLimit int) int {
	reserve := model.OutputLimit
	if reserve <= 0 {
		reserve = nativeContextDefaultOutputReserve
	}
	// A provider can advertise an output limit close to its whole context
	// window. Keep enough space for the input side to remain useful.
	maxReserve := contextLimit / 3
	if maxReserve < 1024 {
		maxReserve = 1024
	}
	if reserve > maxReserve {
		reserve = maxReserve
	}
	if reserve < 512 {
		reserve = 512
	}
	return reserve
}

func nativeContextMessageBudget(system string, tools []nativeModelToolDefinition, model tlProviderModel) (contextLimit, messageBudget, fixedTokens int) {
	contextLimit = nativeContextLimitForModel(model)
	fixedTokens = nativeEstimateTextTokens(system) + nativeEstimateToolDefinitionsTokens(tools) + nativeContextSafetyReserve
	messageBudget = contextLimit - nativeContextOutputReserve(model, contextLimit) - fixedTokens
	if messageBudget < nativeContextMinimumMessageBudget {
		messageBudget = nativeContextMinimumMessageBudget
	}
	return contextLimit, messageBudget, fixedTokens
}

func cloneNativeConversation(messages []nativeConversationMessage) []nativeConversationMessage {
	result := make([]nativeConversationMessage, len(messages))
	for i, message := range messages {
		result[i] = message
		result[i].ToolCalls = append([]nativeModelToolCall(nil), message.ToolCalls...)
	}
	return result
}

func nativeCompactedToolResult(text string) (string, bool) {
	if nativeEstimateTextTokens(text) <= nativeContextToolCompactThreshold {
		return text, false
	}
	const note = "TL Studio omitted an older tool payload from the active model context. If exact content is needed again, re-read only the narrowest missing range; do not blindly repeat the same call."
	var payload map[string]any
	if json.Unmarshal([]byte(text), &payload) != nil {
		encoded, _ := json.Marshal(map[string]any{
			"compacted": true,
			"note":      note,
		})
		return string(encoded), true
	}

	if output, ok := payload["output"].(map[string]any); ok {
		metadata := map[string]any{}
		for key, value := range output {
			switch value.(type) {
			case string, bool, float64, json.Number, nil:
				if key != "content" {
					metadata[key] = value
				}
			}
		}
		metadata["contentCompacted"] = true
		payload["output"] = metadata
	} else {
		delete(payload, "output")
	}
	payload["compacted"] = true
	payload["note"] = note
	encoded, err := json.Marshal(payload)
	if err != nil {
		return text, false
	}
	return string(encoded), true
}

func nativeResolvedToolCallIDs(messages []nativeConversationMessage) map[string]struct{} {
	resolved := map[string]struct{}{}
	for _, message := range messages {
		if strings.EqualFold(strings.TrimSpace(message.Role), "tool") && strings.TrimSpace(message.ToolCallID) != "" {
			resolved[strings.TrimSpace(message.ToolCallID)] = struct{}{}
		}
	}
	return resolved
}

func nativeCompactCompletedToolHistory(messages []nativeConversationMessage, aggressive bool) ([]nativeConversationMessage, int, int) {
	result := cloneNativeConversation(messages)
	resolved := nativeResolvedToolCallIDs(result)
	compactedResults := 0
	compactedCalls := 0

	lastToolIndex := -1
	lastRoundIndex := -1
	lastRoundIDs := map[string]struct{}{}
	for i := range result {
		if strings.EqualFold(strings.TrimSpace(result[i].Role), "assistant") && len(result[i].ToolCalls) > 0 {
			lastRoundIndex = i
			lastRoundIDs = map[string]struct{}{}
			for _, call := range result[i].ToolCalls {
				lastRoundIDs[strings.TrimSpace(call.ID)] = struct{}{}
			}
		}
		if strings.EqualFold(strings.TrimSpace(result[i].Role), "tool") {
			lastToolIndex = i
		}
	}

	for i := range result {
		message := &result[i]
		role := strings.ToLower(strings.TrimSpace(message.Role))
		if role == "tool" {
			// The model must always see the most recent tool result exactly once.
			// Compacting it before the continuation turn can cause blind retries.
			_, isMostRecentRound := lastRoundIDs[strings.TrimSpace(message.ToolCallID)]
			if i == lastToolIndex || (lastRoundIndex >= 0 && isMostRecentRound) {
				continue
			}
			if compacted, ok := nativeCompactedToolResult(message.Text); ok {
				message.Text = compacted
				compactedResults++
			}
			continue
		}
		if role != "assistant" || len(message.ToolCalls) == 0 {
			continue
		}
		for callIndex := range message.ToolCalls {
			call := &message.ToolCalls[callIndex]
			callID := strings.TrimSpace(call.ID)
			if _, ok := resolved[callID]; !ok {
				continue
			}
			if _, protected := lastRoundIDs[callID]; protected && lastRoundIndex >= 0 {
				continue
			}
			if nativeEstimateTextTokens(string(call.Arguments)) <= nativeContextToolArgumentThreshold {
				continue
			}
			if !aggressive && i >= lastToolIndex-1 {
				continue
			}
			originalBytes := len(call.Arguments)
			call.Arguments = json.RawMessage(fmt.Sprintf(`{"_tlStudioContextCompacted":true,"originalBytes":%d}`, originalBytes))
			call.ProviderState = nil
			compactedCalls++
		}
	}
	return result, compactedResults, compactedCalls
}

type nativeContextSegment struct {
	start int
	end   int
	cost  int
}

func nativeContextSegments(messages []nativeConversationMessage) []nativeContextSegment {
	if len(messages) == 0 {
		return nil
	}
	starts := []int{0}
	for i := 1; i < len(messages); i++ {
		if strings.EqualFold(strings.TrimSpace(messages[i].Role), "user") {
			starts = append(starts, i)
		}
	}
	segments := make([]nativeContextSegment, 0, len(starts))
	for i, start := range starts {
		end := len(messages)
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		segments = append(segments, nativeContextSegment{
			start: start,
			end:   end,
			cost:  nativeEstimateConversationTokens(messages[start:end]),
		})
	}
	return segments
}

func nativeContextCheckpointMessage(omittedMessages, omittedTokens int) nativeConversationMessage {
	return nativeConversationMessage{
		Role: "user",
		Text: fmt.Sprintf(
			"[TL Studio context checkpoint]\nEarlier persisted conversation history was omitted from this model request to stay within the selected model's context budget. Omitted messages: %d (estimated %d tokens). The full session transcript is still stored locally by TL Studio. Do not invent details from omitted history; inspect the workspace or ask for missing information when those details are required.",
			omittedMessages,
			omittedTokens,
		),
	}
}

func nativeSelectRecentContext(messages []nativeConversationMessage, budget int) (selected []nativeConversationMessage, omittedMessages, omittedTokens int, checkpoint bool) {
	if len(messages) == 0 {
		return nil, 0, 0, false
	}
	segments := nativeContextSegments(messages)
	if len(segments) == 0 {
		return cloneNativeConversation(messages), 0, 0, false
	}

	startSegment := len(segments) - 1
	used := segments[startSegment].cost
	checkpointReserve := nativeEstimateConversationMessageTokens(nativeContextCheckpointMessage(len(messages), nativeEstimateConversationTokens(messages)))

	for startSegment > 0 {
		previous := segments[startSegment-1]
		if used+previous.cost+checkpointReserve > budget {
			break
		}
		startSegment--
		used += previous.cost
	}

	startIndex := segments[startSegment].start
	if startIndex == 0 {
		return cloneNativeConversation(messages), 0, 0, false
	}
	selected = cloneNativeConversation(messages[startIndex:])
	omittedMessages = startIndex
	omittedTokens = nativeEstimateConversationTokens(messages[:startIndex])
	checkpointMessage := nativeContextCheckpointMessage(omittedMessages, omittedTokens)
	selected = append([]nativeConversationMessage{checkpointMessage}, selected...)
	return selected, omittedMessages, omittedTokens, true
}

func buildNativeContextPlan(messages []nativeConversationMessage, system string, tools []nativeModelToolDefinition, model tlProviderModel) nativeContextPlan {
	contextLimit, messageBudget, fixedTokens := nativeContextMessageBudget(system, tools, model)
	working := cloneNativeConversation(messages)
	originalTokens := nativeEstimateConversationTokens(working)
	outputReserve := nativeContextOutputReserve(model, contextLimit)

	plan := nativeContextPlan{
		Messages:               working,
		ContextLimit:           contextLimit,
		MessageBudget:          messageBudget,
		EstimatedMessageTokens: originalTokens,
		EstimatedRequestTokens: fixedTokens + outputReserve + originalTokens,
	}

	softLimit := messageBudget * nativeContextSoftCompactPercent / 100
	if softLimit < nativeContextMinimumMessageBudget {
		softLimit = nativeContextMinimumMessageBudget
	}
	if originalTokens > softLimit {
		working, plan.CompactedToolResults, plan.CompactedToolCalls = nativeCompactCompletedToolHistory(working, false)
	}

	workingTokens := nativeEstimateConversationTokens(working)
	if workingTokens > messageBudget {
		var moreResults, moreCalls int
		working, moreResults, moreCalls = nativeCompactCompletedToolHistory(working, true)
		plan.CompactedToolResults += moreResults
		plan.CompactedToolCalls += moreCalls
		workingTokens = nativeEstimateConversationTokens(working)
	}

	if workingTokens <= messageBudget {
		plan.Messages = working
		plan.EstimatedMessageTokens = workingTokens
		plan.EstimatedRequestTokens = fixedTokens + outputReserve + workingTokens
		plan.OverBudget = plan.EstimatedRequestTokens > contextLimit
		return plan
	}

	selected, omittedMessages, omittedTokens, checkpoint := nativeSelectRecentContext(working, messageBudget)
	plan.Messages = selected
	plan.OmittedMessages = omittedMessages
	plan.OmittedTokens = omittedTokens
	plan.CheckpointInserted = checkpoint
	plan.EstimatedMessageTokens = nativeEstimateConversationTokens(selected)
	plan.EstimatedRequestTokens = fixedTokens + outputReserve + plan.EstimatedMessageTokens
	plan.OverBudget = plan.EstimatedRequestTokens > contextLimit
	return plan
}

func nativeContextRunTokenBudget(model tlProviderModel) int {
	budget := nativeContextLimitForModel(model) * 4
	if budget < nativeContextMinimumRunTokenBudget {
		budget = nativeContextMinimumRunTokenBudget
	}
	if budget > nativeContextMaximumRunTokenBudget {
		budget = nativeContextMaximumRunTokenBudget
	}
	return budget
}

func nativeContextEstimatedInputTokens(plan nativeContextPlan, model tlProviderModel) int {
	input := plan.EstimatedRequestTokens - nativeContextOutputReserve(model, plan.ContextLimit)
	if input < 0 {
		return 0
	}
	return input
}

func nativeContextActivity(plan nativeContextPlan) sessionActivityView {
	status := "completed"
	title := "Context window managed"
	if plan.OverBudget {
		status = "warning"
		title = "Context budget exceeded"
	}
	return sessionActivityView{
		Kind:   "context",
		Status: status,
		Title:  title,
		Metadata: map[string]any{
			"contextLimit":         plan.ContextLimit,
			"messageBudget":        plan.MessageBudget,
			"estimatedTokens":      plan.EstimatedMessageTokens,
			"omittedMessages":      plan.OmittedMessages,
			"omittedTokens":        plan.OmittedTokens,
			"compactedToolResults": plan.CompactedToolResults,
			"compactedToolCalls":   plan.CompactedToolCalls,
			"checkpointInserted":   plan.CheckpointInserted,
			"overBudget":           plan.OverBudget,
		},
	}
}

func nativeContextPlanSignature(plan nativeContextPlan) string {
	if !nativeContextPlanChanged(plan) {
		return ""
	}
	return fmt.Sprintf(
		"%d/%d/%d/%d/%d/%t/%t",
		plan.ContextLimit,
		plan.MessageBudget,
		plan.OmittedMessages,
		plan.CompactedToolResults,
		plan.CompactedToolCalls,
		plan.CheckpointInserted,
		plan.OverBudget,
	)
}

func nativeContextPlanChanged(plan nativeContextPlan) bool {
	return plan.OmittedMessages > 0 ||
		plan.CompactedToolResults > 0 ||
		plan.CompactedToolCalls > 0 ||
		plan.OverBudget
}
