package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

type kiloQuestionAdapter struct{}

func (kiloRuntimeEngine) Questions() runtimeQuestionAdapter {
	return kiloQuestionAdapter{}
}

func normalizeRuntimeQuestionOption(value any) questionOptionView {
	raw := sessionMap(value)
	if raw == nil {
		return questionOptionView{}
	}
	return questionOptionView{
		Label:       sessionString(raw["label"]),
		Description: sessionString(raw["description"]),
	}
}

func normalizeRuntimeQuestionPrompt(value any) questionPromptView {
	raw := sessionMap(value)
	if raw == nil {
		return questionPromptView{}
	}
	custom := true
	if value, ok := raw["custom"].(bool); ok {
		custom = value
	}
	options := make([]questionOptionView, 0)
	for _, item := range sessionArray(raw["options"]) {
		option := normalizeRuntimeQuestionOption(item)
		if option.Label != "" {
			options = append(options, option)
		}
	}
	return questionPromptView{
		Header:   sessionString(raw["header"]),
		Question: sessionString(raw["question"]),
		Options:  options,
		Multiple: sessionBool(raw["multiple"]),
		Custom:   custom,
		Default:  sessionString(raw["default"]),
	}
}

func normalizeRuntimeQuestionRequest(value any) questionRequestView {
	raw := sessionMap(value)
	if raw == nil {
		return questionRequestView{}
	}
	result := questionRequestView{
		ID:        sessionString(raw["id"]),
		SessionID: sessionString(raw["sessionID"]),
		Questions: []questionPromptView{},
	}
	for _, item := range sessionArray(raw["questions"]) {
		prompt := normalizeRuntimeQuestionPrompt(item)
		if prompt.Question != "" || prompt.Header != "" || len(prompt.Options) > 0 {
			result.Questions = append(result.Questions, prompt)
		}
	}
	return result
}

func kiloQuestionRequest(
	ctx context.Context,
	backend *runtimeBackend,
	method, route, directory string,
	body any,
) (json.RawMessage, error) {
	raw, err := kiloSessionCommandRequest(ctx, backend, method, route, directory, url.Values{}, body)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func (kiloQuestionAdapter) ListQuestions(
	ctx context.Context,
	backend *runtimeBackend,
	directory string,
) ([]questionRequestView, error) {
	raw, err := kiloQuestionRequest(ctx, backend, http.MethodGet, "/question", directory, nil)
	if err != nil {
		return nil, err
	}
	var rows []any
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, err
	}
	result := make([]questionRequestView, 0, len(rows))
	for _, row := range rows {
		item := normalizeRuntimeQuestionRequest(row)
		if item.ID != "" {
			result = append(result, item)
		}
	}
	return result, nil
}

func (kiloQuestionAdapter) ReplyQuestion(
	ctx context.Context,
	backend *runtimeBackend,
	directory, requestID string,
	answers [][]string,
) error {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return errors.New("question request id is required")
	}
	_, err := kiloQuestionRequest(
		ctx,
		backend,
		http.MethodPost,
		"/question/"+url.PathEscape(requestID)+"/reply",
		directory,
		map[string]any{"answers": answers},
	)
	return err
}

func (kiloQuestionAdapter) RejectQuestion(
	ctx context.Context,
	backend *runtimeBackend,
	directory, requestID string,
) error {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return errors.New("question request id is required")
	}
	_, err := kiloQuestionRequest(
		ctx,
		backend,
		http.MethodPost,
		"/question/"+url.PathEscape(requestID)+"/reject",
		directory,
		nil,
	)
	return err
}
