package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	layaRouterProviderID    = "tl-laya-router"
	layaRouterModelID       = "auto"
	layaRouterDisplayName   = "Laya Router"
	layaRouterConfigVersion = 1
)

type layaRouterModelPreference struct {
	ProviderID string `json:"providerID"`
	ModelID    string `json:"modelID"`
	Enabled    bool   `json:"enabled"`
	Group      string `json:"group"`
	Quality    int    `json:"quality"`
	Speed      int    `json:"speed"`
}

type layaRouterConfig struct {
	Version int                         `json:"version"`
	Profile string                      `json:"profile"`
	Models  []layaRouterModelPreference `json:"models,omitempty"`
}

type layaRouterCandidate struct {
	ProviderID   string `json:"providerID"`
	ProviderName string `json:"providerName"`
	ModelID      string `json:"modelID"`
	ModelName    string `json:"modelName"`
	Connected    bool   `json:"connected"`
	Ready        bool   `json:"ready"`
	Availability string `json:"availability,omitempty"`
	Enabled      bool   `json:"enabled"`
	Group        string `json:"group"`
	Quality      int    `json:"quality"`
	Speed        int    `json:"speed"`
	Reasoning    bool   `json:"reasoning,omitempty"`
	ContextLimit int    `json:"contextLimit,omitempty"`
	OutputLimit  int    `json:"outputLimit,omitempty"`
}

type layaRouterStatus struct {
	Available     bool                  `json:"available"`
	PluginEnabled bool                  `json:"pluginEnabled"`
	Profile       string                `json:"profile"`
	ProviderID    string                `json:"providerID"`
	ModelID       string                `json:"modelID"`
	DisplayName   string                `json:"displayName"`
	Models        []layaRouterCandidate `json:"models"`
	Message       string                `json:"message,omitempty"`
}

type layaRouteAnalysis struct {
	Difficulty   float64 `json:"difficulty"`
	Domain       string  `json:"domain"`
	NeedsTools   float64 `json:"needsTools"`
	Sensitive    float64 `json:"sensitive"`
	Checkpoint   string  `json:"checkpoint,omitempty"`
	LayaReason   string  `json:"layaReason,omitempty"`
	LatencyMS    float64 `json:"latencyMs,omitempty"`
}

type nativeRouteSelection struct {
	ProviderID       string            `json:"providerID"`
	ProviderName     string            `json:"providerName"`
	ModelID          string            `json:"modelID"`
	ModelName        string            `json:"modelName"`
	Profile          string            `json:"profile"`
	Group            string            `json:"group"`
	Quality          int               `json:"quality"`
	Speed            int               `json:"speed"`
	Reason           string            `json:"reason"`
	Analysis         layaRouteAnalysis `json:"analysis"`
	RouterProviderID string            `json:"routerProviderID,omitempty"`
	RouterModelID    string            `json:"routerModelID,omitempty"`
	RouterName       string            `json:"routerName,omitempty"`
	RouterSource     string            `json:"routerSource,omitempty"`
}

type nativeRequestRouter interface {
	Handles(*sessionModelRef) bool
	Route(context.Context, string, string) (nativeRouteSelection, error)
}

type nativeTargetedRequestRouter interface {
	RouteFor(context.Context, string, string, *sessionModelRef) (nativeRouteSelection, error)
}

type nativeRequestFallbackRouter interface {
	Fallback(context.Context, string, string, nativeRouteSelection, error) (nativeRouteSelection, bool, error)
}

type layaRouterCooldown struct {
	Until  time.Time
	Reason string
}

type layaRouterService struct {
	providers *providerManager
	plugins   *pluginManager
	mu        sync.Mutex
	healthMu  sync.Mutex
	cooldowns map[string]layaRouterCooldown
	// Credential identities are kept as hashes in memory only; a newly
	// configured API key must not inherit the old account's quota cooldown.
	credentialHashes map[string][32]byte
}

func newLayaRouterService(providers *providerManager, plugins *pluginManager) *layaRouterService {
	return &layaRouterService{providers: providers, plugins: plugins, cooldowns: map[string]layaRouterCooldown{}, credentialHashes: map[string][32]byte{}}
}

func layaRouterConfigPath() string {
	return filepath.Join(tlStudioStateDirectory(), "laya-router.json")
}

func defaultLayaRouterConfig() layaRouterConfig {
	return layaRouterConfig{Version: layaRouterConfigVersion, Profile: "balanced", Models: []layaRouterModelPreference{}}
}

func normalizeLayaRouterProfile(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "balanced", "cost", "quality", "speed", "free":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "balanced"
	}
}

func normalizeLayaModelGroup(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "free", "included", "budget", "standard", "premium":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "standard"
	}
}

func clampLayaScore(value int) int {
	if value < 1 {
		return 1
	}
	if value > 5 {
		return 5
	}
	return value
}

func layaPreferenceKey(providerID, modelID string) string {
	return strings.TrimSpace(providerID) + "\x00" + strings.TrimSpace(modelID)
}

func layaProviderCooldownKey(providerID string) string {
	return layaPreferenceKey(providerID, "*")
}

func layaGroupCooldownKey(providerID, group string) string {
	return layaPreferenceKey(providerID, "group:"+normalizeLayaModelGroup(group))
}

func layaFreeTierQuotaFailure(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(text, "free-models-per-day") ||
		strings.Contains(text, "openrouter_free_tier_daily") ||
		strings.Contains(text, "free model requests per day")
}

func classifyLayaRouteFailure(err error) (providerWide bool, cooldown time.Duration, reason string, ok bool) {
	if err == nil {
		return false, 0, "", false
	}
	text := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(text, "bridge is not paired") ||
		strings.Contains(text, "chrome extension is not paired") ||
		strings.Contains(text, "browser sign-in") ||
		strings.Contains(text, "reauthenticate") ||
		strings.Contains(text, "unauthorized"):
		return true, 2 * time.Minute, "Provider session unavailable", true
	case strings.Contains(text, "model_not_available") ||
		strings.Contains(text, "model isn't available right now") ||
		strings.Contains(text, "model is not available"):
		return false, 30 * time.Minute, "Model unavailable", true
	case strings.Contains(text, "unavailable for free") ||
		strings.Contains(text, "model is unavailable for free"):
		return false, 30 * time.Minute, "Free model unavailable", true
	case strings.Contains(text, "status 429") ||
		strings.Contains(text, "rate-limited") ||
		strings.Contains(text, "rate limit") ||
		strings.Contains(text, "too many requests"):
		return false, 90 * time.Second, "Rate limited", true
	case strings.Contains(text, "model returned an empty response") ||
		strings.Contains(text, "model stopped without executing requested work"):
		return false, 30 * time.Second, "Model did not complete the turn", true
	case strings.Contains(text, "status 500") ||
		strings.Contains(text, "status 502") ||
		strings.Contains(text, "status 503") ||
		strings.Contains(text, "status 504") ||
		strings.Contains(text, "provider unavailable") ||
		strings.Contains(text, "service unavailable") ||
		strings.Contains(text, "upstream unavailable") ||
		strings.Contains(text, "temporary failure") ||
		strings.Contains(text, "overloaded") ||
		strings.Contains(text, "temporarily unavailable") ||
		strings.Contains(text, "timed out") ||
		strings.Contains(text, "timeout") ||
		strings.Contains(text, "connection reset") ||
		strings.Contains(text, "connection closed") ||
		strings.Contains(text, "fetch failed"):
		return false, 45 * time.Second, "Temporarily unavailable", true
	default:
		return false, 0, "", false
	}
}

func (s *layaRouterService) markRouteFailure(selection nativeRouteSelection, err error) bool {
	if s == nil {
		return false
	}
	if normalizeLayaModelGroup(selection.Group) == "free" && layaFreeTierQuotaFailure(err) {
		s.healthMu.Lock()
		if s.cooldowns == nil {
			s.cooldowns = map[string]layaRouterCooldown{}
		}
		s.cooldowns[layaGroupCooldownKey(selection.ProviderID, "free")] = layaRouterCooldown{
			Until:  time.Now().Add(15 * time.Minute),
			Reason: "Free-tier daily quota exhausted",
		}
		s.healthMu.Unlock()
		return true
	}
	providerWide, duration, reason, ok := classifyLayaRouteFailure(err)
	if !ok {
		return false
	}
	key := layaPreferenceKey(selection.ProviderID, selection.ModelID)
	if providerWide {
		key = layaProviderCooldownKey(selection.ProviderID)
	}
	s.healthMu.Lock()
	if s.cooldowns == nil {
		s.cooldowns = map[string]layaRouterCooldown{}
	}
	s.cooldowns[key] = layaRouterCooldown{Until: time.Now().Add(duration), Reason: reason}
	s.healthMu.Unlock()
	return true
}

func (s *layaRouterService) candidateCooldown(providerID, modelID, group string) (layaRouterCooldown, bool) {
	if s == nil {
		return layaRouterCooldown{}, false
	}
	now := time.Now()
	keys := []string{
		layaProviderCooldownKey(providerID),
		layaGroupCooldownKey(providerID, group),
		layaPreferenceKey(providerID, modelID),
	}
	s.healthMu.Lock()
	defer s.healthMu.Unlock()
	for _, key := range keys {
		entry, ok := s.cooldowns[key]
		if !ok {
			continue
		}
		if !entry.Until.After(now) {
			delete(s.cooldowns, key)
			continue
		}
		return entry, true
	}
	return layaRouterCooldown{}, false
}

// observeCredential invalidates runtime provider/model cooldowns only when an
// actual credential changes. It never stores, persists, or logs API keys.
func (s *layaRouterService) observeCredential(providerID, key string) {
	if s == nil { return }
	identity := sha256.Sum256([]byte(key))
	s.healthMu.Lock()
	defer s.healthMu.Unlock()
	if s.credentialHashes == nil { s.credentialHashes = make(map[string][32]byte) }
	if previous, ok := s.credentialHashes[providerID]; ok && previous != identity {
		prefix := providerID + "\x00"
		for id := range s.cooldowns {
			if strings.HasPrefix(id, prefix) { delete(s.cooldowns, id) }
		}
	}
	s.credentialHashes[providerID] = identity
}

func (s *layaRouterService) providerReady(ctx context.Context, provider tlProviderDefinition, connected bool) (bool, string) {
	if !connected {
		return false, "Not connected"
	}
	if provider.Protocol != claudeWebProviderProtocol {
		return true, "Ready"
	}
	adapter, _ := s.providers.accountAdapter(provider.ID).(*claudeWebAccountAdapter)
	if adapter == nil || adapter.transport == nil {
		return false, "Claude Web transport unavailable"
	}
	probeCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	probe, err := adapter.transport.Probe(probeCtx)
	if err != nil {
		if errors.Is(err, errClaudeWebExtensionNotPaired) || strings.Contains(strings.ToLower(err.Error()), "not paired") {
			return false, "Claude Web not paired"
		}
		return false, "Claude Web unavailable"
	}
	if !probe.Connected {
		return false, "Claude Web signed out"
	}
	return true, "Ready"
}

func normalizeLayaRouterConfig(input layaRouterConfig) layaRouterConfig {
	input.Version = layaRouterConfigVersion
	input.Profile = normalizeLayaRouterProfile(input.Profile)
	seen := map[string]bool{}
	models := make([]layaRouterModelPreference, 0, len(input.Models))
	for _, item := range input.Models {
		item.ProviderID = strings.TrimSpace(item.ProviderID)
		item.ModelID = strings.TrimSpace(item.ModelID)
		if item.ProviderID == "" || item.ModelID == "" {
			continue
		}
		key := layaPreferenceKey(item.ProviderID, item.ModelID)
		if seen[key] {
			continue
		}
		seen[key] = true
		item.Group = normalizeLayaModelGroup(item.Group)
		item.Quality = clampLayaScore(item.Quality)
		item.Speed = clampLayaScore(item.Speed)
		models = append(models, item)
	}
	sort.Slice(models, func(i, j int) bool {
		left := strings.ToLower(models[i].ProviderID + "\x00" + models[i].ModelID)
		right := strings.ToLower(models[j].ProviderID + "\x00" + models[j].ModelID)
		return left < right
	})
	input.Models = models
	return input
}

func (s *layaRouterService) loadConfig() (layaRouterConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(layaRouterConfigPath())
	if errors.Is(err, os.ErrNotExist) {
		return defaultLayaRouterConfig(), nil
	}
	if err != nil {
		return layaRouterConfig{}, err
	}
	var config layaRouterConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return layaRouterConfig{}, fmt.Errorf("decode Laya Router config: %w", err)
	}
	if config.Version != 0 && config.Version != layaRouterConfigVersion {
		return layaRouterConfig{}, fmt.Errorf("unsupported Laya Router config version %d", config.Version)
	}
	return normalizeLayaRouterConfig(config), nil
}

func (s *layaRouterService) saveConfig(input layaRouterConfig) (layaRouterConfig, error) {
	input = normalizeLayaRouterConfig(input)
	s.mu.Lock()
	defer s.mu.Unlock()
	path := layaRouterConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return layaRouterConfig{}, err
	}
	data, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return layaRouterConfig{}, err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), "laya-router-*.tmp")
	if err != nil {
		return layaRouterConfig{}, err
	}
	name := temp.Name()
	defer os.Remove(name)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return layaRouterConfig{}, err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return layaRouterConfig{}, err
	}
	if err := temp.Close(); err != nil {
		return layaRouterConfig{}, err
	}
	if err := os.Rename(name, path); err != nil {
		if writeErr := os.WriteFile(path, data, 0o600); writeErr != nil {
			return layaRouterConfig{}, writeErr
		}
	}
	return input, nil
}

func (s *layaRouterService) pluginEnabled(project string) bool {
	if s == nil || s.plugins == nil {
		return false
	}
	config, found, err := s.plugins.store.find(project, "laya")
	return err == nil && found && config.Enabled
}

func layaDefaultTraits(provider tlProviderDefinition, model tlProviderModel) (string, int, int) {
	text := strings.ToLower(strings.Join([]string{provider.ID, provider.Name, provider.Protocol, model.ID, model.Name}, " "))
	group := "standard"
	switch {
	case strings.Contains(text, ":free") || strings.Contains(text, " free"):
		group = "free"
	case provider.Protocol == "codex-chatgpt" || provider.Protocol == "claude-code-account" || provider.Protocol == "claude-web-browser":
		group = "included"
	case strings.Contains(text, "nano") || strings.Contains(text, "mini") || strings.Contains(text, "flash") ||
		strings.Contains(text, "haiku") || strings.Contains(text, "small"):
		group = "budget"
	case strings.Contains(text, "opus") || strings.Contains(text, "premium") || strings.Contains(text, "ultra") ||
		strings.Contains(text, "max"):
		group = "premium"
	}

	quality := 3
	switch {
	case strings.Contains(text, "astra") || strings.Contains(text, "opus") || strings.Contains(text, "405b") ||
		strings.Contains(text, "70b") || strings.Contains(text, "gpt-6 pro") || strings.Contains(text, "gpt-6-pro"):
		quality = 5
	case strings.Contains(text, "sonnet") || strings.Contains(text, "gpt-6") || strings.Contains(text, "gpt-5") ||
		strings.Contains(text, "o3") || strings.Contains(text, "deepseek-v3") || strings.Contains(text, "qwen3"):
		quality = 4
	case strings.Contains(text, "nano") || strings.Contains(text, "small") || strings.Contains(text, "8b"):
		quality = 2
	}
	if strings.Contains(text, "luna") {
		quality = 4
	}

	speed := 3
	switch {
	case strings.Contains(text, "nano") || strings.Contains(text, "mini") || strings.Contains(text, "flash") ||
		strings.Contains(text, "haiku") || strings.Contains(text, "luna"):
		speed = 5
	case strings.Contains(text, "sonnet") || strings.Contains(text, "small"):
		speed = 4
	case strings.Contains(text, "opus") || strings.Contains(text, "405b") || strings.Contains(text, "70b") ||
		strings.Contains(text, "pro"):
		speed = 2
	}
	return group, quality, speed
}

func (s *layaRouterService) candidates(ctx context.Context, project string) ([]layaRouterCandidate, error) {
	if s == nil || s.providers == nil {
		return nil, errors.New("provider manager is unavailable")
	}
	definitions, err := s.providers.ensureRegistryInitialized(ctx)
	if err != nil {
		return nil, err
	}
	config, err := s.loadConfig()
	if err != nil {
		return nil, err
	}
	prefs := map[string]layaRouterModelPreference{}
	for _, item := range config.Models {
		prefs[layaPreferenceKey(item.ProviderID, item.ModelID)] = item
	}

	result := []layaRouterCandidate{}
	for _, provider := range definitions {
		connected := false
		key, keyErr := s.providers.effectiveCredential(ctx, provider.ID, project)
		if keyErr == nil && strings.TrimSpace(key) != "" {
			connected = true
		}
		s.observeCredential(provider.ID, key)
		providerReady, providerAvailability := s.providerReady(ctx, provider, connected)
		for _, model := range provider.Models {
			if model.Kind == "router" || !model.ToolCall || !providerModelUsesNativeAgent(provider, model) {
				continue
			}
			group, quality, speed := layaDefaultTraits(provider, model)
			enabled := true
			if pref, ok := prefs[layaPreferenceKey(provider.ID, model.ID)]; ok {
				enabled = pref.Enabled
				group = normalizeLayaModelGroup(pref.Group)
				quality = clampLayaScore(pref.Quality)
				speed = clampLayaScore(pref.Speed)
			}
			ready := providerReady
			availability := providerAvailability
			if cooldown, cooling := s.candidateCooldown(provider.ID, model.ID, group); cooling {
				ready = false
				availability = cooldown.Reason
			}
			result = append(result, layaRouterCandidate{
				ProviderID: provider.ID, ProviderName: provider.Name,
				ModelID: model.ID, ModelName: model.Name,
				Connected: connected, Ready: ready, Availability: availability, Enabled: enabled,
				Group: group, Quality: quality, Speed: speed,
				Reasoning: model.Reasoning, ContextLimit: model.ContextLimit, OutputLimit: model.OutputLimit,
			})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		left := strings.ToLower(result[i].ProviderName + "\x00" + result[i].ModelName + "\x00" + result[i].ModelID)
		right := strings.ToLower(result[j].ProviderName + "\x00" + result[j].ModelName + "\x00" + result[j].ModelID)
		return left < right
	})
	return result, nil
}

func (s *layaRouterService) Status(ctx context.Context, project string) (layaRouterStatus, error) {
	config, err := s.loadConfig()
	if err != nil {
		return layaRouterStatus{}, err
	}
	models, err := s.candidates(ctx, project)
	if err != nil {
		return layaRouterStatus{}, err
	}
	enabled := s.pluginEnabled(project)
	status := layaRouterStatus{
		Available: enabled && len(models) > 0,
		PluginEnabled: enabled,
		Profile: config.Profile,
		ProviderID: layaRouterProviderID,
		ModelID: layaRouterModelID,
		DisplayName: layaRouterDisplayName,
		Models: models,
	}
	if !enabled {
		status.Message = "Enable the Laya plugin to use Laya Router."
	} else {
		usable := 0
		for _, candidate := range models {
			if candidate.Enabled && layaCandidateReady(candidate) {
				usable++
			}
		}
		if usable == 0 {
			// Keep Laya selectable and its configuration discoverable; routing
			// itself must fail safely until an eligible model becomes ready.
			status.Message = "No enabled, connected tool-capable models are currently ready for Laya Router."
		}
	}
	return status, nil
}

func (s *layaRouterService) Handles(model *sessionModelRef) bool {
	return model != nil && strings.TrimSpace(model.ProviderID) == layaRouterProviderID && strings.TrimSpace(model.ID) == layaRouterModelID
}

func numberFromAny(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case json.Number:
		result, _ := typed.Float64()
		return result
	case string:
		result, _ := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return result
	default:
		return 0
	}
}

func mapFromAny(value any) map[string]any {
	if result, ok := value.(map[string]any); ok {
		return result
	}
	return nil
}

func decodeLayaJSONMap(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		for _, key := range []string{"result", "value", "text"} {
			if nested := decodeLayaJSONMap(typed[key]); nested != nil {
				if _, hasAnswers := nested["answers"]; hasAnswers {
					return nested
				}
			}
		}
		return typed
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return nil
		}
		var decoded any
		if json.Unmarshal([]byte(text), &decoded) == nil {
			return decodeLayaJSONMap(decoded)
		}
	}
	return nil
}

func layaToolPayload(output any) (map[string]any, error) {
	root := mapFromAny(output)
	if root == nil {
		return nil, errors.New("Laya returned an invalid MCP result")
	}

	// Laya's MCP tools return a JSON string. MCP SDK versions may additionally
	// synthesize structuredContent for string-returning tools, but that wrapper is
	// not the Laya payload itself. Prefer the canonical content[].text first.
	if content, ok := root["content"].([]map[string]any); ok {
		for _, item := range content {
			if decoded := decodeLayaJSONMap(item["text"]); decoded != nil {
				return decoded, nil
			}
		}
	}
	if content, ok := root["content"].([]any); ok {
		for _, raw := range content {
			item := mapFromAny(raw)
			if decoded := decodeLayaJSONMap(item["text"]); decoded != nil {
				return decoded, nil
			}
		}
	}
	if structured := decodeLayaJSONMap(root["structuredContent"]); structured != nil {
		return structured, nil
	}
	return nil, errors.New("Laya MCP result did not contain a JSON payload")
}

func layaAnswerMap(answers map[string]any, key string) map[string]any {
	return mapFromAny(answers[key])
}

func layaAnalyzePayload(payload map[string]any) (layaRouteAnalysis, error) {
	answers := mapFromAny(payload["answers"])
	if answers == nil {
		return layaRouteAnalysis{}, errors.New("Laya model_router returned no answers")
	}
	difficulty := layaAnswerMap(answers, "difficulty")
	domain := layaAnswerMap(answers, "domain")
	needsTools := layaAnswerMap(answers, "needs_tools")
	sensitive := layaAnswerMap(answers, "is_sensitive")
	if difficulty == nil || domain == nil || needsTools == nil || sensitive == nil {
		return layaRouteAnalysis{}, errors.New("Laya model_router returned an incomplete analysis")
	}
	analysis := layaRouteAnalysis{
		Difficulty: numberFromAny(difficulty["score"]),
		Domain: strings.TrimSpace(fmt.Sprint(domain["choice"])),
		NeedsTools: numberFromAny(needsTools["noul"]),
		Sensitive: numberFromAny(sensitive["noul"]),
		LatencyMS: numberFromAny(payload["latency_ms"]),
	}
	if routing := mapFromAny(payload["routing"]); routing != nil {
		analysis.Checkpoint = strings.TrimSpace(fmt.Sprint(routing["model"]))
		analysis.LayaReason = strings.TrimSpace(fmt.Sprint(routing["reason"]))
	}
	return analysis, nil
}

func layaCandidateReady(candidate layaRouterCandidate) bool {
	return candidate.Connected && (candidate.Ready || candidate.Availability == "")
}

func layaGroupRank(group string) int {
	switch normalizeLayaModelGroup(group) {
	case "free":
		return 0
	case "included":
		return 1
	case "budget":
		return 2
	case "standard":
		return 3
	case "premium":
		return 4
	default:
		return 3
	}
}

func layaTargetQuality(analysis layaRouteAnalysis) int {
	target := 2
	switch {
	case analysis.Difficulty >= 2.5:
		target = 5
	case analysis.Difficulty >= 1.7:
		target = 4
	case analysis.Difficulty >= 0.9:
		target = 3
	}
	if analysis.Sensitive >= 0.5 && target < 4 {
		target = 4
	}
	if analysis.NeedsTools >= 0.5 && target < 3 {
		target = 3
	}
	if (analysis.Domain == "code" || analysis.Domain == "math_or_logic" || analysis.Domain == "data_analysis") && analysis.Difficulty >= 1.5 && target < 5 {
		target++
	}
	return clampLayaScore(target)
}

func chooseLayaCandidate(profile string, analysis layaRouteAnalysis, candidates []layaRouterCandidate) (layaRouterCandidate, string, error) {
	profile = normalizeLayaRouterProfile(profile)
	eligible := make([]layaRouterCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Enabled && layaCandidateReady(candidate) {
			eligible = append(eligible, candidate)
		}
	}
	if profile == "free" {
		free := eligible[:0]
		for _, candidate := range eligible {
			if normalizeLayaModelGroup(candidate.Group) == "free" {
				free = append(free, candidate)
			}
		}
		eligible = free
		if len(eligible) == 0 {
			return layaRouterCandidate{}, "", errors.New("Free only routing is selected, but no enabled connected model is classified as Free")
		}
	}
	if len(eligible) == 0 {
		return layaRouterCandidate{}, "", errors.New("Laya Router has no enabled connected models")
	}

	target := layaTargetQuality(analysis)
	qualified := make([]layaRouterCandidate, 0, len(eligible))
	for _, candidate := range eligible {
		if candidate.Quality >= target {
			qualified = append(qualified, candidate)
		}
	}
	if len(qualified) == 0 {
		maxQuality := 0
		for _, candidate := range eligible {
			if candidate.Quality > maxQuality {
				maxQuality = candidate.Quality
			}
		}
		for _, candidate := range eligible {
			if candidate.Quality == maxQuality {
				qualified = append(qualified, candidate)
			}
		}
	}

	sort.SliceStable(qualified, func(i, j int) bool {
		a, b := qualified[i], qualified[j]
		switch profile {
		case "quality", "free":
			if a.Quality != b.Quality {
				return a.Quality > b.Quality
			}
			if a.Speed != b.Speed {
				return a.Speed > b.Speed
			}
			return layaGroupRank(a.Group) < layaGroupRank(b.Group)
		case "speed":
			if a.Speed != b.Speed {
				return a.Speed > b.Speed
			}
			if a.Quality != b.Quality {
				return a.Quality > b.Quality
			}
			return layaGroupRank(a.Group) < layaGroupRank(b.Group)
		case "cost":
			if layaGroupRank(a.Group) != layaGroupRank(b.Group) {
				return layaGroupRank(a.Group) < layaGroupRank(b.Group)
			}
			if a.Quality != b.Quality {
				return a.Quality > b.Quality
			}
			return a.Speed > b.Speed
		default:
			aDistance := int(math.Abs(float64(a.Quality - target)))
			bDistance := int(math.Abs(float64(b.Quality - target)))
			if aDistance != bDistance {
				return aDistance < bDistance
			}
			if layaGroupRank(a.Group) != layaGroupRank(b.Group) {
				return layaGroupRank(a.Group) < layaGroupRank(b.Group)
			}
			return a.Speed > b.Speed
		}
	})
	selected := qualified[0]
	reason := fmt.Sprintf(
		"%s profile · Laya difficulty %.2f/3 · domain %s · tools %.2f · sensitive %.2f · target quality %d/5",
		profile, analysis.Difficulty, firstSessionString(analysis.Domain, "unknown"), analysis.NeedsTools, analysis.Sensitive, target,
	)
	return selected, reason, nil
}

func (m *pluginManager) CallPluginTool(ctx context.Context, project, pluginID, toolName string, arguments map[string]any) (any, error) {
	if m == nil {
		return nil, errors.New("plugin manager is unavailable")
	}
	config, found, err := m.store.find(project, pluginID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("%s plugin is not installed", pluginID)
	}
	if !config.Enabled {
		return nil, fmt.Errorf("%s plugin is disabled", pluginID)
	}
	m.mu.Lock()
	client, err := m.ensureClientLocked(ctx, config, project)
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	call := func(target mcpPluginClient) (any, error) {
		if concrete, ok := target.(*mcpClient); ok && pluginID == "laya" {
			// The first local Laya decision may need to download and load its checkpoint.
			// Keep normal Agent-facing MCP calls on the short timeout; only this explicit
			// internal routing inference gets a longer cancellable window.
			return concrete.callToolWithTimeout(ctx, toolName, arguments, 5*time.Minute)
		}
		return target.CallTool(ctx, toolName, arguments)
	}
	output, err := call(client)
	if err == nil {
		return output, nil
	}
	// Restart once on transport/process failure, matching ordinary MCP tool execution.
	m.mu.Lock()
	m.stopLocked(config)
	restarted, restartErr := m.ensureClientLocked(ctx, config, project)
	m.mu.Unlock()
	if restartErr != nil {
		return nil, err
	}
	return call(restarted)
}

func (s *layaRouterService) Analyze(ctx context.Context, project, prompt string) (layaRouteAnalysis, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return layaRouteAnalysis{}, errors.New("prompt is required for Laya routing")
	}
	if !s.pluginEnabled(project) {
		return layaRouteAnalysis{}, errors.New("Laya plugin is disabled")
	}
	output, err := s.plugins.CallPluginTool(ctx, project, "laya", "laya_preset", map[string]any{
		"preset": "model_router",
		"state": map[string]any{"request": prompt},
	})
	if err != nil {
		return layaRouteAnalysis{}, fmt.Errorf("Laya model_router failed: %w", err)
	}
	payload, err := layaToolPayload(output)
	if err != nil {
		return layaRouteAnalysis{}, err
	}
	return layaAnalyzePayload(payload)
}

func applyLayaRouterPreferences(candidates []layaRouterCandidate, config layaRouterConfig) []layaRouterCandidate {
	prefs := map[string]layaRouterModelPreference{}
	for _, item := range normalizeLayaRouterConfig(config).Models {
		prefs[layaPreferenceKey(item.ProviderID, item.ModelID)] = item
	}
	result := append([]layaRouterCandidate(nil), candidates...)
	for index := range result {
		if pref, ok := prefs[layaPreferenceKey(result[index].ProviderID, result[index].ModelID)]; ok {
			result[index].Enabled = pref.Enabled
			result[index].Group = normalizeLayaModelGroup(pref.Group)
			result[index].Quality = clampLayaScore(pref.Quality)
			result[index].Speed = clampLayaScore(pref.Speed)
		}
	}
	return result
}

func (s *layaRouterService) routeWithConfig(ctx context.Context, project, prompt string, config layaRouterConfig) (nativeRouteSelection, error) {
	config = normalizeLayaRouterConfig(config)
	analysis, err := s.Analyze(ctx, project, prompt)
	if err != nil {
		return nativeRouteSelection{}, err
	}
	candidates, err := s.candidates(ctx, project)
	if err != nil {
		return nativeRouteSelection{}, err
	}
	candidates = applyLayaRouterPreferences(candidates, config)
	selected, reason, err := chooseLayaCandidate(config.Profile, analysis, candidates)
	if err != nil {
		return nativeRouteSelection{}, err
	}
	return nativeRouteSelection{
		ProviderID: selected.ProviderID, ProviderName: selected.ProviderName,
		ModelID: selected.ModelID, ModelName: selected.ModelName,
		Profile: config.Profile, Group: selected.Group, Quality: selected.Quality, Speed: selected.Speed,
		Reason: reason, Analysis: analysis,
		RouterProviderID: layaRouterProviderID,
		RouterModelID: layaRouterModelID,
		RouterName: "Laya",
		RouterSource: "laya-model-router",
	}, nil
}

func (s *layaRouterService) Route(ctx context.Context, project, prompt string) (nativeRouteSelection, error) {
	config, err := s.loadConfig()
	if err != nil {
		return nativeRouteSelection{}, err
	}
	return s.routeWithConfig(ctx, project, prompt, config)
}

func (s *layaRouterService) Fallback(
	ctx context.Context,
	project string,
	prompt string,
	previous nativeRouteSelection,
	failure error,
) (nativeRouteSelection, bool, error) {
	if !s.markRouteFailure(previous, failure) {
		return nativeRouteSelection{}, false, nil
	}
	config, err := s.loadConfig()
	if err != nil {
		return nativeRouteSelection{}, false, err
	}
	candidates, err := s.candidates(ctx, project)
	if err != nil {
		return nativeRouteSelection{}, false, err
	}
	candidates = applyLayaRouterPreferences(candidates, config)
	selected, reason, err := chooseLayaCandidate(config.Profile, previous.Analysis, candidates)
	if err != nil {
		return nativeRouteSelection{}, false, nil
	}
	if selected.ProviderID == previous.ProviderID && selected.ModelID == previous.ModelID {
		return nativeRouteSelection{}, false, nil
	}
	return nativeRouteSelection{
		ProviderID: selected.ProviderID,
		ProviderName: selected.ProviderName,
		ModelID: selected.ModelID,
		ModelName: selected.ModelName,
		Profile: config.Profile,
		Group: selected.Group,
		Quality: selected.Quality,
		Speed: selected.Speed,
		Reason: "Automatic fallback after " + strings.TrimSpace(failure.Error()) + " · " + reason,
		Analysis: previous.Analysis,
		RouterProviderID: firstSessionString(previous.RouterProviderID, layaRouterProviderID),
		RouterModelID: firstSessionString(previous.RouterModelID, layaRouterModelID),
		RouterName: firstSessionString(previous.RouterName, "Laya"),
		RouterSource: firstSessionString(previous.RouterSource, "laya-model-router"),
	}, true, nil
}

func (s *layaRouterService) decorateCatalog(ctx context.Context, directory string, result *providerCatalogResponse) error {
	if result == nil || !s.pluginEnabled(directory) {
		return nil
	}
	status, err := s.Status(ctx, directory)
	if err != nil || !status.Available {
		return err
	}
	enabled := true
	result.All = append(result.All, catalogProvider{
		ID: layaRouterProviderID,
		Name: layaRouterDisplayName,
		Source: "local-router",
		Models: map[string]catalogModel{
			layaRouterModelID: {Name: layaRouterDisplayName, Kind: "router", Enabled: &enabled},
		},
	})
	result.Connected = appendUniqueString(result.Connected, layaRouterProviderID)
	result.Default[layaRouterProviderID] = layaRouterModelID
	return nil
}

func registerLayaRouterRoutes(mux *http.ServeMux, state *appState, service *layaRouterService) {
	mux.HandleFunc("GET /local/laya-router", func(w http.ResponseWriter, r *http.Request) {
		status, err := service.Status(r.Context(), state.projectPath())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, jsonError{Error: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, status)
	})
	mux.HandleFunc("PUT /local/laya-router", func(w http.ResponseWriter, r *http.Request) {
		var input layaRouterConfig
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
			writeJSON(w, http.StatusBadRequest, jsonError{Error: "invalid Laya Router JSON body"})
			return
		}
		saved, err := service.saveConfig(input)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, jsonError{Error: err.Error()})
			return
		}
		status, err := service.Status(r.Context(), state.projectPath())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, jsonError{Error: err.Error()})
			return
		}
		status.Profile = saved.Profile
		writeJSON(w, http.StatusOK, status)
	})
	mux.HandleFunc("POST /local/laya-router/preview", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Prompt  string                      `json:"prompt"`
			Profile string                      `json:"profile,omitempty"`
			Models  []layaRouterModelPreference `json:"models,omitempty"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
			writeJSON(w, http.StatusBadRequest, jsonError{Error: "invalid JSON body"})
			return
		}
		config, err := service.loadConfig()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, jsonError{Error: err.Error()})
			return
		}
		if strings.TrimSpace(input.Profile) != "" {
			config.Profile = input.Profile
		}
		if input.Models != nil {
			config.Models = input.Models
		}
		decision, err := service.routeWithConfig(r.Context(), state.projectPath(), input.Prompt, config)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, jsonError{Error: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, decision)
	})
}
