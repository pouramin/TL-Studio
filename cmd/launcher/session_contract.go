package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
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

type sessionMessageView struct {
	ID          string                `json:"id,omitempty"`
	SessionID   string                `json:"sessionID,omitempty"`
	Role        string                `json:"role"`
	Agent       string                `json:"agent,omitempty"`
	Model       *sessionModelRef      `json:"model,omitempty"`
	CreatedAt   int64                 `json:"createdAt,omitempty"`
	CompletedAt int64                 `json:"completedAt,omitempty"`
	Text        string                `json:"text,omitempty"`
	Error       *sessionErrorView     `json:"error,omitempty"`
	Activities  []sessionActivityView `json:"activities"`
	Usage       sessionUsage          `json:"usage"`
	Changes     []sessionChangeView   `json:"changes"`
}

type sessionStatusView struct {
	State   string `json:"state"`
	Active  bool   `json:"active"`
	Attempt int    `json:"attempt,omitempty"`
	NextAt  int64  `json:"nextAt,omitempty"`
	Message string `json:"message,omitempty"`
}

type sessionReadContract struct {
	state    *appState
	target   *url.URL
	username string
	password string
	client   *http.Client
	history  *projectHistoryStore
}

type sessionRuntimeError struct {
	Status int
	Body   string
}

func (e *sessionRuntimeError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("runtime session request failed with status %d", e.Status)
	}
	return fmt.Sprintf("runtime session request failed with status %d: %s", e.Status, e.Body)
}

func newSessionReadContract(state *appState, backendURL, username, password string) (*sessionReadContract, error) {
	target, err := url.Parse(backendURL)
	if err != nil {
		return nil, err
	}
	return &sessionReadContract{
		state:    state,
		target:   target,
		username: username,
		password: password,
		client:   &http.Client{},
		history:  recentProjects,
	}, nil
}

func (c *sessionReadContract) runtimeGet(ctx context.Context, route, directory string, query url.Values) (json.RawMessage, error) {
	target := *c.target
	target.Path = route
	if query == nil {
		query = url.Values{}
	}
	if directory != "" {
		query.Set("directory", directory)
	}
	target.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.username, c.password)
	if directory != "" {
		req.Header.Set("x-kilo-directory", strings.ReplaceAll(url.QueryEscape(directory), "+", "%20"))
	}

	response, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, &sessionRuntimeError{Status: response.StatusCode, Body: strings.TrimSpace(string(data))}
	}
	return unwrapRuntimePayload(json.RawMessage(data)), nil
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
		if sessionString(part["type"]) == "text" && !sessionBool(part["ignored"]) {
			if text := sessionString(part["text"]); text != "" {
				textParts = append(textParts, text)
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

func (c *sessionReadContract) listProjectSessions(ctx context.Context, directory string, limit int) ([]sessionView, error) {
	query := url.Values{}
	query.Set("limit", strconv.Itoa(limit))
	query.Set("roots", "true")
	raw, err := c.runtimeGet(ctx, "/session", directory, query)
	if err != nil {
		return nil, err
	}
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("decode runtime sessions: %w", err)
	}
	result := make([]sessionView, 0, len(rows))
	for _, row := range rows {
		session := normalizeSession(row, directory)
		if session.ID != "" {
			result = append(result, session)
		}
	}
	return result, nil
}

func (c *sessionReadContract) listSessions(ctx context.Context, limit int) ([]sessionView, error) {
	if limit <= 0 || limit > maxSemanticSessions {
		limit = maxSemanticSessions
	}
	current := c.state.projectPath()
	c.history.remember(current)
	projects := c.history.list()
	if current != "" && !containsProject(projects, current) {
		projects = append([]string{current}, projects...)
	}
	if len(projects) == 0 {
		return []sessionView{}, nil
	}

	type projectResult struct {
		sessions []sessionView
		err      error
	}
	results := make(chan projectResult, len(projects))
	semaphore := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for _, directory := range projects {
		directory := directory
		if strings.TrimSpace(directory) == "" {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			sessions, err := c.listProjectSessions(ctx, directory, 50)
			results <- projectResult{sessions: sessions, err: err}
		}()
	}
	wg.Wait()
	close(results)

	merged := map[string]sessionView{}
	successes := 0
	var lastErr error
	for result := range results {
		if result.err != nil {
			lastErr = result.err
			continue
		}
		successes++
		for _, session := range result.sessions {
			previous, exists := merged[session.ID]
			if !exists || session.UpdatedAt >= previous.UpdatedAt {
				merged[session.ID] = session
			}
		}
	}
	if successes == 0 && lastErr != nil {
		return nil, lastErr
	}
	list := make([]sessionView, 0, len(merged))
	for _, session := range merged {
		list = append(list, session)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].UpdatedAt == list[j].UpdatedAt {
			return list[i].ID < list[j].ID
		}
		return list[i].UpdatedAt > list[j].UpdatedAt
	})
	if len(list) > limit {
		list = list[:limit]
	}
	return list, nil
}

func (c *sessionReadContract) getSession(ctx context.Context, sessionID, directory string) (sessionView, error) {
	raw, err := c.runtimeGet(ctx, "/session/"+url.PathEscape(sessionID), directory, nil)
	if err != nil {
		return sessionView{}, err
	}
	var row map[string]any
	if err := json.Unmarshal(raw, &row); err != nil {
		return sessionView{}, fmt.Errorf("decode runtime session: %w", err)
	}
	result := normalizeSession(row, directory)
	if result.ID == "" {
		return sessionView{}, errors.New("runtime session is missing an id")
	}
	return result, nil
}

func (c *sessionReadContract) getMessages(ctx context.Context, sessionID, directory string, limit int) ([]sessionMessageView, error) {
	if limit <= 0 || limit > 2000 {
		limit = 200
	}
	query := url.Values{}
	query.Set("limit", strconv.Itoa(limit))
	raw, err := c.runtimeGet(ctx, "/session/"+url.PathEscape(sessionID)+"/message", directory, query)
	if err != nil {
		return nil, err
	}
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("decode runtime session messages: %w", err)
	}
	result := make([]sessionMessageView, 0, len(rows))
	for _, row := range rows {
		message := normalizeMessage(row)
		if message.Role != "" {
			result = append(result, message)
		}
	}
	return result, nil
}

func (c *sessionReadContract) getStatuses(ctx context.Context, directory string) (map[string]sessionStatusView, error) {
	raw, err := c.runtimeGet(ctx, "/session/status", directory, nil)
	if err != nil {
		return nil, err
	}
	var rows map[string]map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("decode runtime session status: %w", err)
	}
	result := make(map[string]sessionStatusView, len(rows))
	for sessionID, row := range rows {
		result[sessionID] = normalizeSessionStatus(row)
	}
	return result, nil
}

func (c *sessionReadContract) getChanges(ctx context.Context, sessionID, directory string) ([]sessionChangeView, error) {
	raw, err := c.runtimeGet(ctx, "/session/"+url.PathEscape(sessionID)+"/diff", directory, nil)
	if err == nil {
		var rows []map[string]any
		if decodeErr := json.Unmarshal(raw, &rows); decodeErr == nil {
			changes := make([]sessionChangeView, 0, len(rows))
			for _, row := range rows {
				if change, ok := normalizeChange(row, ""); ok {
					changes = append(changes, change)
				}
			}
			if len(changes) > 0 {
				return mergeSessionChanges(changes), nil
			}
		}
	}

	messages, messageErr := c.getMessages(ctx, sessionID, directory, 1000)
	if messageErr != nil {
		if err != nil {
			return nil, err
		}
		return nil, messageErr
	}
	var changes []sessionChangeView
	for _, message := range messages {
		changes = append(changes, message.Changes...)
	}
	return mergeSessionChanges(changes), nil
}

func writeSessionContractError(w http.ResponseWriter, err error) {
	var runtimeErr *sessionRuntimeError
	if errors.As(err, &runtimeErr) {
		status := http.StatusBadGateway
		if runtimeErr.Status == http.StatusNotFound {
			status = http.StatusNotFound
		}
		writeJSON(w, status, jsonError{Error: runtimeErr.Error()})
		return
	}
	if strings.Contains(err.Error(), "recent-project history") {
		writeJSON(w, http.StatusForbidden, jsonError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusBadGateway, jsonError{Error: err.Error()})
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
