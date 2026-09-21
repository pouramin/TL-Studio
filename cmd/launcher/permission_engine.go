package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const permissionPolicyVersion = 1

type permissionRule struct {
	ID         string `json:"id"`
	Project    string `json:"project"`
	Permission string `json:"permission"`
	Matcher    string `json:"matcher"`
	Decision   string `json:"decision"`
	CreatedAt  string `json:"createdAt"`
}

type permissionPolicyFile struct {
	Version int              `json:"version"`
	Rules   []permissionRule `json:"rules"`
}

type permissionPolicyStore struct {
	mu       sync.Mutex
	loaded   bool
	filePath string
	rules    []permissionRule
}

func permissionPolicyPath() string {
	if dir := strings.TrimSpace(os.Getenv("TL_STUDIO_STATE_DIR")); dir != "" {
		return filepath.Join(dir, "permissions.json")
	}
	base, err := os.UserConfigDir()
	if err != nil || strings.TrimSpace(base) == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "TL Studio", "permissions.json")
}

func newPermissionPolicyStore(filePath string) *permissionPolicyStore {
	return &permissionPolicyStore{filePath: filePath}
}

func normalizePermissionProject(project string) string {
	project = filepath.Clean(strings.TrimSpace(project))
	if runtime.GOOS == "windows" {
		project = strings.ToLower(project)
	}
	return project
}

func permissionRuleID(project, permission, matcher, decision string) string {
	value := strings.Join([]string{
		normalizePermissionProject(project),
		strings.TrimSpace(permission),
		strings.TrimSpace(matcher),
		strings.TrimSpace(decision),
	}, "\x00")
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:12])
}

func (s *permissionPolicyStore) loadLocked() error {
	if s.loaded {
		return nil
	}
	s.loaded = true
	s.rules = nil

	data, err := os.ReadFile(s.filePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var stored permissionPolicyFile
	if err := json.Unmarshal(data, &stored); err != nil {
		return fmt.Errorf("decode permission policy: %w", err)
	}
	if stored.Version != 0 && stored.Version != permissionPolicyVersion {
		return fmt.Errorf("unsupported permission policy version %d", stored.Version)
	}
	seen := map[string]bool{}
	for _, rule := range stored.Rules {
		rule.Project = normalizePermissionProject(rule.Project)
		rule.Permission = strings.TrimSpace(rule.Permission)
		rule.Matcher = strings.TrimSpace(rule.Matcher)
		rule.Decision = strings.TrimSpace(rule.Decision)
		if rule.Project == "" || rule.Permission == "" || rule.Matcher == "" || rule.Decision != "allow" {
			continue
		}
		if rule.ID == "" {
			rule.ID = permissionRuleID(rule.Project, rule.Permission, rule.Matcher, rule.Decision)
		}
		if seen[rule.ID] {
			continue
		}
		seen[rule.ID] = true
		s.rules = append(s.rules, rule)
	}
	return nil
}

func (s *permissionPolicyStore) persistLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.filePath), 0o700); err != nil {
		return err
	}
	rules := append([]permissionRule(nil), s.rules...)
	sort.Slice(rules, func(i, j int) bool {
		left := rules[i].Project + "\x00" + rules[i].Permission + "\x00" + rules[i].Matcher
		right := rules[j].Project + "\x00" + rules[j].Permission + "\x00" + rules[j].Matcher
		return left < right
	})
	data, err := json.MarshalIndent(permissionPolicyFile{Version: permissionPolicyVersion, Rules: rules}, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(s.filePath), "permissions-*.tmp")
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
		if writeErr := os.WriteFile(s.filePath, data, 0o600); writeErr != nil {
			return err
		}
	}
	return nil
}

func (s *permissionPolicyStore) snapshot() ([]permissionRule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return nil, err
	}
	result := append([]permissionRule(nil), s.rules...)
	sort.Slice(result, func(i, j int) bool {
		left := result[i].Project + "\x00" + result[i].Permission + "\x00" + result[i].Matcher
		right := result[j].Project + "\x00" + result[j].Permission + "\x00" + result[j].Matcher
		return left < right
	})
	return result, nil
}

func (s *permissionPolicyStore) addAllowRules(project, permission string, matchers []string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return nil, err
	}
	project = normalizePermissionProject(project)
	permission = strings.TrimSpace(permission)
	if project == "" || permission == "" {
		return nil, errors.New("permission project and permission are required")
	}
	existing := map[string]bool{}
	for _, rule := range s.rules {
		existing[rule.ID] = true
	}
	added := []string{}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, matcher := range matchers {
		matcher = strings.TrimSpace(matcher)
		if matcher == "" {
			continue
		}
		id := permissionRuleID(project, permission, matcher, "allow")
		if existing[id] {
			continue
		}
		s.rules = append(s.rules, permissionRule{
			ID:         id,
			Project:    project,
			Permission: permission,
			Matcher:    matcher,
			Decision:   "allow",
			CreatedAt:  now,
		})
		existing[id] = true
		added = append(added, id)
	}
	if len(added) == 0 {
		return nil, nil
	}
	if err := s.persistLocked(); err != nil {
		s.rules = s.rules[:len(s.rules)-len(added)]
		return nil, err
	}
	return added, nil
}

func (s *permissionPolicyStore) removeIDs(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return err
	}
	remove := map[string]bool{}
	for _, id := range ids {
		remove[id] = true
	}
	next := s.rules[:0]
	for _, rule := range s.rules {
		if !remove[rule.ID] {
			next = append(next, rule)
		}
	}
	s.rules = append([]permissionRule(nil), next...)
	return s.persistLocked()
}

func (s *permissionPolicyStore) remove(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return false, err
	}
	found := false
	next := s.rules[:0]
	for _, rule := range s.rules {
		if rule.ID == id {
			found = true
			continue
		}
		next = append(next, rule)
	}
	if !found {
		return false, nil
	}
	s.rules = append([]permissionRule(nil), next...)
	if err := s.persistLocked(); err != nil {
		return false, err
	}
	return true, nil
}

func (s *permissionPolicyStore) covers(project, permission string, matchers []string) (bool, error) {
	project = normalizePermissionProject(project)
	permission = strings.TrimSpace(permission)
	required := map[string]bool{}
	for _, matcher := range matchers {
		if matcher = strings.TrimSpace(matcher); matcher != "" {
			required[matcher] = true
		}
	}
	if len(required) == 0 {
		return false, nil
	}
	rules, err := s.snapshot()
	if err != nil {
		return false, err
	}
	for _, rule := range rules {
		if rule.Decision == "allow" && rule.Project == project && rule.Permission == permission {
			delete(required, rule.Matcher)
		}
	}
	return len(required) == 0, nil
}

type runtimePermissionError struct {
	Status int
	Body   string
}

func (e *runtimePermissionError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("runtime permission request failed with status %d", e.Status)
	}
	return fmt.Sprintf("runtime permission request failed with status %d: %s", e.Status, e.Body)
}

type permissionEngine struct {
	state         *appState
	backend       *runtimeBackend
	store         *permissionPolicyStore
	nativeMu      sync.Mutex
	nativePending map[string]*nativePermissionWaiter
	events        *liveEventBus
}

func newPermissionEngine(state *appState, backendURL, username, password string) (*permissionEngine, error) {
	backend, err := newRuntimeBackend(
		state,
		backendURL,
		runtimeCredentials{Username: username, Password: password},
		defaultRuntimeEngine(),
	)
	if err != nil {
		return nil, err
	}
	return newPermissionEngineWithBackend(state, backend), nil
}

func newPermissionEngineWithBackend(state *appState, backend *runtimeBackend) *permissionEngine {
	return &permissionEngine{
		state:         state,
		backend:       backend,
		store:         newPermissionPolicyStore(permissionPolicyPath()),
		nativePending: map[string]*nativePermissionWaiter{},
	}
}

func (e *permissionEngine) runtimeRequest(ctx context.Context, method, route string, query url.Values, body any) (json.RawMessage, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := e.backend.newRequest(ctx, method, route, e.state.projectPath(), query, reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	response, err := e.backend.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, &runtimePermissionError{Status: response.StatusCode, Body: strings.TrimSpace(string(data))}
	}
	return json.RawMessage(data), nil
}

func (e *permissionEngine) runtimeQuery() url.Values {
	query := url.Values{}
	if project := strings.TrimSpace(e.state.projectPath()); project != "" {
		query.Set("directory", project)
	}
	return query
}

func permissionString(item map[string]any) string {
	for _, key := range []string{"permission", "action"} {
		if value, ok := item[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return "action"
}

func stringList(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		if direct, ok := value.([]string); ok {
			return append([]string(nil), direct...)
		}
		return nil
	}
	result := make([]string, 0, len(raw))
	for _, item := range raw {
		if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
			result = append(result, strings.TrimSpace(text))
		}
	}
	return result
}

func permissionAlwaysMatchers(item map[string]any) []string {
	return stringList(item["always"])
}

func permissionSensitive(item map[string]any) bool {
	metadata, _ := item["metadata"].(map[string]any)
	if metadata == nil {
		return false
	}
	for _, key := range []string{"skillShell", "sandboxEscalation"} {
		if value, _ := metadata[key].(bool); value {
			return true
		}
	}
	return false
}

func permissionCanRemember(item map[string]any) bool {
	if permissionSensitive(item) {
		return false
	}
	metadata, _ := item["metadata"].(map[string]any)
	if value, _ := metadata["disableAlways"].(bool); value {
		return false
	}
	return len(permissionAlwaysMatchers(item)) > 0
}

func (e *permissionEngine) rawPending(ctx context.Context) ([]map[string]any, error) {
	raw, err := e.runtimeRequest(ctx, http.MethodGet, "/permission", e.runtimeQuery(), nil)
	if err != nil {
		return nil, err
	}
	var pending []map[string]any
	if err := json.Unmarshal(unwrapRuntimePayload(raw), &pending); err != nil {
		return nil, fmt.Errorf("decode runtime permissions: %w", err)
	}
	return pending, nil
}

func permissionID(item map[string]any) string {
	id, _ := item["id"].(string)
	return strings.TrimSpace(id)
}

func permissionSessionID(item map[string]any) string {
	id, _ := item["sessionID"].(string)
	return strings.TrimSpace(id)
}

func (e *permissionEngine) runtimeReply(ctx context.Context, requestID, reply, message string, interactive bool) error {
	payload := map[string]any{
		"reply":       reply,
		"interactive": interactive,
	}
	if strings.TrimSpace(message) != "" {
		payload["message"] = strings.TrimSpace(message)
	}
	_, err := e.runtimeRequest(
		ctx,
		http.MethodPost,
		"/permission/"+url.PathEscape(requestID)+"/reply",
		e.runtimeQuery(),
		payload,
	)
	return err
}

func (e *permissionEngine) shouldAutoAllow(item map[string]any) (bool, error) {
	if !permissionCanRemember(item) {
		return false, nil
	}
	return e.store.covers(e.state.projectPath(), permissionString(item), permissionAlwaysMatchers(item))
}

func (e *permissionEngine) listPending(ctx context.Context, sessionID string) ([]map[string]any, error) {
	visible := e.nativePendingSnapshot(sessionID)
	pending, err := e.rawPending(ctx)
	if err != nil {
		if len(visible) > 0 {
			return visible, nil
		}
		return nil, err
	}
	for _, item := range pending {
		if sessionID != "" && permissionSessionID(item) != sessionID {
			continue
		}
		auto, policyErr := e.shouldAutoAllow(item)
		if policyErr != nil {
			return nil, policyErr
		}
		if auto {
			id := permissionID(item)
			if id != "" {
				if replyErr := e.runtimeReply(ctx, id, "once", "Approved by TL Studio project permission policy.", false); replyErr == nil {
					continue
				}
			}
		}
		visible = append(visible, item)
	}
	return visible, nil
}

func (e *permissionEngine) findPending(ctx context.Context, requestID, sessionID string) (map[string]any, error) {
	pending, err := e.rawPending(ctx)
	if err != nil {
		return nil, err
	}
	for _, item := range pending {
		if permissionID(item) != requestID {
			continue
		}
		if sessionID != "" && permissionSessionID(item) != sessionID {
			continue
		}
		return item, nil
	}
	return nil, os.ErrNotExist
}

func (e *permissionEngine) reply(ctx context.Context, requestID, sessionID, reply, message string) (map[string]any, error) {
	if result, handled, err := e.replyNativePermission(requestID, sessionID, reply); handled {
		return result, err
	}
	item, err := e.findPending(ctx, requestID, sessionID)
	if err != nil {
		return nil, err
	}
	switch reply {
	case "once", "reject":
		if err := e.runtimeReply(ctx, requestID, reply, message, true); err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "reply": reply, "remembered": 0}, nil
	case "always":
		if !permissionCanRemember(item) {
			return nil, errors.New("this permission cannot be remembered safely")
		}
		matchers := permissionAlwaysMatchers(item)
		added, err := e.store.addAllowRules(e.state.projectPath(), permissionString(item), matchers)
		if err != nil {
			return nil, err
		}
		if err := e.runtimeReply(ctx, requestID, "once", message, true); err != nil {
			_ = e.store.removeIDs(added)
			return nil, err
		}
		return map[string]any{"ok": true, "reply": "once", "remembered": len(added)}, nil
	default:
		return nil, errors.New("reply must be once, always, or reject")
	}
}

func writePermissionEngineError(w http.ResponseWriter, err error) {
	if errors.Is(err, os.ErrNotExist) {
		writeJSON(w, http.StatusNotFound, jsonError{Error: "permission request not found"})
		return
	}
	var runtimeErr *runtimePermissionError
	if errors.As(err, &runtimeErr) {
		status := runtimeErr.Status
		if status < 400 || status > 599 {
			status = http.StatusBadGateway
		}
		writeJSON(w, status, jsonError{Error: runtimeErr.Error()})
		return
	}
	writeJSON(w, http.StatusInternalServerError, jsonError{Error: err.Error()})
}

func registerPermissionRoutes(mux *http.ServeMux, engine *permissionEngine) {
	mux.HandleFunc("GET /local/permissions", func(w http.ResponseWriter, r *http.Request) {
		pending, err := engine.listPending(r.Context(), strings.TrimSpace(r.URL.Query().Get("sessionID")))
		if err != nil {
			writePermissionEngineError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, pending)
	})

	mux.HandleFunc("POST /local/permissions/{requestID}/reply", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			SessionID string `json:"sessionID"`
			Reply     string `json:"reply"`
			Message   string `json:"message,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, jsonError{Error: "invalid JSON body"})
			return
		}
		result, err := engine.reply(
			r.Context(),
			strings.TrimSpace(r.PathValue("requestID")),
			strings.TrimSpace(body.SessionID),
			strings.TrimSpace(body.Reply),
			body.Message,
		)
		if err != nil {
			if strings.Contains(err.Error(), "cannot be remembered") || strings.Contains(err.Error(), "reply must be") {
				writeJSON(w, http.StatusBadRequest, jsonError{Error: err.Error()})
				return
			}
			writePermissionEngineError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})

	mux.HandleFunc("GET /local/permissions/rules", func(w http.ResponseWriter, _ *http.Request) {
		rules, err := engine.store.snapshot()
		if err != nil {
			writePermissionEngineError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, rules)
	})

	mux.HandleFunc("DELETE /local/permissions/rules/{ruleID}", func(w http.ResponseWriter, r *http.Request) {
		removed, err := engine.store.remove(strings.TrimSpace(r.PathValue("ruleID")))
		if err != nil {
			writePermissionEngineError(w, err)
			return
		}
		if !removed {
			writeJSON(w, http.StatusNotFound, jsonError{Error: "permission rule not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
}
