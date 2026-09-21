package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type kiloSessionCommandAdapter struct{}

func (kiloRuntimeEngine) SessionCommands() runtimeSessionCommandAdapter {
	return kiloSessionCommandAdapter{}
}

func kiloSessionCommandRequest(
	ctx context.Context,
	backend *runtimeBackend,
	method string,
	route string,
	directory string,
	query url.Values,
	body any,
) (json.RawMessage, error) {
	if query == nil {
		query = url.Values{}
	}
	if strings.TrimSpace(directory) != "" {
		query.Set("directory", directory)
	}

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := backend.newRequest(ctx, method, route, directory, query, reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	response, err := backend.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	data, err := io.ReadAll(io.LimitReader(response.Body, maxSessionCommandBody))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, &sessionRuntimeError{
			Status: response.StatusCode,
			Body:   strings.TrimSpace(string(data)),
		}
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}
	return unwrapRuntimePayload(json.RawMessage(data)), nil
}

func (kiloSessionCommandAdapter) CreateSession(
	ctx context.Context,
	backend *runtimeBackend,
	directory string,
	input sessionCreateInput,
) (string, error) {
	payload := map[string]any{}
	if input.ParentID != "" {
		payload["parentID"] = input.ParentID
	}
	if input.Title != "" {
		payload["title"] = input.Title
	}

	raw, err := kiloSessionCommandRequest(ctx, backend, http.MethodPost, "/session", directory, nil, payload)
	if err != nil {
		return "", err
	}
	var created map[string]any
	if err := json.Unmarshal(raw, &created); err != nil {
		return "", fmt.Errorf("decode runtime session create response: %w", err)
	}
	sessionID := sessionString(created["id"])
	if sessionID == "" {
		return "", errorsNewRuntimeSessionID()
	}
	return sessionID, nil
}

func (kiloSessionCommandAdapter) UpdateSession(
	ctx context.Context,
	backend *runtimeBackend,
	directory string,
	sessionID string,
	input sessionUpdateInput,
) error {
	payload := map[string]any{}
	if input.Title != nil {
		payload["title"] = *input.Title
	}
	_, err := kiloSessionCommandRequest(
		ctx,
		backend,
		http.MethodPatch,
		"/session/"+url.PathEscape(sessionID),
		directory,
		nil,
		payload,
	)
	return err
}

func (kiloSessionCommandAdapter) DeleteSession(
	ctx context.Context,
	backend *runtimeBackend,
	directory string,
	sessionID string,
) error {
	_, err := kiloSessionCommandRequest(
		ctx,
		backend,
		http.MethodDelete,
		"/session/"+url.PathEscape(sessionID),
		directory,
		nil,
		nil,
	)
	return err
}

func (kiloSessionCommandAdapter) RunSession(
	ctx context.Context,
	backend *runtimeBackend,
	directory string,
	sessionID string,
	input sessionRunInput,
) error {
	parts := input.Parts
	if len(parts) == 0 {
		parts = []map[string]any{{"type": "text", "text": input.Text}}
	}

	payload := map[string]any{"parts": parts}
	if input.MessageID != "" {
		payload["messageID"] = input.MessageID
	}
	if input.Agent != "" {
		payload["agent"] = input.Agent
	}
	if input.Model != nil {
		payload["model"] = map[string]any{
			"providerID": input.Model.ProviderID,
			"modelID":    input.Model.ID,
		}
	}
	if input.Variant != "" {
		payload["variant"] = input.Variant
	}

	_, err := kiloSessionCommandRequest(
		ctx,
		backend,
		http.MethodPost,
		"/session/"+url.PathEscape(sessionID)+"/prompt_async",
		directory,
		nil,
		payload,
	)
	return err
}

func (kiloSessionCommandAdapter) AbortSession(
	ctx context.Context,
	backend *runtimeBackend,
	directory string,
	sessionID string,
	input sessionAbortInput,
) error {
	query := url.Values{}
	if input.Scope != "" {
		query.Set("scope", input.Scope)
	}
	_, err := kiloSessionCommandRequest(
		ctx,
		backend,
		http.MethodPost,
		"/session/"+url.PathEscape(sessionID)+"/abort",
		directory,
		query,
		nil,
	)
	return err
}

func errorsNewRuntimeSessionID() error {
	return fmt.Errorf("runtime session create response is missing an id")
}
