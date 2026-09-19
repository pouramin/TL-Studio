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
)

const providerRegistryVersion = 1

const runtimeHostedProviderID = "kilo"

var runtimeHostedPreferredModels = []string{"kilo-auto/free"}

var runtimeProviderPackages = map[string]string{
	"openai-compatible":  "@ai-sdk/openai-compatible",
	"openai-responses":   "@ai-sdk/openai",
	"anthropic-messages": "@ai-sdk/anthropic",
}

var runtimePackageProtocols = func() map[string]string {
	result := make(map[string]string, len(runtimeProviderPackages))
	for protocol, packageName := range runtimeProviderPackages {
		result[packageName] = protocol
	}
	return result
}()

type tlProviderModel struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ToolCall     bool   `json:"toolCall"`
	Reasoning    bool   `json:"reasoning"`
	ContextLimit int    `json:"contextLimit,omitempty"`
	OutputLimit  int    `json:"outputLimit,omitempty"`
}

type tlProviderDefinition struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Protocol string            `json:"protocol"`
	BaseURL  string            `json:"baseURL"`
	Models   []tlProviderModel `json:"models"`
}

type providerRegistryFile struct {
	Version   int                    `json:"version"`
	Providers []tlProviderDefinition `json:"providers"`
}

type providerRegistryStore struct {
	mu        sync.Mutex
	loaded    bool
	existed   bool
	filePath  string
	providers map[string]tlProviderDefinition
}

func newProviderRegistryStore(filePath string) *providerRegistryStore {
	return &providerRegistryStore{
		filePath:  filePath,
		providers: map[string]tlProviderDefinition{},
	}
}

func providerRegistryPath() string {
	if dir := strings.TrimSpace(os.Getenv("TL_STUDIO_STATE_DIR")); dir != "" {
		return filepath.Join(dir, "providers.json")
	}
	base, err := os.UserConfigDir()
	if err != nil || strings.TrimSpace(base) == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "TL Studio", "providers.json")
}

func cloneProviderDefinition(input tlProviderDefinition) tlProviderDefinition {
	result := input
	result.Models = append([]tlProviderModel(nil), input.Models...)
	return result
}

func (s *providerRegistryStore) loadLocked() error {
	if s.loaded {
		return nil
	}
	s.loaded = true
	s.providers = map[string]tlProviderDefinition{}

	data, err := os.ReadFile(s.filePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	s.existed = true

	var stored providerRegistryFile
	if err := json.Unmarshal(data, &stored); err != nil {
		return fmt.Errorf("decode provider registry: %w", err)
	}
	if stored.Version != 0 && stored.Version != providerRegistryVersion {
		return fmt.Errorf("unsupported provider registry version %d", stored.Version)
	}
	for _, provider := range stored.Providers {
		normalized, err := normalizeProviderDefinition(provider)
		if err != nil {
			continue
		}
		s.providers[normalized.ID] = normalized
	}
	return nil
}

func (s *providerRegistryStore) snapshot() ([]tlProviderDefinition, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return nil, false, err
	}
	result := make([]tlProviderDefinition, 0, len(s.providers))
	for _, provider := range s.providers {
		result = append(result, cloneProviderDefinition(provider))
	}
	sort.Slice(result, func(i, j int) bool {
		left := strings.ToLower(result[i].Name + "\x00" + result[i].ID)
		right := strings.ToLower(result[j].Name + "\x00" + result[j].ID)
		return left < right
	})
	return result, s.existed, nil
}

func (s *providerRegistryStore) get(id string) (tlProviderDefinition, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return tlProviderDefinition{}, false, err
	}
	provider, ok := s.providers[id]
	return cloneProviderDefinition(provider), ok, nil
}

func (s *providerRegistryStore) replace(providers []tlProviderDefinition) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return err
	}
	next := make(map[string]tlProviderDefinition, len(providers))
	for _, provider := range providers {
		normalized, err := normalizeProviderDefinition(provider)
		if err != nil {
			return err
		}
		next[normalized.ID] = normalized
	}
	s.providers = next
	s.existed = true
	return s.persistLocked()
}

func (s *providerRegistryStore) put(provider tlProviderDefinition) error {
	normalized, err := normalizeProviderDefinition(provider)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return err
	}
	s.providers[normalized.ID] = normalized
	s.existed = true
	return s.persistLocked()
}

func (s *providerRegistryStore) remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return err
	}
	delete(s.providers, id)
	s.existed = true
	return s.persistLocked()
}

func (s *providerRegistryStore) persistLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.filePath), 0o700); err != nil {
		return err
	}
	providers := make([]tlProviderDefinition, 0, len(s.providers))
	for _, provider := range s.providers {
		providers = append(providers, cloneProviderDefinition(provider))
	}
	sort.Slice(providers, func(i, j int) bool { return providers[i].ID < providers[j].ID })
	data, err := json.MarshalIndent(providerRegistryFile{Version: providerRegistryVersion, Providers: providers}, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(s.filePath), "providers-*.tmp")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
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
	if err := os.Rename(tempName, s.filePath); err != nil {
		// Windows cannot always replace an existing file with Rename. Fall back
		// to a direct private write rather than losing the validated registry.
		if writeErr := os.WriteFile(s.filePath, data, 0o600); writeErr != nil {
			return err
		}
	}
	return nil
}

func validProviderID(id string) bool {
	if id == "" {
		return false
	}
	for index, r := range id {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || index > 0 && (r == '-' || r == '_') {
			continue
		}
		return false
	}
	return true
}

func normalizeProviderDefinition(input tlProviderDefinition) (tlProviderDefinition, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.Name = strings.TrimSpace(input.Name)
	input.Protocol = strings.TrimSpace(input.Protocol)
	input.BaseURL = strings.TrimSpace(input.BaseURL)

	if !validProviderID(input.ID) {
		return tlProviderDefinition{}, errors.New("provider ID must use lowercase letters, numbers, dashes, or underscores")
	}
	if input.ID == runtimeHostedProviderID {
		return tlProviderDefinition{}, errors.New("provider ID is reserved by the hosted model adapter")
	}
	if input.Name == "" {
		return tlProviderDefinition{}, errors.New("provider display name is required")
	}
	if _, ok := runtimeProviderPackages[input.Protocol]; !ok {
		return tlProviderDefinition{}, errors.New("unsupported provider protocol")
	}
	parsed, err := url.Parse(input.BaseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return tlProviderDefinition{}, errors.New("provider base URL must be a valid http(s) URL")
	}
	input.BaseURL = strings.TrimRight(parsed.String(), "/")
	if len(input.Models) == 0 {
		return tlProviderDefinition{}, errors.New("provider must define at least one model")
	}

	seen := map[string]bool{}
	models := make([]tlProviderModel, 0, len(input.Models))
	for _, model := range input.Models {
		model.ID = strings.TrimSpace(model.ID)
		model.Name = strings.TrimSpace(model.Name)
		if model.ID == "" {
			return tlProviderDefinition{}, errors.New("model ID is required")
		}
		if seen[model.ID] {
			return tlProviderDefinition{}, fmt.Errorf("duplicate model ID %q", model.ID)
		}
		if model.Name == "" {
			model.Name = model.ID
		}
		if model.ContextLimit < 0 || model.OutputLimit < 0 {
			return tlProviderDefinition{}, errors.New("model limits cannot be negative")
		}
		seen[model.ID] = true
		models = append(models, model)
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	input.Models = models
	return input, nil
}

type runtimeProviderError struct {
	Status int
	Body   string
}

func (e *runtimeProviderError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("runtime provider request failed with status %d", e.Status)
	}
	return fmt.Sprintf("runtime provider request failed with status %d", e.Status)
}

type runtimeProviderManager struct {
	state       *appState
	target      *url.URL
	username    string
	password    string
	store       *providerRegistryStore
	client      *http.Client
	bootstrapMu sync.Mutex
	bootstrapped bool
}

func newRuntimeProviderManager(state *appState, backendURL, username, password string) (*runtimeProviderManager, error) {
	target, err := url.Parse(backendURL)
	if err != nil {
		return nil, err
	}
	return &runtimeProviderManager{
		state:    state,
		target:   target,
		username: username,
		password: password,
		store:    newProviderRegistryStore(providerRegistryPath()),
		client:   &http.Client{},
	}, nil
}

func (m *runtimeProviderManager) requestRaw(ctx context.Context, method, route string, query url.Values, body any) (json.RawMessage, error) {
	target := *m.target
	target.Path = route
	target.RawQuery = query.Encode()

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, target.String(), reader)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(m.username, m.password)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if project := m.state.projectPath(); project != "" {
		req.Header.Set("x-kilo-directory", strings.ReplaceAll(url.QueryEscape(project), "+", "%20"))
	}

	response, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, &runtimeProviderError{Status: response.StatusCode, Body: strings.TrimSpace(string(data))}
	}
	return json.RawMessage(data), nil
}

func unwrapRuntimePayload(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(raw, &envelope) == nil {
		if data, ok := envelope["data"]; ok && len(data) > 0 && string(data) != "null" {
			return data
		}
	}
	return raw
}

func (m *runtimeProviderManager) runtimeQuery(directory string) url.Values {
	values := url.Values{}
	if strings.TrimSpace(directory) == "" {
		directory = m.state.projectPath()
	}
	if strings.TrimSpace(directory) != "" {
		values.Set("directory", directory)
	}
	return values
}

func (m *runtimeProviderManager) fetchOverlay(ctx context.Context) (map[string]any, error) {
	query := m.runtimeQuery("")
	query.Set("scope", "global")
	raw, err := m.requestRaw(ctx, http.MethodGet, "/config/overlay", query, nil)
	if err != nil {
		return nil, err
	}
	var overlay map[string]any
	if err := json.Unmarshal(unwrapRuntimePayload(raw), &overlay); err != nil {
		return nil, fmt.Errorf("decode runtime provider overlay: %w", err)
	}
	return overlay, nil
}

func effectiveProviderMap(overlay map[string]any) map[string]any {
	effective, _ := overlay["effective"].(map[string]any)
	current, _ := effective["provider"].(map[string]any)
	result := make(map[string]any, len(current)+1)
	for id, value := range current {
		result[id] = value
	}
	return result
}

func effectiveDisabledProviders(overlay map[string]any) []string {
	effective, _ := overlay["effective"].(map[string]any)
	values, _ := effective["disabled_providers"].([]any)
	result := make([]string, 0, len(values))
	for _, value := range values {
		if item, ok := value.(string); ok && item != "" {
			result = append(result, item)
		}
	}
	return result
}

func withoutString(values []string, target string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != target {
			result = append(result, value)
		}
	}
	return result
}

func runtimeConfigForProvider(provider tlProviderDefinition) map[string]any {
	models := map[string]any{}
	for _, model := range provider.Models {
		value := map[string]any{
			"name":      model.Name,
			"tool_call": model.ToolCall,
			"reasoning": model.Reasoning,
		}
		limits := map[string]any{}
		if model.ContextLimit > 0 {
			limits["context"] = model.ContextLimit
		}
		if model.OutputLimit > 0 {
			limits["output"] = model.OutputLimit
		}
		if len(limits) > 0 {
			value["limit"] = limits
		}
		models[model.ID] = value
	}
	return map[string]any{
		"name": provider.Name,
		"npm":  runtimeProviderPackages[provider.Protocol],
		"options": map[string]any{
			"baseURL": provider.BaseURL,
		},
		"models": models,
	}
}

func (m *runtimeProviderManager) patchRuntimeProviders(ctx context.Context, providers map[string]any, disabled []string) error {
	body := map[string]any{
		"scope": "global",
		"set": map[string]any{
			"provider":           providers,
			"disabled_providers": disabled,
		},
	}
	_, err := m.requestRaw(ctx, http.MethodPatch, "/config/overlay", m.runtimeQuery(""), body)
	return err
}

func (m *runtimeProviderManager) syncProvider(ctx context.Context, provider tlProviderDefinition) error {
	overlay, err := m.fetchOverlay(ctx)
	if err != nil {
		return err
	}
	providers := effectiveProviderMap(overlay)
	providers[provider.ID] = runtimeConfigForProvider(provider)
	disabled := withoutString(effectiveDisabledProviders(overlay), provider.ID)
	return m.patchRuntimeProviders(ctx, providers, disabled)
}

func (m *runtimeProviderManager) syncAll(ctx context.Context, providers []tlProviderDefinition) error {
	if len(providers) == 0 {
		return nil
	}
	overlay, err := m.fetchOverlay(ctx)
	if err != nil {
		return err
	}
	current := effectiveProviderMap(overlay)
	disabled := effectiveDisabledProviders(overlay)
	for _, provider := range providers {
		current[provider.ID] = runtimeConfigForProvider(provider)
		disabled = withoutString(disabled, provider.ID)
	}
	if err := m.patchRuntimeProviders(ctx, current, disabled); err != nil {
		return err
	}
	return m.dispose(ctx)
}

func (m *runtimeProviderManager) deleteRuntimeProvider(ctx context.Context, id string) error {
	overlay, err := m.fetchOverlay(ctx)
	if err != nil {
		return err
	}
	providers := effectiveProviderMap(overlay)
	providers[id] = nil
	if err := m.patchRuntimeProviders(ctx, providers, effectiveDisabledProviders(overlay)); err != nil {
		return err
	}
	_, authErr := m.requestRaw(ctx, http.MethodDelete, "/auth/"+url.PathEscape(id), url.Values{}, nil)
	if authErr != nil {
		var runtimeErr *runtimeProviderError
		if !errors.As(authErr, &runtimeErr) || runtimeErr.Status != http.StatusNotFound {
			return authErr
		}
	}
	return m.dispose(ctx)
}

func (m *runtimeProviderManager) setCredential(ctx context.Context, id, key string) error {
	if strings.TrimSpace(key) == "" {
		return nil
	}
	_, err := m.requestRaw(ctx, http.MethodPut, "/auth/"+url.PathEscape(id), url.Values{}, map[string]any{
		"type": "api",
		"key":  key,
	})
	return err
}

func (m *runtimeProviderManager) dispose(ctx context.Context) error {
	_, err := m.requestRaw(ctx, http.MethodPost, "/global/dispose", url.Values{}, nil)
	return err
}

func intFromAny(value any) int {
	switch number := value.(type) {
	case float64:
		return int(number)
	case int:
		return number
	case json.Number:
		result, _ := number.Int64()
		return int(result)
	default:
		return 0
	}
}

func importRuntimeProviders(overlay map[string]any) []tlProviderDefinition {
	effective, _ := overlay["effective"].(map[string]any)
	providers, _ := effective["provider"].(map[string]any)
	result := []tlProviderDefinition{}
	for id, rawProvider := range providers {
		config, _ := rawProvider.(map[string]any)
		if config == nil {
			continue
		}
		packageName, _ := config["npm"].(string)
		protocol, ok := runtimePackageProtocols[packageName]
		if !ok {
			continue
		}
		options, _ := config["options"].(map[string]any)
		baseURL, _ := options["baseURL"].(string)
		name, _ := config["name"].(string)
		if name == "" {
			name = id
		}
		rawModels, _ := config["models"].(map[string]any)
		models := make([]tlProviderModel, 0, len(rawModels))
		for modelID, rawModel := range rawModels {
			modelConfig, _ := rawModel.(map[string]any)
			if modelConfig == nil {
				continue
			}
			modelName, _ := modelConfig["name"].(string)
			toolCall, _ := modelConfig["tool_call"].(bool)
			reasoning, _ := modelConfig["reasoning"].(bool)
			limit, _ := modelConfig["limit"].(map[string]any)
			models = append(models, tlProviderModel{
				ID:           modelID,
				Name:         modelName,
				ToolCall:     toolCall,
				Reasoning:    reasoning,
				ContextLimit: intFromAny(limit["context"]),
				OutputLimit:  intFromAny(limit["output"]),
			})
		}
		provider, err := normalizeProviderDefinition(tlProviderDefinition{
			ID:       id,
			Name:     name,
			Protocol: protocol,
			BaseURL:  baseURL,
			Models:   models,
		})
		if err == nil {
			result = append(result, provider)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func (m *runtimeProviderManager) ensureBootstrapped(ctx context.Context) error {
	m.bootstrapMu.Lock()
	defer m.bootstrapMu.Unlock()
	if m.bootstrapped {
		return nil
	}

	providers, existed, err := m.store.snapshot()
	if err != nil {
		return err
	}
	if !existed {
		overlay, err := m.fetchOverlay(ctx)
		if err != nil {
			return err
		}
		providers = importRuntimeProviders(overlay)
		if err := m.store.replace(providers); err != nil {
			return err
		}
	}
	if err := m.syncAll(ctx, providers); err != nil {
		return err
	}
	m.bootstrapped = true
	return nil
}

type catalogModel struct {
	Name    string `json:"name"`
	Enabled *bool  `json:"enabled,omitempty"`
	Variant any    `json:"variant,omitempty"`
}

type catalogProvider struct {
	ID     string                  `json:"id"`
	Name   string                  `json:"name"`
	Source string                  `json:"source"`
	Models map[string]catalogModel `json:"models"`
}

type providerCatalogResponse struct {
	All       []catalogProvider `json:"all"`
	Connected []string          `json:"connected"`
	Default   map[string]string `json:"default"`
	Failed    []json.RawMessage `json:"failed,omitempty"`
	Hosted    struct {
		ProviderID      string   `json:"providerID"`
		PreferredModels []string `json:"preferredModels"`
	} `json:"hosted"`
}

func normalizeCatalogModel(id string, raw json.RawMessage) (string, catalogModel, bool) {
	var value map[string]any
	if json.Unmarshal(raw, &value) != nil {
		return "", catalogModel{}, false
	}
	if id == "" {
		for _, key := range []string{"id", "modelID", "name"} {
			if candidate, ok := value[key].(string); ok && candidate != "" {
				id = candidate
				break
			}
		}
	}
	if id == "" {
		return "", catalogModel{}, false
	}
	model := catalogModel{Name: id}
	if name, ok := value["name"].(string); ok && name != "" {
		model.Name = name
	}
	if enabled, ok := value["enabled"].(bool); ok {
		model.Enabled = &enabled
	}
	if variant, ok := value["variant"]; ok {
		model.Variant = variant
	}
	return id, model, true
}

func normalizeCatalogProvider(raw json.RawMessage, managed map[string]bool) (catalogProvider, bool) {
	var value struct {
		ID     string          `json:"id"`
		Name   string          `json:"name"`
		Models json.RawMessage `json:"models"`
	}
	if json.Unmarshal(raw, &value) != nil || strings.TrimSpace(value.ID) == "" {
		return catalogProvider{}, false
	}
	result := catalogProvider{
		ID:     value.ID,
		Name:   value.Name,
		Source: "runtime",
		Models: map[string]catalogModel{},
	}
	if result.Name == "" {
		result.Name = result.ID
	}
	if managed[result.ID] {
		result.Source = "custom"
	} else if result.ID == runtimeHostedProviderID {
		result.Source = "hosted"
	}
	if len(value.Models) > 0 {
		var objectModels map[string]json.RawMessage
		if json.Unmarshal(value.Models, &objectModels) == nil {
			for id, rawModel := range objectModels {
				if modelID, model, ok := normalizeCatalogModel(id, rawModel); ok {
					result.Models[modelID] = model
				}
			}
		} else {
			var arrayModels []json.RawMessage
			if json.Unmarshal(value.Models, &arrayModels) == nil {
				for _, rawModel := range arrayModels {
					if modelID, model, ok := normalizeCatalogModel("", rawModel); ok {
						result.Models[modelID] = model
					}
				}
			}
		}
	}
	return result, true
}

func (m *runtimeProviderManager) catalog(ctx context.Context, directory string) (providerCatalogResponse, error) {
	if err := m.ensureBootstrapped(ctx); err != nil {
		return providerCatalogResponse{}, err
	}
	raw, err := m.requestRaw(ctx, http.MethodGet, "/provider", m.runtimeQuery(directory), nil)
	if err != nil {
		return providerCatalogResponse{}, err
	}
	var upstream struct {
		All       []json.RawMessage `json:"all"`
		Connected []string          `json:"connected"`
		Default   map[string]string `json:"default"`
		Failed    []json.RawMessage `json:"failed"`
	}
	if err := json.Unmarshal(unwrapRuntimePayload(raw), &upstream); err != nil {
		return providerCatalogResponse{}, fmt.Errorf("decode runtime provider catalog: %w", err)
	}

	managedDefinitions, _, err := m.store.snapshot()
	if err != nil {
		return providerCatalogResponse{}, err
	}
	managed := map[string]bool{}
	for _, provider := range managedDefinitions {
		managed[provider.ID] = true
	}

	result := providerCatalogResponse{
		Connected: append([]string(nil), upstream.Connected...),
		Default:   upstream.Default,
		Failed:    upstream.Failed,
	}
	if result.Default == nil {
		result.Default = map[string]string{}
	}
	result.Hosted.ProviderID = runtimeHostedProviderID
	result.Hosted.PreferredModels = append([]string(nil), runtimeHostedPreferredModels...)
	for _, rawProvider := range upstream.All {
		if provider, ok := normalizeCatalogProvider(rawProvider, managed); ok {
			result.All = append(result.All, provider)
		}
	}
	sort.Slice(result.All, func(i, j int) bool {
		return strings.ToLower(result.All[i].Name+"\x00"+result.All[i].ID) < strings.ToLower(result.All[j].Name+"\x00"+result.All[j].ID)
	})
	return result, nil
}

type providerWriteRequest struct {
	Provider tlProviderDefinition `json:"provider"`
	APIKey   string               `json:"apiKey,omitempty"`
}

type providerConfigResponse struct {
	Providers []tlProviderDefinition `json:"providers"`
}

type hostedStatusResponse struct {
	Authenticated   bool     `json:"authenticated"`
	Type            string   `json:"type"`
	OrganizationID  string   `json:"organizationId"`
	ProviderID      string   `json:"providerID"`
	PreferredModels []string `json:"preferredModels"`
}

func writeProviderManagerError(w http.ResponseWriter, err error) {
	var runtimeErr *runtimeProviderError
	if errors.As(err, &runtimeErr) {
		writeJSON(w, http.StatusBadGateway, jsonError{Error: fmt.Sprintf("Runtime provider operation failed (%d)", runtimeErr.Status)})
		return
	}
	writeJSON(w, http.StatusInternalServerError, jsonError{Error: err.Error()})
}

func writeUnwrappedJSON(w http.ResponseWriter, raw json.RawMessage) {
	raw = unwrapRuntimePayload(raw)
	if len(raw) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{})
		return
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		writeJSON(w, http.StatusOK, map[string]any{})
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func registerRuntimeProviderRoutes(mux *http.ServeMux, manager *runtimeProviderManager) {
	mux.HandleFunc("GET /runtime/providers/catalog", func(w http.ResponseWriter, r *http.Request) {
		catalog, err := manager.catalog(r.Context(), r.URL.Query().Get("directory"))
		if err != nil {
			writeProviderManagerError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, catalog)
	})

	mux.HandleFunc("GET /runtime/providers/config", func(w http.ResponseWriter, r *http.Request) {
		if err := manager.ensureBootstrapped(r.Context()); err != nil {
			writeProviderManagerError(w, err)
			return
		}
		providers, _, err := manager.store.snapshot()
		if err != nil {
			writeProviderManagerError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, providerConfigResponse{Providers: providers})
	})

	mux.HandleFunc("PUT /runtime/providers/config/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := manager.ensureBootstrapped(r.Context()); err != nil {
			writeProviderManagerError(w, err)
			return
		}
		var input providerWriteRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 2<<20)).Decode(&input); err != nil {
			writeJSON(w, http.StatusBadRequest, jsonError{Error: "invalid provider JSON body"})
			return
		}
		input.Provider.ID = strings.TrimSpace(input.Provider.ID)
		pathID := strings.TrimSpace(r.PathValue("id"))
		if input.Provider.ID == "" {
			input.Provider.ID = pathID
		}
		if input.Provider.ID != pathID {
			writeJSON(w, http.StatusBadRequest, jsonError{Error: "provider ID does not match request path"})
			return
		}
		provider, err := normalizeProviderDefinition(input.Provider)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, jsonError{Error: err.Error()})
			return
		}
		if err := manager.store.put(provider); err != nil {
			writeProviderManagerError(w, err)
			return
		}
		if err := manager.syncProvider(r.Context(), provider); err != nil {
			writeProviderManagerError(w, err)
			return
		}
		if err := manager.setCredential(r.Context(), provider.ID, input.APIKey); err != nil {
			writeProviderManagerError(w, err)
			return
		}
		if err := manager.dispose(r.Context()); err != nil {
			writeProviderManagerError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, provider)
	})

	mux.HandleFunc("DELETE /runtime/providers/config/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := manager.ensureBootstrapped(r.Context()); err != nil {
			writeProviderManagerError(w, err)
			return
		}
		id := strings.TrimSpace(r.PathValue("id"))
		if !validProviderID(id) || id == runtimeHostedProviderID {
			writeJSON(w, http.StatusBadRequest, jsonError{Error: "invalid provider ID"})
			return
		}
		if _, ok, err := manager.store.get(id); err != nil {
			writeProviderManagerError(w, err)
			return
		} else if !ok {
			writeJSON(w, http.StatusNotFound, jsonError{Error: "provider is not managed by TL Studio"})
			return
		}
		if err := manager.deleteRuntimeProvider(r.Context(), id); err != nil {
			writeProviderManagerError(w, err)
			return
		}
		if err := manager.store.remove(id); err != nil {
			writeProviderManagerError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"removed": id})
	})

	mux.HandleFunc("GET /runtime/hosted/status", func(w http.ResponseWriter, r *http.Request) {
		raw, err := manager.requestRaw(r.Context(), http.MethodGet, "/kilo/auth-status", manager.runtimeQuery(r.URL.Query().Get("directory")), nil)
		if err != nil {
			writeProviderManagerError(w, err)
			return
		}
		var upstream struct {
			Authenticated  bool   `json:"authenticated"`
			Type           string `json:"type"`
			OrganizationID string `json:"organizationId"`
		}
		if err := json.Unmarshal(unwrapRuntimePayload(raw), &upstream); err != nil {
			writeProviderManagerError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, hostedStatusResponse{
			Authenticated:   upstream.Authenticated,
			Type:            upstream.Type,
			OrganizationID:  upstream.OrganizationID,
			ProviderID:      runtimeHostedProviderID,
			PreferredModels: append([]string(nil), runtimeHostedPreferredModels...),
		})
	})

	mux.HandleFunc("POST /runtime/hosted/authorize", func(w http.ResponseWriter, r *http.Request) {
		raw, err := manager.requestRaw(r.Context(), http.MethodPost, "/provider/kilo/oauth/authorize", manager.runtimeQuery(r.URL.Query().Get("directory")), map[string]any{"method": 0})
		if err != nil {
			writeProviderManagerError(w, err)
			return
		}
		writeUnwrappedJSON(w, raw)
	})

	mux.HandleFunc("POST /runtime/hosted/callback", func(w http.ResponseWriter, r *http.Request) {
		raw, err := manager.requestRaw(r.Context(), http.MethodPost, "/provider/kilo/oauth/callback", manager.runtimeQuery(r.URL.Query().Get("directory")), map[string]any{"method": 0})
		if err != nil {
			writeProviderManagerError(w, err)
			return
		}
		writeUnwrappedJSON(w, raw)
	})

	mux.HandleFunc("DELETE /runtime/hosted", func(w http.ResponseWriter, r *http.Request) {
		if _, err := manager.requestRaw(r.Context(), http.MethodDelete, "/auth/kilo", url.Values{}, nil); err != nil {
			writeProviderManagerError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"disconnected": true})
	})
}
