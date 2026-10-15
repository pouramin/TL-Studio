package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const maxSemanticSessions = 150

type sessionModelRef struct {
	ProviderID string `json:"providerID,omitempty"`
	ID         string `json:"id,omitempty"`
}

type sessionView struct {
	ID        string           `json:"id"`
	Title     string           `json:"title"`
	Directory string           `json:"directory"`
	ParentID  string           `json:"parentID,omitempty"`
	Agent     string           `json:"agent,omitempty"`
	Model     *sessionModelRef `json:"model,omitempty"`
	Execution string           `json:"execution,omitempty"`
	CreatedAt int64            `json:"createdAt,omitempty"`
	UpdatedAt int64            `json:"updatedAt,omitempty"`
}

type sessionUsage struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	Reasoning  int64 `json:"reasoning"`
	CacheRead  int64 `json:"cacheRead"`
	CacheWrite int64 `json:"cacheWrite"`
}

type sessionErrorView struct {
	Type         string `json:"type,omitempty"`
	Message      string `json:"message,omitempty"`
	Ref          string `json:"ref,omitempty"`
	StatusCode   int    `json:"statusCode,omitempty"`
	ResponseBody string `json:"responseBody,omitempty"`
	Retryable    bool   `json:"retryable,omitempty"`
}

type sessionChangeView struct {
	File      string `json:"file"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Patch     string `json:"patch,omitempty"`
}

type sessionActivityView struct {
	Kind            string              `json:"kind"`
	Status          string              `json:"status,omitempty"`
	Text            string              `json:"text,omitempty"`
	Agent           string              `json:"agent,omitempty"`
	Title           string              `json:"title,omitempty"`
	ToolID          string              `json:"toolID,omitempty"`
	RuntimeToolID   string              `json:"runtimeToolID,omitempty"`
	ToolName        string              `json:"toolName,omitempty"`
	Category        string              `json:"category,omitempty"`
	PermissionClass string              `json:"permissionClass,omitempty"`
	Input           any                 `json:"input,omitempty"`
	Output          any                 `json:"output,omitempty"`
	Error           *sessionErrorView   `json:"error,omitempty"`
	Metadata        map[string]any      `json:"metadata,omitempty"`
	Model           *sessionModelRef    `json:"model,omitempty"`
	Usage           *sessionUsage       `json:"usage,omitempty"`
	Changes         []sessionChangeView `json:"changes,omitempty"`
	StartAt         int64               `json:"startAt,omitempty"`
	EndAt           int64               `json:"endAt,omitempty"`
	Elapsed         int64               `json:"elapsed,omitempty"`
}

type sessionAttachmentView struct {
	Text string `json:"text,omitempty"`
	Name string `json:"name"`
	MIME string `json:"mime,omitempty"`
	URL  string `json:"url,omitempty"`
}

type sessionMessageView struct {
	ID          string                  `json:"id,omitempty"`
	SessionID   string                  `json:"sessionID,omitempty"`
	Role        string                  `json:"role"`
	Agent       string                  `json:"agent,omitempty"`
	Model       *sessionModelRef        `json:"model,omitempty"`
	CreatedAt   int64                   `json:"createdAt,omitempty"`
	CompletedAt int64                   `json:"completedAt,omitempty"`
	Text        string                  `json:"text,omitempty"`
	Error       *sessionErrorView       `json:"error,omitempty"`
	Activities  []sessionActivityView   `json:"activities"`
	Attachments []sessionAttachmentView `json:"attachments"`
	Usage       sessionUsage            `json:"usage"`
	Changes     []sessionChangeView     `json:"changes"`
}

type sessionStatusView struct {
	State   string `json:"state"`
	Active  bool   `json:"active"`
	Attempt int    `json:"attempt,omitempty"`
	NextAt  int64  `json:"nextAt,omitempty"`
	Message string `json:"message,omitempty"`
}

type nativeSessionStatusProvider interface {
	NativeStatuses(directory string) map[string]sessionStatusView
}

type sessionReadContract struct {
	state        *appState
	history      *projectHistoryStore
	store        *sessionPersistenceStore
	nativeStatus nativeSessionStatusProvider
}

func newSessionReadContract(state *appState) *sessionReadContract {
	return &sessionReadContract{
		state:   state,
		history: recentProjects,
		store:   newSessionPersistenceStore(sessionPersistenceRoot(), "native"),
	}
}

func (c *sessionReadContract) setNativeStatusProvider(provider nativeSessionStatusProvider) {
	c.nativeStatus = provider
}

func sessionUsesNativeExecution(session sessionView) bool {
	return strings.TrimSpace(session.Execution) == "native" || strings.TrimSpace(session.Execution) == ""
}

func sessionMap(value any) map[string]any {
	result, _ := value.(map[string]any)
	return result
}

func sessionArray(value any) []any {
	result, _ := value.([]any)
	return result
}

func sessionString(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func sessionInt64(value any) int64 {
	switch number := value.(type) {
	case float64:
		return int64(number)
	case float32:
		return int64(number)
	case int:
		return int64(number)
	case int64:
		return number
	case json.Number:
		result, _ := number.Int64()
		return result
	}
	return 0
}

func sessionBool(value any) bool {
	result, _ := value.(bool)
	return result
}

func sessionModel(value any) *sessionModelRef {
	raw := sessionMap(value)
	if raw == nil {
		return nil
	}
	model := &sessionModelRef{
		ProviderID: sessionString(raw["providerID"]),
		ID:         sessionString(raw["modelID"]),
	}
	if model.ID == "" {
		model.ID = sessionString(raw["id"])
	}
	if model.ProviderID == "" && model.ID == "" {
		return nil
	}
	return model
}

func normalizeSession(raw map[string]any, fallbackDirectory string) sessionView {
	timeValue := sessionMap(raw["time"])
	directory := sessionString(raw["directory"])
	if directory == "" {
		directory = sessionString(raw["path"])
	}
	if directory == "" {
		directory = fallbackDirectory
	}
	return sessionView{
		ID:        sessionString(raw["id"]),
		Title:     sessionString(raw["title"]),
		Directory: directory,
		ParentID:  sessionString(raw["parentID"]),
		Agent:     sessionString(raw["agent"]),
		Model:     sessionModel(raw["model"]),
		CreatedAt: sessionInt64(timeValue["created"]),
		UpdatedAt: maxSessionTimestamp(sessionInt64(timeValue["updated"]), sessionInt64(timeValue["created"])),
	}
}

func maxSessionTimestamp(values ...int64) int64 {
	var result int64
	for _, value := range values {
		if value > result {
			result = value
		}
	}
	return result
}

func normalizeUsage(value any) sessionUsage {
	raw := sessionMap(value)
	cache := sessionMap(raw["cache"])
	return sessionUsage{
		Input:      sessionInt64(raw["input"]),
		Output:     sessionInt64(raw["output"]),
		Reasoning:  sessionInt64(raw["reasoning"]),
		CacheRead:  sessionInt64(cache["read"]),
		CacheWrite: sessionInt64(cache["write"]),
	}
}

func addSessionUsage(target *sessionUsage, source sessionUsage) {
	target.Input += source.Input
	target.Output += source.Output
	target.Reasoning += source.Reasoning
	target.CacheRead += source.CacheRead
	target.CacheWrite += source.CacheWrite
}

func sessionUsageTotal(value sessionUsage) int64 {
	return value.Input + value.Output + value.Reasoning + value.CacheRead + value.CacheWrite
}

func normalizeSessionError(value any) *sessionErrorView {
	if value == nil {
		return nil
	}
	if text, ok := value.(string); ok {
		text = strings.TrimSpace(text)
		if text == "" {
			return nil
		}
		return &sessionErrorView{Message: text}
	}
	raw := sessionMap(value)
	if raw == nil {
		return &sessionErrorView{Message: fmt.Sprint(value)}
	}
	data := sessionMap(raw["data"])
	nested := sessionMap(raw["error"])
	message := sessionString(raw["message"])
	if message == "" {
		message = sessionString(data["message"])
	}
	if message == "" {
		message = sessionString(nested["message"])
	}
	errorType := sessionString(raw["name"])
	if errorType == "" {
		errorType = sessionString(raw["type"])
	}
	if errorType == "" {
		errorType = sessionString(data["name"])
	}
	if errorType == "" {
		errorType = sessionString(data["type"])
	}
	ref := sessionString(raw["ref"])
	if ref == "" {
		ref = sessionString(data["ref"])
	}
	status := int(sessionInt64(raw["statusCode"]))
	if status == 0 {
		status = int(sessionInt64(data["statusCode"]))
	}
	responseBody := sessionString(raw["responseBody"])
	if responseBody == "" {
		responseBody = sessionString(data["responseBody"])
	}
	retryable := sessionBool(raw["isRetryable"]) || sessionBool(data["isRetryable"])
	if errorType == "" && message == "" && ref == "" && status == 0 && responseBody == "" {
		encoded, _ := json.Marshal(raw)
		message = string(encoded)
	}
	return &sessionErrorView{
		Type:         errorType,
		Message:      message,
		Ref:          ref,
		StatusCode:   status,
		ResponseBody: responseBody,
		Retryable:    retryable,
	}
}

func normalizeActivityStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "completed", "success", "done":
		return "completed"
	case "error", "failed", "failure":
		return "failed"
	case "running", "in_progress", "active":
		return "running"
	case "retry", "retrying":
		return "retrying"
	case "":
		return "pending"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func normalizeChange(value any, fallbackPath string) (sessionChangeView, bool) {
	raw := sessionMap(value)
	if raw == nil {
		return sessionChangeView{}, false
	}
	file := sessionString(raw["file"])
	if file == "" {
		file = sessionString(raw["filePath"])
	}
	if file == "" {
		file = sessionString(raw["path"])
	}
	if file == "" {
		file = fallbackPath
	}
	if file == "" {
		return sessionChangeView{}, false
	}
	return sessionChangeView{
		File:      file,
		Additions: int(sessionInt64(raw["additions"])),
		Deletions: int(sessionInt64(raw["deletions"])),
		Patch:     sessionString(raw["patch"]),
	}, true
}

func changesFromToolPart(part map[string]any) []sessionChangeView {
	state := sessionMap(part["state"])
	metadata := sessionMap(state["metadata"])
	input := sessionMap(state["input"])
	output := state["output"]
	if output == nil {
		output = state["result"]
	}
	if output == nil {
		output = part["output"]
	}
	if output == nil {
		output = part["result"]
	}
	outputMap := sessionMap(output)
	fallbackPath := sessionString(input["filePath"])
	if fallbackPath == "" {
		fallbackPath = sessionString(input["path"])
	}
	if fallbackPath == "" {
		fallbackPath = sessionString(input["file"])
	}
	if fallbackPath == "" {
		fallbackPath = sessionString(metadata["filepath"])
	}
	if fallbackPath == "" {
		fallbackPath = sessionString(metadata["path"])
	}

	candidates := []any{
		metadata["filediff"],
		metadata["fileDiff"],
		outputMap["filediff"],
		outputMap["fileDiff"],
	}
	if outputMap != nil {
		if _, ok := outputMap["patch"]; ok {
			candidates = append(candidates, outputMap)
		} else if _, ok := outputMap["additions"]; ok {
			candidates = append(candidates, outputMap)
		} else if _, ok := outputMap["deletions"]; ok {
			candidates = append(candidates, outputMap)
		}
	}
	for _, candidate := range candidates {
		if change, ok := normalizeChange(candidate, fallbackPath); ok {
			return []sessionChangeView{change}
		}
	}
	return nil
}

func normalizeActivity(part map[string]any) (sessionActivityView, bool) {
	kind := sessionString(part["type"])
	partTime := sessionMap(part["time"])
	switch kind {
	case "reasoning":
		status := sessionString(part["status"])
		if sessionInt64(partTime["end"]) > 0 || sessionInt64(partTime["completed"]) > 0 {
			status = "completed"
		}
		return sessionActivityView{
			Kind:    "reasoning",
			Status:  normalizeActivityStatus(status),
			Text:    sessionString(part["text"]),
			StartAt: sessionInt64(partTime["start"]),
			EndAt:   maxSessionTimestamp(sessionInt64(partTime["end"]), sessionInt64(partTime["completed"])),
		}, true
	case "tool":
		state := sessionMap(part["state"])
		stateTime := sessionMap(state["time"])
		runtimeID := sessionString(part["tool"])
		if runtimeID == "" {
			runtimeID = sessionString(part["name"])
		}
		descriptor, _ := toolDescriptorForRuntimeID(runtimeID)
		output := state["output"]
		if output == nil {
			output = state["result"]
		}
		if output == nil {
			output = part["output"]
		}
		if output == nil {
			output = part["result"]
		}
		status := sessionString(state["status"])
		if status == "" {
			status = sessionString(part["status"])
		}
		return sessionActivityView{
			Kind:            "tool",
			Status:          normalizeActivityStatus(status),
			Title:           sessionString(state["title"]),
			ToolID:          descriptor.ID,
			RuntimeToolID:   runtimeID,
			ToolName:        descriptor.Name,
			Category:        descriptor.Category,
			PermissionClass: descriptor.PermissionClass,
			Input:           state["input"],
			Output:          output,
			Error:           normalizeSessionError(state["error"]),
			Metadata:        sessionMap(state["metadata"]),
			Changes:         changesFromToolPart(part),
			StartAt:         maxSessionTimestamp(sessionInt64(partTime["start"]), sessionInt64(stateTime["start"])),
			EndAt:           maxSessionTimestamp(sessionInt64(partTime["end"]), sessionInt64(partTime["completed"]), sessionInt64(stateTime["end"])),
		}, true
	case "subtask":
		return sessionActivityView{
			Kind:   "subtask",
			Status: normalizeActivityStatus(sessionString(part["status"])),
			Agent:  sessionString(part["agent"]),
			Text:   firstSessionString(part["description"], part["prompt"]),
		}, true
	case "step-finish":
		usage := normalizeUsage(part["tokens"])
		return sessionActivityView{
			Kind:    "model",
			Status:  "completed",
			Model:   sessionModel(part["model"]),
			Usage:   &usage,
			StartAt: sessionInt64(partTime["start"]),
			EndAt:   maxSessionTimestamp(sessionInt64(partTime["end"]), sessionInt64(partTime["completed"])),
			Elapsed: sessionInt64(partTime["elapsed"]),
		}, true
	default:
		return sessionActivityView{}, false
	}
}

func firstSessionString(values ...any) string {
	for _, value := range values {
		if text := sessionString(value); text != "" {
			return text
		}
	}
	return ""
}

func normalizeMessage(raw map[string]any) sessionMessageView {
	info := sessionMap(raw["info"])
	timeValue := sessionMap(info["time"])
	parts := sessionArray(raw["parts"])
	message := sessionMessageView{
		ID:          sessionString(info["id"]),
		SessionID:   sessionString(info["sessionID"]),
		Role:        sessionString(info["role"]),
		Agent:       sessionString(info["agent"]),
		Model:       sessionModel(info["model"]),
		CreatedAt:   sessionInt64(timeValue["created"]),
		CompletedAt: maxSessionTimestamp(sessionInt64(timeValue["completed"]), sessionInt64(timeValue["updated"])),
		Error:       normalizeSessionError(info["error"]),
		Activities:  []sessionActivityView{},
		Attachments: []sessionAttachmentView{},
		Changes:     []sessionChangeView{},
		Usage:       normalizeUsage(info["tokens"]),
	}

	var textParts []string
	var fallbackChanges []sessionChangeView
	var stepUsage sessionUsage
	for _, value := range parts {
		part := sessionMap(value)
		if part == nil {
			continue
		}
		partType := sessionString(part["type"])
		if partType == "text" && !sessionBool(part["ignored"]) {
			if text := sessionString(part["text"]); text != "" {
				textParts = append(textParts, text)
			}
		}
		if partType == "file" {
			name := firstSessionString(part["filename"], part["name"])
			if name != "" {
				message.Attachments = append(message.Attachments, sessionAttachmentView{
					Name: name,
					MIME: sessionString(part["mime"]),
					URL:  sessionString(part["url"]),
				})
			}
		}
		if activity, ok := normalizeActivity(part); ok {
			message.Activities = append(message.Activities, activity)
			if activity.Usage != nil {
				addSessionUsage(&stepUsage, *activity.Usage)
			}
			fallbackChanges = append(fallbackChanges, activity.Changes...)
		}
	}
	message.Text = strings.TrimSpace(strings.Join(textParts, "\n"))
	if sessionUsageTotal(message.Usage) == 0 {
		message.Usage = stepUsage
	}

	summary := sessionMap(info["summary"])
	for _, value := range sessionArray(summary["diffs"]) {
		if change, ok := normalizeChange(value, ""); ok {
			message.Changes = append(message.Changes, change)
		}
	}
	if len(message.Changes) == 0 {
		message.Changes = fallbackChanges
	}
	return message
}

func mergeSessionChanges(items []sessionChangeView) []sessionChangeView {
	merged := map[string]sessionChangeView{}
	order := []string{}
	for _, item := range items {
		if item.File == "" {
			continue
		}
		key := strings.ToLower(filepath.ToSlash(item.File))
		previous, exists := merged[key]
		if !exists {
			order = append(order, key)
			merged[key] = item
			continue
		}
		previous.Additions += item.Additions
		previous.Deletions += item.Deletions
		if item.Patch != "" {
			previous.Patch = item.Patch
		}
		if item.File != "" {
			previous.File = item.File
		}
		merged[key] = previous
	}
	result := make([]sessionChangeView, 0, len(order))
	for _, key := range order {
		result = append(result, merged[key])
	}
	return result
}

func normalizeSessionStatus(raw map[string]any) sessionStatusView {
	runtimeType := strings.ToLower(sessionString(raw["type"]))
	result := sessionStatusView{
		State:   "unknown",
		Active:  runtimeType != "" && runtimeType != "idle",
		Attempt: int(sessionInt64(raw["attempt"])),
		NextAt:  sessionInt64(raw["next"]),
		Message: sessionString(raw["message"]),
	}
	switch runtimeType {
	case "idle":
		result.State = "idle"
		result.Active = false
	case "busy", "running", "active":
		result.State = "running"
		result.Active = true
	case "retry", "retrying":
		result.State = "retrying"
		result.Active = true
	case "":
		result.State = "unknown"
		result.Active = false
	default:
		if result.Active {
			result.State = "running"
		}
	}
	return result
}

func (c *sessionReadContract) allowedDirectory(requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	current := c.state.projectPath()
	if requested == "" || sameProjectPath(requested, current) {
		return current, nil
	}
	for _, known := range c.history.list() {
		if sameProjectPath(requested, known) {
			return known, nil
		}
	}
	return "", errors.New("session directory is not in TL Studio recent-project history")
}

func (c *sessionReadContract) listProjectSessions(_ context.Context, directory string, limit int) ([]sessionView, error) {
	if c.store == nil {
		return []sessionView{}, nil
	}
	persisted, err := c.store.list(maxPersistedSessions)
	if err != nil {
		return nil, err
	}
	result := make([]sessionView, 0, len(persisted))
	for _, session := range persisted {
		if sameProjectPath(session.Directory, directory) {
			result = append(result, session)
			if limit > 0 && len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

func (c *sessionReadContract) listSessions(_ context.Context, limit int) ([]sessionView, error) {
	if limit <= 0 || limit > maxSemanticSessions {
		limit = maxSemanticSessions
	}
	current := c.state.projectPath()
	c.history.remember(current)
	if c.store == nil {
		return []sessionView{}, nil
	}
	sessions, err := c.store.list(maxPersistedSessions)
	if err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	for _, project := range c.history.list() {
		allowed[filepath.Clean(project)] = true
	}
	if current != "" {
		allowed[filepath.Clean(current)] = true
	}
	result := make([]sessionView, 0, len(sessions))
	for _, session := range sessions {
		if allowed[filepath.Clean(session.Directory)] {
			result = append(result, session)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].UpdatedAt == result[j].UpdatedAt {
			return result[i].ID < result[j].ID
		}
		return result[i].UpdatedAt > result[j].UpdatedAt
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (c *sessionReadContract) getSession(_ context.Context, sessionID, directory string) (sessionView, error) {
	if c.store == nil {
		return sessionView{}, errors.New("native session store is unavailable")
	}
	session, ok, err := c.store.getSession(strings.TrimSpace(sessionID))
	if err != nil {
		return sessionView{}, err
	}
	if !ok {
		return sessionView{}, errors.New("session not found")
	}
	if directory != "" && !sameProjectPath(session.Directory, directory) {
		return sessionView{}, errors.New("session does not belong to the selected project")
	}
	return session, nil
}

func (c *sessionReadContract) getMessages(_ context.Context, sessionID, directory string, limit int) ([]sessionMessageView, error) {
	if _, err := c.getSession(context.Background(), sessionID, directory); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 2000 {
		limit = 200
	}
	messages, ok, err := c.store.getRecentMessages(strings.TrimSpace(sessionID), limit)
	if err != nil {
		return nil, err
	}
	if !ok {
		return []sessionMessageView{}, nil
	}
	if len(messages) > limit {
		messages = messages[len(messages)-limit:]
	}
	return messages, nil
}

func (c *sessionReadContract) getStatuses(_ context.Context, directory string) (map[string]sessionStatusView, error) {
	if c.nativeStatus == nil {
		return map[string]sessionStatusView{}, nil
	}
	return c.nativeStatus.NativeStatuses(directory), nil
}

func (c *sessionReadContract) getChanges(_ context.Context, sessionID, directory string) ([]sessionChangeView, error) {
	if _, err := c.getSession(context.Background(), sessionID, directory); err != nil {
		return nil, err
	}
	if c.store == nil {
		return []sessionChangeView{}, nil
	}
	changes, ok, err := c.store.getChanges(strings.TrimSpace(sessionID))
	if err != nil {
		return nil, err
	}
	if !ok {
		return []sessionChangeView{}, nil
	}
	return changes, nil
}

func writeSessionContractError(w http.ResponseWriter, err error) {
	message := err.Error()
	switch {
	case strings.Contains(message, "recent-project history"), strings.Contains(message, "does not belong"):
		writeJSON(w, http.StatusForbidden, jsonError{Error: message})
	case strings.Contains(message, "not found"):
		writeJSON(w, http.StatusNotFound, jsonError{Error: message})
	default:
		writeJSON(w, http.StatusInternalServerError, jsonError{Error: message})
	}
}

func registerSessionReadRoutes(mux *http.ServeMux, contract *sessionReadContract) {
	mux.HandleFunc("GET /local/sessions", func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		sessions, err := contract.listSessions(r.Context(), limit)
		if err != nil {
			writeSessionContractError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, sessions)
	})

	mux.HandleFunc("GET /local/sessions/status", func(w http.ResponseWriter, r *http.Request) {
		directory, err := contract.allowedDirectory(r.URL.Query().Get("directory"))
		if err != nil {
			writeSessionContractError(w, err)
			return
		}
		statuses, err := contract.getStatuses(r.Context(), directory)
		if err != nil {
			writeSessionContractError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, statuses)
	})

	mux.HandleFunc("GET /local/sessions/{sessionID}", func(w http.ResponseWriter, r *http.Request) {
		directory, err := contract.allowedDirectory(r.URL.Query().Get("directory"))
		if err != nil {
			writeSessionContractError(w, err)
			return
		}
		session, err := contract.getSession(r.Context(), r.PathValue("sessionID"), directory)
		if err != nil {
			writeSessionContractError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, session)
	})

	mux.HandleFunc("GET /local/sessions/{sessionID}/messages", func(w http.ResponseWriter, r *http.Request) {
		directory, err := contract.allowedDirectory(r.URL.Query().Get("directory"))
		if err != nil {
			writeSessionContractError(w, err)
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		messages, err := contract.getMessages(r.Context(), r.PathValue("sessionID"), directory, limit)
		if err != nil {
			writeSessionContractError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, messages)
	})

	mux.HandleFunc("GET /local/sessions/{sessionID}/changes", func(w http.ResponseWriter, r *http.Request) {
		directory, err := contract.allowedDirectory(r.URL.Query().Get("directory"))
		if err != nil {
			writeSessionContractError(w, err)
			return
		}
		changes, err := contract.getChanges(r.Context(), r.PathValue("sessionID"), directory)
		if err != nil {
			writeSessionContractError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, changes)
	})
}
