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
	jevRouterModelID              = "typesafe/jev-router"
	jevRouterDisplayName          = "Jev Router"
	openRouterBaseURL             = "https://openrouter.ai/api/v1"
	jevDecisionEngineID           = "jev"
	jevDecisionDefaultModel       = "~typesafe/jev-latest"
	decisionEngineConfigVersion   = 1
	decisionEngineRequestTimeout  = 20 * time.Second
	decisionEngineMaxResponseSize = 8 << 20
)

type decisionQuestion struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
	Criteria     any    `json:"criteria,omitempty"`
}

type decisionEvaluateRequest struct {
	State     any                         `json:"state"`
	Questions map[string]decisionQuestion `json:"questions"`
}

type decisionAnswer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice,omitempty"`
	Score         *float64           `json:"score,omitempty"`
	Noul          *float64           `json:"noul,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Confidence    *float64           `json:"confidence,omitempty"`
	Legend        map[string]string  `json:"legend,omitempty"`
}

type decisionUsage struct {
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	Cost         float64 `json:"cost"`
}

type decisionEvaluateResponse struct {
	ID       string                    `json:"id,omitempty"`
	Model    string                    `json:"model,omitempty"`
	Provider string                    `json:"provider,omitempty"`
	Answers  map[string]decisionAnswer `json:"answers"`
	Usage    decisionUsage             `json:"usage"`
}

type decisionEngine interface {
	Evaluate(context.Context, decisionEvaluateRequest) (decisionEvaluateResponse, error)
}

type decisionEngineConfig struct {
	Version int    `json:"version"`
	Engine  string `json:"engine"`
	Model   string `json:"model,omitempty"`
}

type decisionEngineStatus struct {
	Engine             string `json:"engine"`
	Model              string `json:"model,omitempty"`
	ProviderConfigured bool   `json:"providerConfigured"`
	ProviderID         string `json:"providerID,omitempty"`
	ProviderName       string `json:"providerName,omitempty"`
	Paid               bool   `json:"paid"`
	AutomaticUse       bool   `json:"automaticUse"`
}

type decisionEngineError struct {
	Kind   string
	Status int
	Err    error
}

func (e *decisionEngineError) Error() string {
	if e == nil || e.Err == nil {
		return "decision engine error"
	}
	return e.Err.Error()
}

func (e *decisionEngineError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type openRouterDecisionEngine struct {
	endpoint   string
	model      string
	apiKey     string
	httpClient *http.Client
}

func (e *openRouterDecisionEngine) Evaluate(ctx context.Context, input decisionEvaluateRequest) (decisionEvaluateResponse, error) {
	if err := validateDecisionRequest(input); err != nil {
		return decisionEvaluateResponse{}, &decisionEngineError{Kind: "validation", Status: http.StatusBadRequest, Err: err}
	}
	payload := map[string]any{
		"model":     e.model,
		"state":     input.State,
		"questions": input.Questions,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return decisionEvaluateResponse{}, &decisionEngineError{Kind: "validation", Status: http.StatusBadRequest, Err: err}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(data))
	if err != nil {
		return decisionEvaluateResponse{}, &decisionEngineError{Kind: "network", Status: http.StatusBadGateway, Err: err}
	}
	req.Header.Set("Authorization", "Bearer "+e.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	response, err := e.httpClient.Do(req)
	if err != nil {
		kind := "network"
		status := http.StatusBadGateway
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			kind = "timeout"
			status = http.StatusGatewayTimeout
		}
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			kind = "cancelled"
			status = 499
		}
		return decisionEvaluateResponse{}, &decisionEngineError{Kind: kind, Status: status, Err: err}
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, decisionEngineMaxResponseSize+1))
	if err != nil {
		return decisionEvaluateResponse{}, &decisionEngineError{Kind: "response", Status: http.StatusBadGateway, Err: err}
	}
	if len(body) > decisionEngineMaxResponseSize {
		return decisionEvaluateResponse{}, &decisionEngineError{Kind: "response", Status: http.StatusBadGateway, Err: errors.New("decision response is too large")}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		kind := "upstream"
		status := http.StatusBadGateway
		switch response.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			kind = "authentication"
			status = http.StatusUnauthorized
		case http.StatusTooManyRequests:
			kind = "rate_limit"
			status = http.StatusTooManyRequests
		case http.StatusBadRequest, http.StatusUnprocessableEntity:
			kind = "validation"
			status = http.StatusBadRequest
		case http.StatusRequestTimeout, http.StatusGatewayTimeout:
			kind = "timeout"
			status = http.StatusGatewayTimeout
		}
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = fmt.Sprintf("OpenRouter Decisions API returned status %d", response.StatusCode)
		}
		return decisionEvaluateResponse{}, &decisionEngineError{Kind: kind, Status: status, Err: errors.New(message)}
	}

	var result decisionEvaluateResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return decisionEvaluateResponse{}, &decisionEngineError{Kind: "response", Status: http.StatusBadGateway, Err: fmt.Errorf("decode decision response: %w", err)}
	}
	if len(result.Answers) == 0 {
		return decisionEvaluateResponse{}, &decisionEngineError{Kind: "response", Status: http.StatusBadGateway, Err: errors.New("decision response contained no answers")}
	}
	for id, question := range input.Questions {
		answer, ok := result.Answers[id]
		if !ok {
			return decisionEvaluateResponse{}, &decisionEngineError{Kind: "response", Status: http.StatusBadGateway, Err: fmt.Errorf("decision response omitted answer %q", id)}
		}
		if answer.Type != question.Type {
			return decisionEvaluateResponse{}, &decisionEngineError{Kind: "response", Status: http.StatusBadGateway, Err: fmt.Errorf("decision response type mismatch for %q", id)}
		}
	}
	return result, nil
}

func validateDecisionRequest(input decisionEvaluateRequest) error {
	if input.State == nil {
		return errors.New("decision state is required")
	}
	if len(input.Questions) == 0 {
		return errors.New("at least one decision question is required")
	}
	if len(input.Questions) > 1000 {
		return errors.New("too many decision questions")
	}
	for id, question := range input.Questions {
		if strings.TrimSpace(id) == "" {
			return errors.New("decision question ID is required")
		}
		question.Type = strings.ToLower(strings.TrimSpace(question.Type))
		if strings.TrimSpace(question.Instructions) == "" {
			return fmt.Errorf("decision question %q requires instructions", id)
		}
		switch question.Type {
		case "choice":
			criteria, ok := question.Criteria.(map[string]any)
			if !ok || len(criteria) < 2 || len(criteria) > 255 {
				return fmt.Errorf("choice question %q requires 2 to 255 criteria", id)
			}
		case "score":
			criteria, ok := question.Criteria.([]any)
			if !ok || len(criteria) < 2 || len(criteria) > 10 {
				return fmt.Errorf("score question %q requires 2 to 10 ordered criteria", id)
			}
		case "noul":
			if question.Criteria != nil {
				if _, ok := question.Criteria.(map[string]any); !ok {
					return fmt.Errorf("noul question %q criteria must be an object when provided", id)
				}
			}
		default:
			return fmt.Errorf("decision question %q has unsupported type %q", id, question.Type)
		}
	}
	return nil
}

func canonicalProviderURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, errors.New("invalid provider URL")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed, nil
}

func isOpenRouterBaseURL(raw string) bool {
	parsed, err := canonicalProviderURL(raw)
	if err != nil {
		return false
	}
	return strings.EqualFold(parsed.Scheme, "https") &&
		strings.EqualFold(parsed.Hostname(), "openrouter.ai") &&
		parsed.Path == "/api/v1"
}

func openRouterDecisionEndpoint(raw string) (string, error) {
	parsed, err := canonicalProviderURL(raw)
	if err != nil || !isOpenRouterBaseURL(raw) {
		return "", errors.New("configured provider is not the official OpenRouter API")
	}
	parsed.Path = "/api/alpha/decisions"
	return parsed.String(), nil
}

func (m *runtimeProviderManager) findOpenRouterProvider() (tlProviderDefinition, string, error) {
	providers, _, err := m.store.snapshot()
	if err != nil {
		return tlProviderDefinition{}, "", err
	}
	candidates := make([]tlProviderDefinition, 0)
	for _, provider := range providers {
		if isOpenRouterBaseURL(provider.BaseURL) {
			candidates = append(candidates, provider)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].ID == "openrouter" {
			return true
		}
		if candidates[j].ID == "openrouter" {
			return false
		}
		return candidates[i].ID < candidates[j].ID
	})
	if len(candidates) == 0 {
		return tlProviderDefinition{}, "", &decisionEngineError{
			Kind: "credential", Status: http.StatusUnauthorized,
			Err: errors.New("configure OpenRouter in Settings → Providers before enabling Jev decisions"),
		}
	}
	if m.credentials == nil {
		return tlProviderDefinition{}, "", &decisionEngineError{
			Kind: "credential", Status: http.StatusUnauthorized,
			Err: errors.New("TL Studio credential store is unavailable"),
		}
	}
	for _, provider := range candidates {
		key, getErr := m.credentials.Get(provider.ID)
		if getErr == nil && strings.TrimSpace(key) != "" {
			return provider, strings.TrimSpace(key), nil
		}
		if getErr != nil && !errors.Is(getErr, errCredentialNotFound) {
			return tlProviderDefinition{}, "", getErr
		}
	}
	return candidates[0], "", &decisionEngineError{
		Kind: "credential", Status: http.StatusUnauthorized,
		Err: fmt.Errorf("OpenRouter provider %q has no TL Studio-owned API key", candidates[0].ID),
	}
}

var decisionEngineConfigMu sync.Mutex

func decisionEngineConfigPath() string {
	return filepath.Join(tlStudioStateDirectory(), "decision-engine.json")
}

func defaultDecisionEngineConfig() decisionEngineConfig {
	return decisionEngineConfig{Version: decisionEngineConfigVersion, Engine: "off", Model: jevDecisionDefaultModel}
}

func normalizeDecisionEngineConfig(input decisionEngineConfig) (decisionEngineConfig, error) {
	input.Version = decisionEngineConfigVersion
	input.Engine = strings.ToLower(strings.TrimSpace(input.Engine))
	if input.Engine == "" {
		input.Engine = "off"
	}
	switch input.Engine {
	case "off":
		input.Model = jevDecisionDefaultModel
	case jevDecisionEngineID:
		model := strings.TrimSpace(input.Model)
		if model == "" {
			model = jevDecisionDefaultModel
		}
		if model != jevDecisionDefaultModel && model != "typesafe/jev-1.13" {
			return decisionEngineConfig{}, errors.New("unsupported Jev decision model")
		}
		input.Model = model
	default:
		return decisionEngineConfig{}, errors.New("unsupported decision engine")
	}
	return input, nil
}

func loadDecisionEngineConfig() (decisionEngineConfig, error) {
	decisionEngineConfigMu.Lock()
	defer decisionEngineConfigMu.Unlock()
	data, err := os.ReadFile(decisionEngineConfigPath())
	if errors.Is(err, os.ErrNotExist) {
		return defaultDecisionEngineConfig(), nil
	}
	if err != nil {
		return decisionEngineConfig{}, err
	}
	var config decisionEngineConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return decisionEngineConfig{}, fmt.Errorf("decode decision engine config: %w", err)
	}
	if config.Version != 0 && config.Version != decisionEngineConfigVersion {
		return decisionEngineConfig{}, fmt.Errorf("unsupported decision engine config version %d", config.Version)
	}
	return normalizeDecisionEngineConfig(config)
}

func saveDecisionEngineConfig(input decisionEngineConfig) (decisionEngineConfig, error) {
	config, err := normalizeDecisionEngineConfig(input)
	if err != nil {
		return decisionEngineConfig{}, err
	}
	decisionEngineConfigMu.Lock()
	defer decisionEngineConfigMu.Unlock()
	path := decisionEngineConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return decisionEngineConfig{}, err
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return decisionEngineConfig{}, err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), "decision-engine-*.tmp")
	if err != nil {
		return decisionEngineConfig{}, err
	}
	name := temp.Name()
	defer os.Remove(name)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return decisionEngineConfig{}, err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return decisionEngineConfig{}, err
	}
	if err := temp.Close(); err != nil {
		return decisionEngineConfig{}, err
	}
	if err := os.Rename(name, path); err != nil {
		if writeErr := os.WriteFile(path, data, 0o600); writeErr != nil {
			return decisionEngineConfig{}, err
		}
	}
	return config, nil
}

type decisionEngineService struct {
	providers *runtimeProviderManager
	client    *http.Client
}

func sameDecisionOrigin(left, right *url.URL) bool {
	if left == nil || right == nil {
		return false
	}
	leftPort := left.Port()
	rightPort := right.Port()
	if leftPort == "" {
		if strings.EqualFold(left.Scheme, "https") {
			leftPort = "443"
		} else if strings.EqualFold(left.Scheme, "http") {
			leftPort = "80"
		}
	}
	if rightPort == "" {
		if strings.EqualFold(right.Scheme, "https") {
			rightPort = "443"
		} else if strings.EqualFold(right.Scheme, "http") {
			rightPort = "80"
		}
	}
	return strings.EqualFold(left.Scheme, right.Scheme) &&
		strings.EqualFold(left.Hostname(), right.Hostname()) &&
		leftPort == rightPort
}

func newDecisionEngineHTTPClient() *http.Client {
	return &http.Client{
		Timeout: decisionEngineRequestTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many Decision API redirects")
			}
			if len(via) > 0 && !sameDecisionOrigin(via[0].URL, req.URL) {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
}

func newDecisionEngineService(providers *runtimeProviderManager) *decisionEngineService {
	return &decisionEngineService{
		providers: providers,
		client:    newDecisionEngineHTTPClient(),
	}
}

func (s *decisionEngineService) status() (decisionEngineStatus, error) {
	config, err := loadDecisionEngineConfig()
	if err != nil {
		return decisionEngineStatus{}, err
	}
	status := decisionEngineStatus{
		Engine:       config.Engine,
		Model:        config.Model,
		Paid:         config.Engine == jevDecisionEngineID,
		AutomaticUse: false,
	}
	if s.providers == nil {
		return status, nil
	}
	provider, key, providerErr := s.providers.findOpenRouterProvider()
	if providerErr == nil {
		status.ProviderConfigured = strings.TrimSpace(key) != ""
		status.ProviderID = provider.ID
		status.ProviderName = provider.Name
		return status, nil
	}
	var typed *decisionEngineError
	if errors.As(providerErr, &typed) && typed.Kind == "credential" {
		return status, nil
	}
	return decisionEngineStatus{}, providerErr
}

func (s *decisionEngineService) configure(input decisionEngineConfig) (decisionEngineStatus, error) {
	config, err := normalizeDecisionEngineConfig(input)
	if err != nil {
		return decisionEngineStatus{}, &decisionEngineError{Kind: "validation", Status: http.StatusBadRequest, Err: err}
	}
	if config.Engine == jevDecisionEngineID {
		if s.providers == nil {
			return decisionEngineStatus{}, &decisionEngineError{Kind: "credential", Status: http.StatusUnauthorized, Err: errors.New("provider manager is unavailable")}
		}
		if _, _, err := s.providers.findOpenRouterProvider(); err != nil {
			return decisionEngineStatus{}, err
		}
	}
	if _, err := saveDecisionEngineConfig(config); err != nil {
		return decisionEngineStatus{}, err
	}
	return s.status()
}

func (s *decisionEngineService) engine() (decisionEngine, error) {
	config, err := loadDecisionEngineConfig()
	if err != nil {
		return nil, err
	}
	if config.Engine == "off" {
		return nil, &decisionEngineError{Kind: "disabled", Status: http.StatusConflict, Err: errors.New("Decision Engine is off")}
	}
	if config.Engine != jevDecisionEngineID {
		return nil, &decisionEngineError{Kind: "validation", Status: http.StatusBadRequest, Err: errors.New("unsupported decision engine")}
	}
	provider, key, err := s.providers.findOpenRouterProvider()
	if err != nil {
		return nil, err
	}
	endpoint, err := openRouterDecisionEndpoint(provider.BaseURL)
	if err != nil {
		return nil, &decisionEngineError{Kind: "validation", Status: http.StatusBadRequest, Err: err}
	}
	return &openRouterDecisionEngine{
		endpoint: endpoint,
		model: config.Model,
		apiKey: key,
		httpClient: s.client,
	}, nil
}

func (s *decisionEngineService) evaluate(ctx context.Context, input decisionEvaluateRequest) (decisionEvaluateResponse, error) {
	engine, err := s.engine()
	if err != nil {
		return decisionEvaluateResponse{}, err
	}
	return engine.Evaluate(ctx, input)
}

func writeDecisionEngineError(w http.ResponseWriter, err error) {
	var typed *decisionEngineError
	if errors.As(err, &typed) {
		status := typed.Status
		if status == 0 {
			status = http.StatusBadGateway
		}
		writeJSON(w, status, map[string]any{"error": typed.Error(), "kind": typed.Kind})
		return
	}
	writeJSON(w, http.StatusInternalServerError, jsonError{Error: err.Error()})
}

func registerDecisionEngineRoutes(mux *http.ServeMux, service *decisionEngineService) {
	mux.HandleFunc("GET /local/decision-engine", func(w http.ResponseWriter, _ *http.Request) {
		status, err := service.status()
		if err != nil {
			writeDecisionEngineError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, status)
	})
	mux.HandleFunc("PUT /local/decision-engine", func(w http.ResponseWriter, r *http.Request) {
		var input decisionEngineConfig
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
			writeJSON(w, http.StatusBadRequest, jsonError{Error: "invalid decision engine JSON body"})
			return
		}
		status, err := service.configure(input)
		if err != nil {
			writeDecisionEngineError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, status)
	})
	mux.HandleFunc("POST /local/decision-engine/evaluate", func(w http.ResponseWriter, r *http.Request) {
		var input decisionEvaluateRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&input); err != nil {
			writeJSON(w, http.StatusBadRequest, jsonError{Error: "invalid decision request JSON body"})
			return
		}
		result, err := service.evaluate(r.Context(), input)
		if err != nil {
			writeDecisionEngineError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
}
