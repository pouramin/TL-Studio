package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const nativeModelMaxResponseBytes = 32 << 20

type nativeModelToolDefinition struct {
	ID          string
	Name        string
	Description string
	InputSchema map[string]any
}

type nativeModelToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

type nativeConversationMessage struct {
	Role       string
	Text       string
	ToolCallID string
	ToolName   string
	ToolCalls  []nativeModelToolCall
}

type nativeModelRequest struct {
	System   string
	Provider tlProviderDefinition
	Model    tlProviderModel
	APIKey   string
	Messages []nativeConversationMessage
	Tools    []nativeModelToolDefinition
}

type nativeModelResponse struct {
	Text         string
	ToolCalls    []nativeModelToolCall
	FinishReason string
	Usage        sessionUsage
}

type nativeModelClient interface {
	Complete(ctx context.Context, request nativeModelRequest, onTextDelta func(string)) (nativeModelResponse, error)
}

type nativeHTTPModelClient struct {
	httpClient *http.Client
}

func newNativeModelClient() nativeModelClient {
	return &nativeHTTPModelClient{httpClient: &http.Client{Timeout: 0}}
}

func (m *runtimeProviderManager) resolveNativeModel(providerID, modelID string) (tlProviderDefinition, tlProviderModel, string, error) {
	providerID = strings.TrimSpace(providerID)
	modelID = strings.TrimSpace(modelID)
	if providerID == "" || modelID == "" {
		return tlProviderDefinition{}, tlProviderModel{}, "", errors.New("provider and model are required for native execution")
	}
	if providerID == runtimeHostedProviderID {
		return tlProviderDefinition{}, tlProviderModel{}, "", errors.New("hosted runtime models use the compatibility execution path")
	}
	provider, ok, err := m.store.get(providerID)
	if err != nil {
		return tlProviderDefinition{}, tlProviderModel{}, "", err
	}
	if !ok {
		return tlProviderDefinition{}, tlProviderModel{}, "", fmt.Errorf("provider %q is not managed by TL Studio", providerID)
	}
	var model tlProviderModel
	found := false
	for _, candidate := range provider.Models {
		if candidate.ID == modelID {
			model = candidate
			found = true
			break
		}
	}
	if !found {
		return tlProviderDefinition{}, tlProviderModel{}, "", fmt.Errorf("model %q is not configured for provider %q", modelID, providerID)
	}
	if !model.ToolCall {
		return tlProviderDefinition{}, tlProviderModel{}, "", fmt.Errorf("model %q does not advertise tool calling", modelID)
	}
	if m.credentials == nil {
		return tlProviderDefinition{}, tlProviderModel{}, "", errors.New("TL Studio credential store is unavailable")
	}
	key, err := m.credentials.Get(providerID)
	if err != nil {
		if errors.Is(err, errCredentialNotFound) {
			return tlProviderDefinition{}, tlProviderModel{}, "", fmt.Errorf("provider %q has no TL Studio-owned credential", providerID)
		}
		return tlProviderDefinition{}, tlProviderModel{}, "", err
	}
	return provider, model, key, nil
}

func nativeToolWireName(id string) string {
	replacer := strings.NewReplacer(".", "__", "/", "__", ":", "__")
	return "tl_" + replacer.Replace(strings.TrimSpace(id))
}

func nativeToolIDFromWire(name string, tools []nativeModelToolDefinition) string {
	for _, tool := range tools {
		if nativeToolWireName(tool.ID) == name {
			return tool.ID
		}
	}
	return strings.TrimSpace(name)
}

func nativeEndpoint(baseURL, suffix string) (string, error) {
	base, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return "", errors.New("invalid provider base URL")
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/" + strings.TrimLeft(suffix, "/")
	base.RawQuery = ""
	base.Fragment = ""
	return base.String(), nil
}

func (c *nativeHTTPModelClient) Complete(ctx context.Context, request nativeModelRequest, onTextDelta func(string)) (nativeModelResponse, error) {
	switch request.Provider.Protocol {
	case "openai-compatible":
		return c.completeOpenAIChat(ctx, request, onTextDelta)
	case "openai-responses":
		return c.completeOpenAIResponses(ctx, request, onTextDelta)
	case "anthropic-messages":
		return c.completeAnthropic(ctx, request, onTextDelta)
	default:
		return nativeModelResponse{}, fmt.Errorf("unsupported native provider protocol %q", request.Provider.Protocol)
	}
}

func (c *nativeHTTPModelClient) doJSON(ctx context.Context, endpoint string, headers map[string]string, payload any) (*http.Response, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream, application/json")
	for key, value := range headers {
		if strings.TrimSpace(value) != "" {
			req.Header.Set(key, value)
		}
	}
	return c.httpClient.Do(req)
}

func modelHTTPError(response *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	return fmt.Errorf("model request failed with status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
}

func openAITools(tools []nativeModelToolDefinition) []map[string]any {
	result := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		result = append(result, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        nativeToolWireName(tool.ID),
				"description": tool.Description,
				"parameters":  tool.InputSchema,
			},
		})
	}
	return result
}

func openAIChatMessages(request nativeModelRequest) []map[string]any {
	messages := make([]map[string]any, 0, len(request.Messages)+1)
	if strings.TrimSpace(request.System) != "" {
		messages = append(messages, map[string]any{"role": "system", "content": request.System})
	}
	for _, message := range request.Messages {
		switch message.Role {
		case "tool":
			messages = append(messages, map[string]any{
				"role":         "tool",
				"tool_call_id": message.ToolCallID,
				"content":      message.Text,
			})
		case "assistant":
			item := map[string]any{"role": "assistant"}
			if message.Text != "" {
				item["content"] = message.Text
			} else {
				item["content"] = nil
			}
			if len(message.ToolCalls) > 0 {
				calls := make([]map[string]any, 0, len(message.ToolCalls))
				for _, call := range message.ToolCalls {
					calls = append(calls, map[string]any{
						"id":   call.ID,
						"type": "function",
						"function": map[string]any{
							"name":      nativeToolWireName(call.Name),
							"arguments": string(call.Arguments),
						},
					})
				}
				item["tool_calls"] = calls
			}
			messages = append(messages, item)
		default:
			messages = append(messages, map[string]any{"role": "user", "content": message.Text})
		}
	}
	return messages
}

type openAIStreamToolAccumulator struct {
	ID        string
	Name      string
	Arguments strings.Builder
}

func (c *nativeHTTPModelClient) completeOpenAIChat(ctx context.Context, request nativeModelRequest, onTextDelta func(string)) (nativeModelResponse, error) {
	endpoint, err := nativeEndpoint(request.Provider.BaseURL, "chat/completions")
	if err != nil {
		return nativeModelResponse{}, err
	}
	payload := map[string]any{
		"model":    request.Model.ID,
		"messages": openAIChatMessages(request),
		"stream":   true,
	}
	if len(request.Tools) > 0 {
		payload["tools"] = openAITools(request.Tools)
		payload["tool_choice"] = "auto"
	}
	response, err := c.doJSON(ctx, endpoint, map[string]string{"Authorization": "Bearer " + request.APIKey}, payload)
	if err != nil {
		return nativeModelResponse{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nativeModelResponse{}, modelHTTPError(response)
	}
	if !strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "text/event-stream") {
		return parseOpenAIChatJSON(response.Body, request.Tools, onTextDelta)
	}

	var result nativeModelResponse
	toolParts := map[int]*openAIStreamToolAccumulator{}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 64*1024), nativeModelMaxResponseBytes)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var event struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
			Usage struct {
				PromptTokens     int64 `json:"prompt_tokens"`
				CompletionTokens int64 `json:"completion_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal([]byte(data), &event) != nil {
			continue
		}
		result.Usage.Input += event.Usage.PromptTokens
		result.Usage.Output += event.Usage.CompletionTokens
		for _, choice := range event.Choices {
			if choice.Delta.Content != "" {
				result.Text += choice.Delta.Content
				if onTextDelta != nil {
					onTextDelta(choice.Delta.Content)
				}
			}
			for _, delta := range choice.Delta.ToolCalls {
				accumulator := toolParts[delta.Index]
				if accumulator == nil {
					accumulator = &openAIStreamToolAccumulator{}
					toolParts[delta.Index] = accumulator
				}
				if delta.ID != "" {
					accumulator.ID = delta.ID
				}
				if delta.Function.Name != "" {
					accumulator.Name = delta.Function.Name
				}
				accumulator.Arguments.WriteString(delta.Function.Arguments)
			}
			if choice.FinishReason != "" {
				result.FinishReason = choice.FinishReason
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nativeModelResponse{}, err
	}
	for index := 0; index < len(toolParts); index++ {
		part := toolParts[index]
		if part == nil {
			continue
		}
		result.ToolCalls = append(result.ToolCalls, nativeModelToolCall{
			ID:        part.ID,
			Name:      nativeToolIDFromWire(part.Name, request.Tools),
			Arguments: json.RawMessage(part.Arguments.String()),
		})
	}
	return result, nil
}

func parseOpenAIChatJSON(reader io.Reader, tools []nativeModelToolDefinition, onTextDelta func(string)) (nativeModelResponse, error) {
	data, err := io.ReadAll(io.LimitReader(reader, nativeModelMaxResponseBytes))
	if err != nil {
		return nativeModelResponse{}, err
	}
	var payload struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nativeModelResponse{}, fmt.Errorf("decode OpenAI-compatible response: %w", err)
	}
	result := nativeModelResponse{Usage: sessionUsage{Input: payload.Usage.PromptTokens, Output: payload.Usage.CompletionTokens}}
	if len(payload.Choices) == 0 {
		return result, errors.New("model response contained no choices")
	}
	choice := payload.Choices[0]
	result.Text = choice.Message.Content
	result.FinishReason = choice.FinishReason
	if result.Text != "" && onTextDelta != nil {
		onTextDelta(result.Text)
	}
	for _, call := range choice.Message.ToolCalls {
		result.ToolCalls = append(result.ToolCalls, nativeModelToolCall{
			ID: call.ID,
			Name: nativeToolIDFromWire(call.Function.Name, tools),
			Arguments: json.RawMessage(call.Function.Arguments),
		})
	}
	return result, nil
}

func responseTools(tools []nativeModelToolDefinition) []map[string]any {
	result := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		result = append(result, map[string]any{
			"type":        "function",
			"name":        nativeToolWireName(tool.ID),
			"description": tool.Description,
			"parameters":  tool.InputSchema,
			"strict":      false,
		})
	}
	return result
}

func responseInput(request nativeModelRequest) []any {
	input := make([]any, 0, len(request.Messages)+1)
	if strings.TrimSpace(request.System) != "" {
		input = append(input, map[string]any{"role": "system", "content": request.System})
	}
	for _, message := range request.Messages {
		switch message.Role {
		case "tool":
			input = append(input, map[string]any{
				"type":    "function_call_output",
				"call_id": message.ToolCallID,
				"output":  message.Text,
			})
		case "assistant":
			if message.Text != "" {
				input = append(input, map[string]any{"role": "assistant", "content": message.Text})
			}
			for _, call := range message.ToolCalls {
				input = append(input, map[string]any{
					"type":      "function_call",
					"call_id":   call.ID,
					"name":      nativeToolWireName(call.Name),
					"arguments": string(call.Arguments),
				})
			}
		default:
			input = append(input, map[string]any{"role": "user", "content": message.Text})
		}
	}
	return input
}

func (c *nativeHTTPModelClient) completeOpenAIResponses(ctx context.Context, request nativeModelRequest, onTextDelta func(string)) (nativeModelResponse, error) {
	endpoint, err := nativeEndpoint(request.Provider.BaseURL, "responses")
	if err != nil {
		return nativeModelResponse{}, err
	}
	payload := map[string]any{
		"model":  request.Model.ID,
		"input":  responseInput(request),
		"stream": true,
	}
	if len(request.Tools) > 0 {
		payload["tools"] = responseTools(request.Tools)
	}
	response, err := c.doJSON(ctx, endpoint, map[string]string{"Authorization": "Bearer " + request.APIKey}, payload)
	if err != nil {
		return nativeModelResponse{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nativeModelResponse{}, modelHTTPError(response)
	}
	if !strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "text/event-stream") {
		return parseOpenAIResponsesJSON(response.Body, request.Tools, onTextDelta)
	}

	var result nativeModelResponse
	type callAccumulator struct {
		ID, Name string
		Args strings.Builder
	}
	calls := map[string]*callAccumulator{}
	order := []string{}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 64*1024), nativeModelMaxResponseBytes)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var event map[string]any
		if json.Unmarshal([]byte(data), &event) != nil {
			continue
		}
		eventType, _ := event["type"].(string)
		switch eventType {
		case "response.output_text.delta":
			delta, _ := event["delta"].(string)
			if delta != "" {
				result.Text += delta
				if onTextDelta != nil {
					onTextDelta(delta)
				}
			}
		case "response.output_item.added":
			item, _ := event["item"].(map[string]any)
			if item == nil || item["type"] != "function_call" {
				continue
			}
			id, _ := item["call_id"].(string)
			if id == "" {
				id, _ = item["id"].(string)
			}
			name, _ := item["name"].(string)
			if id != "" {
				calls[id] = &callAccumulator{ID: id, Name: name}
				order = append(order, id)
			}
		case "response.function_call_arguments.delta":
			id, _ := event["call_id"].(string)
			if id == "" {
				id, _ = event["item_id"].(string)
			}
			delta, _ := event["delta"].(string)
			if accumulator := calls[id]; accumulator != nil {
				accumulator.Args.WriteString(delta)
			}
		case "response.completed":
			responseValue, _ := event["response"].(map[string]any)
			result.FinishReason = "completed"
			if usage, _ := responseValue["usage"].(map[string]any); usage != nil {
				result.Usage.Input = int64(intFromAny(usage["input_tokens"]))
				result.Usage.Output = int64(intFromAny(usage["output_tokens"]))
			}
		case "response.failed":
			return nativeModelResponse{}, errors.New("OpenAI Responses request failed")
		}
	}
	if err := scanner.Err(); err != nil {
		return nativeModelResponse{}, err
	}
	for _, id := range order {
		accumulator := calls[id]
		result.ToolCalls = append(result.ToolCalls, nativeModelToolCall{
			ID: accumulator.ID,
			Name: nativeToolIDFromWire(accumulator.Name, request.Tools),
			Arguments: json.RawMessage(accumulator.Args.String()),
		})
	}
	return result, nil
}

func parseOpenAIResponsesJSON(reader io.Reader, tools []nativeModelToolDefinition, onTextDelta func(string)) (nativeModelResponse, error) {
	data, err := io.ReadAll(io.LimitReader(reader, nativeModelMaxResponseBytes))
	if err != nil {
		return nativeModelResponse{}, err
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nativeModelResponse{}, fmt.Errorf("decode OpenAI Responses payload: %w", err)
	}
	result := nativeModelResponse{FinishReason: "completed"}
	if text, _ := payload["output_text"].(string); text != "" {
		result.Text = text
		if onTextDelta != nil {
			onTextDelta(text)
		}
	}
	for _, raw := range sessionArray(payload["output"]) {
		item := sessionMap(raw)
		if item == nil {
			continue
		}
		switch sessionString(item["type"]) {
		case "message":
			for _, blockRaw := range sessionArray(item["content"]) {
				block := sessionMap(blockRaw)
				if block != nil && sessionString(block["type"]) == "output_text" {
					text := sessionString(block["text"])
					if text != "" && !strings.Contains(result.Text, text) {
						result.Text += text
						if onTextDelta != nil {
							onTextDelta(text)
						}
					}
				}
			}
		case "function_call":
			args, _ := item["arguments"].(string)
			result.ToolCalls = append(result.ToolCalls, nativeModelToolCall{
				ID: sessionString(item["call_id"]),
				Name: nativeToolIDFromWire(sessionString(item["name"]), tools),
				Arguments: json.RawMessage(args),
			})
		}
	}
	if usage := sessionMap(payload["usage"]); usage != nil {
		result.Usage.Input = sessionInt64(usage["input_tokens"])
		result.Usage.Output = sessionInt64(usage["output_tokens"])
	}
	return result, nil
}

func anthropicTools(tools []nativeModelToolDefinition) []map[string]any {
	result := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		result = append(result, map[string]any{
			"name":         nativeToolWireName(tool.ID),
			"description":  tool.Description,
			"input_schema": tool.InputSchema,
		})
	}
	return result
}

func anthropicMessages(request nativeModelRequest) []map[string]any {
	messages := make([]map[string]any, 0, len(request.Messages))
	for _, message := range request.Messages {
		switch message.Role {
		case "tool":
			messages = append(messages, map[string]any{
				"role": "user",
				"content": []map[string]any{{
					"type":        "tool_result",
					"tool_use_id": message.ToolCallID,
					"content":     message.Text,
				}},
			})
		case "assistant":
			blocks := make([]map[string]any, 0, 1+len(message.ToolCalls))
			if message.Text != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": message.Text})
			}
			for _, call := range message.ToolCalls {
				var input any = map[string]any{}
				if len(call.Arguments) > 0 {
					_ = json.Unmarshal(call.Arguments, &input)
				}
				blocks = append(blocks, map[string]any{
					"type":  "tool_use",
					"id":    call.ID,
					"name":  nativeToolWireName(call.Name),
					"input": input,
				})
			}
			messages = append(messages, map[string]any{"role": "assistant", "content": blocks})
		default:
			messages = append(messages, map[string]any{"role": "user", "content": message.Text})
		}
	}
	return messages
}

func (c *nativeHTTPModelClient) completeAnthropic(ctx context.Context, request nativeModelRequest, onTextDelta func(string)) (nativeModelResponse, error) {
	endpoint, err := nativeEndpoint(request.Provider.BaseURL, "messages")
	if err != nil {
		return nativeModelResponse{}, err
	}
	maxTokens := request.Model.OutputLimit
	if maxTokens <= 0 {
		maxTokens = 8192
	}
	payload := map[string]any{
		"model":      request.Model.ID,
		"max_tokens": maxTokens,
		"messages":   anthropicMessages(request),
		"stream":     true,
	}
	if request.System != "" {
		payload["system"] = request.System
	}
	if len(request.Tools) > 0 {
		payload["tools"] = anthropicTools(request.Tools)
	}
	response, err := c.doJSON(ctx, endpoint, map[string]string{
		"x-api-key":         request.APIKey,
		"anthropic-version": "2023-06-01",
	}, payload)
	if err != nil {
		return nativeModelResponse{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nativeModelResponse{}, modelHTTPError(response)
	}
	if !strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "text/event-stream") {
		return parseAnthropicJSON(response.Body, request.Tools, onTextDelta)
	}

	var result nativeModelResponse
	type anthropicToolAccumulator struct {
		ID, Name string
		Args strings.Builder
	}
	blocks := map[int]*anthropicToolAccumulator{}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 64*1024), nativeModelMaxResponseBytes)
	var eventName string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		var event map[string]any
		if json.Unmarshal([]byte(data), &event) != nil {
			continue
		}
		eventType := sessionString(event["type"])
		if eventType == "" {
			eventType = eventName
		}
		switch eventType {
		case "message_start":
			message := sessionMap(event["message"])
			if usage := sessionMap(message["usage"]); usage != nil {
				result.Usage.Input = sessionInt64(usage["input_tokens"])
			}
		case "content_block_start":
			index := int(sessionInt64(event["index"]))
			block := sessionMap(event["content_block"])
			if block != nil && sessionString(block["type"]) == "tool_use" {
				blocks[index] = &anthropicToolAccumulator{
					ID: sessionString(block["id"]),
					Name: sessionString(block["name"]),
				}
				if input := block["input"]; input != nil {
					if encoded, marshalErr := json.Marshal(input); marshalErr == nil && string(encoded) != "{}" {
						blocks[index].Args.Write(encoded)
					}
				}
			}
		case "content_block_delta":
			index := int(sessionInt64(event["index"]))
			delta := sessionMap(event["delta"])
			switch sessionString(delta["type"]) {
			case "text_delta":
				text := sessionString(delta["text"])
				if text != "" {
					result.Text += text
					if onTextDelta != nil {
						onTextDelta(text)
					}
				}
			case "input_json_delta":
				if block := blocks[index]; block != nil {
					block.Args.WriteString(sessionString(delta["partial_json"]))
				}
			}
		case "message_delta":
			delta := sessionMap(event["delta"])
			result.FinishReason = sessionString(delta["stop_reason"])
			if usage := sessionMap(event["usage"]); usage != nil {
				result.Usage.Output = sessionInt64(usage["output_tokens"])
			}
		case "error":
			errorValue := sessionMap(event["error"])
			return nativeModelResponse{}, fmt.Errorf("Anthropic stream error: %s", firstSessionString(errorValue["message"], errorValue["type"]))
		}
	}
	if err := scanner.Err(); err != nil {
		return nativeModelResponse{}, err
	}
	indices := make([]int, 0, len(blocks))
	for index := range blocks {
		indices = append(indices, index)
	}
	for left := 0; left < len(indices); left++ {
		for right := left + 1; right < len(indices); right++ {
			if indices[right] < indices[left] {
				indices[left], indices[right] = indices[right], indices[left]
			}
		}
	}
	for _, index := range indices {
		block := blocks[index]
		args := block.Args.String()
		if args == "" {
			args = "{}"
		}
		result.ToolCalls = append(result.ToolCalls, nativeModelToolCall{
			ID: block.ID,
			Name: nativeToolIDFromWire(block.Name, request.Tools),
			Arguments: json.RawMessage(args),
		})
	}
	return result, nil
}

func parseAnthropicJSON(reader io.Reader, tools []nativeModelToolDefinition, onTextDelta func(string)) (nativeModelResponse, error) {
	data, err := io.ReadAll(io.LimitReader(reader, nativeModelMaxResponseBytes))
	if err != nil {
		return nativeModelResponse{}, err
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nativeModelResponse{}, fmt.Errorf("decode Anthropic response: %w", err)
	}
	result := nativeModelResponse{FinishReason: sessionString(payload["stop_reason"])}
	for _, raw := range sessionArray(payload["content"]) {
		block := sessionMap(raw)
		switch sessionString(block["type"]) {
		case "text":
			text := sessionString(block["text"])
			result.Text += text
			if text != "" && onTextDelta != nil {
				onTextDelta(text)
			}
		case "tool_use":
			encoded, _ := json.Marshal(block["input"])
			result.ToolCalls = append(result.ToolCalls, nativeModelToolCall{
				ID: sessionString(block["id"]),
				Name: nativeToolIDFromWire(sessionString(block["name"]), tools),
				Arguments: json.RawMessage(encoded),
			})
		}
	}
	if usage := sessionMap(payload["usage"]); usage != nil {
		result.Usage.Input = sessionInt64(usage["input_tokens"])
		result.Usage.Output = sessionInt64(usage["output_tokens"])
	}
	return result, nil
}
