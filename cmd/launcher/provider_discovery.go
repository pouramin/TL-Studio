package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	providerDiscoveryCacheVersion = 1
	providerDiscoveryMaxBodyBytes = 8 << 20
	providerDiscoveryMaxModels    = 5000
	providerDiscoveryTimeout      = 15 * time.Second
)

type providerDiscoveryRequest struct {
	ProviderID string `json:"providerID,omitempty"`
	Protocol   string `json:"protocol"`
	BaseURL    string `json:"baseURL"`
	APIKey     string `json:"apiKey,omitempty"`
}

type providerDiscoveredModel struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Kind         string `json:"kind,omitempty"`
	ToolCall     *bool  `json:"toolCall,omitempty"`
	Reasoning    *bool  `json:"reasoning,omitempty"`
	Vision       *bool  `json:"vision,omitempty"`
	ContextLimit int    `json:"contextLimit,omitempty"`
	OutputLimit  int    `json:"outputLimit,omitempty"`
}

type providerDiscoveryResponse struct {
	ProviderID string                    `json:"providerID,omitempty"`
	Protocol   string                    `json:"protocol"`
	BaseURL    string                    `json:"baseURL"`
	Models     []providerDiscoveredModel `json:"models"`
	Source     string                    `json:"source"`
	Stale      bool                      `json:"stale,omitempty"`
	FetchedAt  string                    `json:"fetchedAt,omitempty"`
	Warning    string                    `json:"warning,omitempty"`
}

type providerDiscoveryCacheEntry struct {
	ProviderID string                    `json:"providerID"`
	Protocol   string                    `json:"protocol"`
	BaseURL    string                    `json:"baseURL"`
	Models     []providerDiscoveredModel `json:"models"`
	FetchedAt  string                    `json:"fetchedAt"`
}

type providerDiscoveryCacheFile struct {
	Version int                           `json:"version"`
	Entries []providerDiscoveryCacheEntry `json:"entries"`
}

var providerDiscoveryCacheMu sync.Mutex

func providerDiscoveryCachePath() string {
	return filepath.Join(tlStudioStateDirectory(), "model-catalog-cache.json")
}

func normalizeProviderDiscoveryRequest(input providerDiscoveryRequest) (providerDiscoveryRequest, error) {
	input.ProviderID = strings.TrimSpace(input.ProviderID)
	input.Protocol = strings.ToLower(strings.TrimSpace(input.Protocol))
	input.BaseURL = strings.TrimSpace(input.BaseURL)
	input.APIKey = strings.TrimSpace(input.APIKey)

	if input.ProviderID != "" && (!validProviderID(input.ProviderID) || input.ProviderID == runtimeHostedProviderID) {
		return providerDiscoveryRequest{}, errors.New("invalid provider ID")
	}
	if _, ok := runtimeProviderPackages[input.Protocol]; !ok {
		return providerDiscoveryRequest{}, errors.New("unsupported provider protocol")
	}
	parsed, err := url.Parse(input.BaseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return providerDiscoveryRequest{}, errors.New("provider base URL must be a valid http(s) URL")
	}
	if parsed.User != nil {
		return providerDiscoveryRequest{}, errors.New("provider base URL must not contain credentials")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	input.BaseURL = strings.TrimRight(parsed.String(), "/")
	return input, nil
}

func providerDiscoveryHTTPClient() *http.Client {
	return &http.Client{
		Timeout: providerDiscoveryTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("provider model discovery stopped after too many redirects")
			}
			if len(via) == 0 {
				return nil
			}
			previous := via[len(via)-1].URL
			if !strings.EqualFold(previous.Scheme, req.URL.Scheme) || !strings.EqualFold(previous.Host, req.URL.Host) {
				return errors.New("provider model discovery blocked a cross-origin redirect")
			}
			return nil
		},
	}
}

type providerDiscoveryHTTPError struct {
	Status int
}

func (e *providerDiscoveryHTTPError) Error() string {
	switch e.Status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "provider authentication failed"
	case http.StatusTooManyRequests:
		return "provider model discovery was rate limited"
	default:
		return fmt.Sprintf("provider model discovery failed with status %d", e.Status)
	}
}

func providerDiscoveryGET(ctx context.Context, endpoint string, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	for key, value := range headers {
		if strings.TrimSpace(value) != "" {
			req.Header.Set(key, value)
		}
	}
	response, err := providerDiscoveryHTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		return nil, &providerDiscoveryHTTPError{Status: response.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, providerDiscoveryMaxBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > providerDiscoveryMaxBodyBytes {
		return nil, errors.New("provider model catalog response is too large")
	}
	return data, nil
}

func providerDiscoveryPositiveInt(value any) int {
	switch number := value.(type) {
	case json.Number:
		parsed, err := number.Int64()
		if err == nil && parsed > 0 {
			return int(parsed)
		}
	case float64:
		if number > 0 {
			return int(number)
		}
	case int:
		if number > 0 {
			return number
		}
	case int64:
		if number > 0 {
			return int(number)
		}
	}
	return 0
}

func providerDiscoveryFirstPositive(values ...any) int {
	for _, value := range values {
		if parsed := providerDiscoveryPositiveInt(value); parsed > 0 {
			return parsed
		}
	}
	return 0
}

func providerDiscoveryRecord(value any) map[string]any {
	record, _ := value.(map[string]any)
	return record
}

func providerDiscoveryNested(record map[string]any, keys ...string) any {
	var current any = record
	for _, key := range keys {
		next, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = next[key]
	}
	return current
}

func providerDiscoveryBoolValue(value any) *bool {
	switch typed := value.(type) {
	case bool:
		result := typed
		return &result
	case map[string]any:
		if supported, ok := typed["supported"].(bool); ok {
			result := supported
			return &result
		}
	}
	return nil
}

func providerDiscoveryFirstBool(record map[string]any, paths ...[]string) *bool {
	for _, path := range paths {
		if value := providerDiscoveryBoolValue(providerDiscoveryNested(record, path...)); value != nil {
			return value
		}
	}
	return nil
}

func providerDiscoveryString(record map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := record[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func providerDiscoveryStringListContains(value any, candidates ...string) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			continue
		}
		for _, candidate := range candidates {
			if strings.EqualFold(strings.TrimSpace(text), candidate) {
				return true
			}
		}
	}
	return false
}

func providerDiscoveryContainsImage(value any) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		if text, ok := item.(string); ok && strings.EqualFold(strings.TrimSpace(text), "image") {
			return true
		}
	}
	return false
}

func providerDiscoveryVision(record map[string]any) *bool {
	if value := providerDiscoveryFirstBool(record,
		[]string{"supportsVision"},
		[]string{"supports_vision"},
		[]string{"capabilities", "vision"},
	); value != nil {
		return value
	}
	architecture := providerDiscoveryRecord(record["architecture"])
	if providerDiscoveryContainsImage(architecture["input_modalities"]) ||
		providerDiscoveryContainsImage(record["input_modalities"]) {
		value := true
		return &value
	}
	for _, raw := range []any{architecture["modality"], record["modality"]} {
		if text, ok := raw.(string); ok {
			input := strings.SplitN(strings.ToLower(text), "->", 2)[0]
			if strings.Contains(input, "image") {
				value := true
				return &value
			}
		}
	}
	return nil
}

func normalizeProviderDiscoveredModel(record map[string]any) (providerDiscoveredModel, bool) {
	id := providerDiscoveryString(record, "id", "modelID", "model")
	if id == "" {
		id = providerDiscoveryString(record, "name")
	}
	if id == "" {
		return providerDiscoveredModel{}, false
	}
	name := providerDiscoveryString(record, "display_name", "displayName", "name")
	if name == "" {
		name = id
	}
	topProvider := providerDiscoveryRecord(record["top_provider"])
	metadata := providerDiscoveryRecord(record["metadata"])
	contextLimit := providerDiscoveryFirstPositive(
		record["max_input_tokens"],
		record["inputTokenLimit"],
		record["input_token_limit"],
		record["context_length"],
		record["contextLength"],
		record["context_window"],
		topProvider["context_length"],
		metadata["context_length"],
	)
	outputLimit := providerDiscoveryFirstPositive(
		record["max_output_tokens"],
		record["outputTokenLimit"],
		record["output_token_limit"],
		record["max_tokens"],
		metadata["max_output_tokens"],
	)
	toolCall := providerDiscoveryFirstBool(record,
		[]string{"toolCall"},
		[]string{"tool_call"},
		[]string{"supportsTools"},
		[]string{"supports_tools"},
		[]string{"capabilities", "tool_calling"},
		[]string{"capabilities", "tools"},
		[]string{"capabilities", "tool_use"},
	)
	if toolCall == nil && providerDiscoveryStringListContains(record["supported_parameters"], "tools", "tool_choice") {
		value := true
		toolCall = &value
	}
	reasoning := providerDiscoveryFirstBool(record,
		[]string{"reasoning"},
		[]string{"supportsReasoning"},
		[]string{"supports_reasoning"},
		[]string{"capabilities", "reasoning"},
		[]string{"capabilities", "thinking"},
		[]string{"thinking"},
	)
	if reasoning == nil && providerDiscoveryStringListContains(record["supported_parameters"], "reasoning", "reasoning_effort", "include_reasoning") {
		value := true
		reasoning = &value
	}
	kind := ""
	if id == jevRouterModelID {
		name = jevRouterDisplayName
		kind = "router"
	}
	return providerDiscoveredModel{
		ID:           id,
		Name:         name,
		Kind:         kind,
		ToolCall:     toolCall,
		Reasoning:    reasoning,
		Vision:       providerDiscoveryVision(record),
		ContextLimit: contextLimit,
		OutputLimit:  outputLimit,
	}, true
}

func providerDiscoveryRecords(data []byte) ([]map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var payload any
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode provider model catalog: %w", err)
	}
	var raw []any
	switch typed := payload.(type) {
	case []any:
		raw = typed
	case map[string]any:
		for _, key := range []string{"data", "models", "items"} {
			if list, ok := typed[key].([]any); ok {
				raw = list
				break
			}
		}
	default:
		return nil, errors.New("provider model catalog returned an unsupported JSON shape")
	}
	if raw == nil {
		return nil, errors.New("provider model catalog did not contain a model list")
	}
	result := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if record, ok := item.(map[string]any); ok {
			result = append(result, record)
		}
	}
	return result, nil
}

func normalizeProviderDiscoveredModels(records []map[string]any) []providerDiscoveredModel {
	byID := map[string]providerDiscoveredModel{}
	for _, record := range records {
		model, ok := normalizeProviderDiscoveredModel(record)
		if !ok {
			continue
		}
		if len(byID) >= providerDiscoveryMaxModels {
			break
		}
		byID[model.ID] = model
	}
	result := make([]providerDiscoveredModel, 0, len(byID))
	for _, model := range byID {
		result = append(result, model)
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i].Name+"\x00"+result[i].ID) < strings.ToLower(result[j].Name+"\x00"+result[j].ID)
	})
	return result
}

func discoverOpenAICompatibleModels(ctx context.Context, baseURL, apiKey string) ([]providerDiscoveredModel, error) {
	endpoint, err := nativeEndpoint(baseURL, "models")
	if err != nil {
		return nil, err
	}
	headers := map[string]string{}
	if apiKey != "" {
		headers["Authorization"] = "Bearer " + apiKey
	}
	data, err := providerDiscoveryGET(ctx, endpoint, headers)
	if err != nil {
		return nil, err
	}
	records, err := providerDiscoveryRecords(data)
	if err != nil {
		return nil, err
	}
	models := normalizeProviderDiscoveredModels(records)
	if len(models) == 0 {
		return nil, errors.New("provider returned no usable models")
	}
	return models, nil
}

func discoverAnthropicModels(ctx context.Context, baseURL, apiKey string) ([]providerDiscoveredModel, error) {
	endpoint, err := nativeEndpoint(baseURL, "models")
	if err != nil {
		return nil, err
	}
	records := make([]map[string]any, 0, 64)
	afterID := ""
	for page := 0; page < 10 && len(records) < providerDiscoveryMaxModels; page++ {
		parsed, parseErr := url.Parse(endpoint)
		if parseErr != nil {
			return nil, parseErr
		}
		query := parsed.Query()
		query.Set("limit", "1000")
		if afterID != "" {
			query.Set("after_id", afterID)
		}
		parsed.RawQuery = query.Encode()
		headers := map[string]string{"anthropic-version": "2023-06-01"}
		if apiKey != "" {
			headers["x-api-key"] = apiKey
		}
		data, requestErr := providerDiscoveryGET(ctx, parsed.String(), headers)
		if requestErr != nil {
			return nil, requestErr
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		var payload struct {
			Data     []map[string]any `json:"data"`
			HasMore  bool             `json:"has_more"`
			LastID   string           `json:"last_id"`
		}
		if err := decoder.Decode(&payload); err != nil {
			return nil, fmt.Errorf("decode Anthropic model catalog: %w", err)
		}
		records = append(records, payload.Data...)
		if !payload.HasMore || strings.TrimSpace(payload.LastID) == "" {
			break
		}
		afterID = strings.TrimSpace(payload.LastID)
	}
	models := normalizeProviderDiscoveredModels(records)
	if len(models) == 0 {
		return nil, errors.New("provider returned no usable models")
	}
	return models, nil
}

func (m *runtimeProviderManager) discoverProviderModels(ctx context.Context, request providerDiscoveryRequest) (providerDiscoveryResponse, error) {
	input, err := normalizeProviderDiscoveryRequest(request)
	if err != nil {
		return providerDiscoveryResponse{}, err
	}
	if input.APIKey == "" && input.ProviderID != "" && m.credentials != nil {
		if key, getErr := m.credentials.Get(input.ProviderID); getErr == nil {
			input.APIKey = strings.TrimSpace(key)
		} else if !errors.Is(getErr, errCredentialNotFound) {
			return providerDiscoveryResponse{}, getErr
		}
	}

	var models []providerDiscoveredModel
	switch input.Protocol {
	case "openai-compatible", "openai-responses":
		models, err = discoverOpenAICompatibleModels(ctx, input.BaseURL, input.APIKey)
	case "anthropic-messages":
		models, err = discoverAnthropicModels(ctx, input.BaseURL, input.APIKey)
	default:
		err = errors.New("unsupported provider protocol")
	}
	if err != nil {
		var discoveryHTTPError *providerDiscoveryHTTPError
		if errors.As(err, &discoveryHTTPError) &&
			(discoveryHTTPError.Status == http.StatusUnauthorized || discoveryHTTPError.Status == http.StatusForbidden) {
			return providerDiscoveryResponse{}, err
		}
		if cached, ok := loadProviderDiscoveryCache(input.ProviderID, input.Protocol, input.BaseURL); ok {
			return providerDiscoveryResponse{
				ProviderID: input.ProviderID,
				Protocol:   input.Protocol,
				BaseURL:    input.BaseURL,
				Models:     cached.Models,
				Source:     "cache",
				Stale:      true,
				FetchedAt:  cached.FetchedAt,
				Warning:    err.Error(),
			}, nil
		}
		return providerDiscoveryResponse{}, err
	}

	fetchedAt := time.Now().UTC().Format(time.RFC3339)
	result := providerDiscoveryResponse{
		ProviderID: input.ProviderID,
		Protocol:   input.Protocol,
		BaseURL:    input.BaseURL,
		Models:     models,
		Source:     "live",
		FetchedAt:  fetchedAt,
	}
	if input.ProviderID != "" {
		if provider, found, getErr := m.store.get(input.ProviderID); getErr == nil && found &&
			provider.Protocol == input.Protocol && strings.TrimRight(provider.BaseURL, "/") == input.BaseURL {
			_ = saveProviderDiscoveryCache(providerDiscoveryCacheEntry{
				ProviderID: input.ProviderID,
				Protocol:   input.Protocol,
				BaseURL:    input.BaseURL,
				Models:     models,
				FetchedAt:  fetchedAt,
			})
		}
	}
	return result, nil
}

func loadProviderDiscoveryCache(providerID, protocol, baseURL string) (providerDiscoveryCacheEntry, bool) {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return providerDiscoveryCacheEntry{}, false
	}
	providerDiscoveryCacheMu.Lock()
	defer providerDiscoveryCacheMu.Unlock()
	data, err := os.ReadFile(providerDiscoveryCachePath())
	if err != nil {
		return providerDiscoveryCacheEntry{}, false
	}
	var stored providerDiscoveryCacheFile
	if json.Unmarshal(data, &stored) != nil || (stored.Version != 0 && stored.Version != providerDiscoveryCacheVersion) {
		return providerDiscoveryCacheEntry{}, false
	}
	for _, entry := range stored.Entries {
		if entry.ProviderID == providerID && entry.Protocol == protocol && strings.TrimRight(entry.BaseURL, "/") == strings.TrimRight(baseURL, "/") {
			entry.Models = append([]providerDiscoveredModel(nil), entry.Models...)
			return entry, true
		}
	}
	return providerDiscoveryCacheEntry{}, false
}

func removeProviderDiscoveryCache(providerID string) error {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return nil
	}
	providerDiscoveryCacheMu.Lock()
	defer providerDiscoveryCacheMu.Unlock()

	path := providerDiscoveryCachePath()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var stored providerDiscoveryCacheFile
	if err := json.Unmarshal(data, &stored); err != nil {
		return fmt.Errorf("decode provider discovery cache: %w", err)
	}
	next := stored.Entries[:0]
	for _, entry := range stored.Entries {
		if entry.ProviderID != providerID {
			next = append(next, entry)
		}
	}
	stored.Entries = next
	if len(stored.Entries) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	stored.Version = providerDiscoveryCacheVersion
	encoded, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, encoded, 0o600)
}

func saveProviderDiscoveryCache(entry providerDiscoveryCacheEntry) error {
	if entry.ProviderID == "" || len(entry.Models) == 0 {
		return nil
	}
	providerDiscoveryCacheMu.Lock()
	defer providerDiscoveryCacheMu.Unlock()
	path := providerDiscoveryCachePath()
	var stored providerDiscoveryCacheFile
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &stored)
	}
	stored.Version = providerDiscoveryCacheVersion
	replaced := false
	for index, existing := range stored.Entries {
		if existing.ProviderID == entry.ProviderID {
			stored.Entries[index] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		stored.Entries = append(stored.Entries, entry)
	}
	sort.Slice(stored.Entries, func(i, j int) bool { return stored.Entries[i].ProviderID < stored.Entries[j].ProviderID })
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), "model-catalog-*.tmp")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return os.WriteFile(path, data, 0o600)
	}
	return nil
}

func writeProviderDiscoveryError(w http.ResponseWriter, err error) {
	var httpErr *providerDiscoveryHTTPError
	switch {
	case errors.As(err, &httpErr):
		status := http.StatusBadGateway
		if httpErr.Status == http.StatusUnauthorized || httpErr.Status == http.StatusForbidden {
			status = http.StatusUnauthorized
		} else if httpErr.Status == http.StatusTooManyRequests {
			status = http.StatusTooManyRequests
		}
		writeJSON(w, status, jsonError{Error: httpErr.Error()})
	case strings.Contains(err.Error(), "provider base URL"),
		strings.Contains(err.Error(), "unsupported provider protocol"),
		strings.Contains(err.Error(), "invalid provider ID"):
		writeJSON(w, http.StatusBadRequest, jsonError{Error: err.Error()})
	default:
		writeJSON(w, http.StatusBadGateway, jsonError{Error: err.Error()})
	}
}

func registerProviderDiscoveryRoutes(mux *http.ServeMux, manager *runtimeProviderManager) {
	mux.HandleFunc("POST /runtime/providers/discover", func(w http.ResponseWriter, r *http.Request) {
		var input providerDiscoveryRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
		if err := decoder.Decode(&input); err != nil {
			writeJSON(w, http.StatusBadRequest, jsonError{Error: "invalid provider discovery JSON body"})
			return
		}
		result, err := manager.discoverProviderModels(r.Context(), input)
		if err != nil {
			writeProviderDiscoveryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
}
