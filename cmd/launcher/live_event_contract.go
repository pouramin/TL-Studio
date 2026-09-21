package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const liveEventContractVersion = 1

type liveEventView struct {
	Version       int    `json:"version"`
	Type          string `json:"type"`
	Action        string `json:"action,omitempty"`
	SessionID     string `json:"sessionID,omitempty"`
	MessageID     string `json:"messageID,omitempty"`
	AttentionKind string `json:"attentionKind,omitempty"`
	Path          string `json:"path,omitempty"`
}

type liveEventContract struct {
	state    *appState
	target   *url.URL
	username string
	password string
	client   *http.Client
}

func newLiveEventContract(state *appState, backendURL, username, password string) (*liveEventContract, error) {
	target, err := url.Parse(backendURL)
	if err != nil {
		return nil, err
	}
	return &liveEventContract{
		state:    state,
		target:   target,
		username: username,
		password: password,
		client:   &http.Client{},
	}, nil
}

func eventMap(value any) map[string]any {
	if mapped, ok := value.(map[string]any); ok {
		return mapped
	}
	return nil
}

func eventString(values ...any) string {
	for _, value := range values {
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

func eventNestedString(root map[string]any, keys ...string) string {
	current := any(root)
	for _, key := range keys {
		mapped, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = mapped[key]
	}
	return eventString(current)
}

func eventAction(runtimeType string) string {
	switch {
	case strings.Contains(runtimeType, ".created"):
		return "created"
	case strings.Contains(runtimeType, ".deleted"), strings.Contains(runtimeType, ".removed"):
		return "removed"
	case strings.Contains(runtimeType, ".asked"):
		return "requested"
	case strings.Contains(runtimeType, ".replied"), strings.Contains(runtimeType, ".rejected"):
		return "resolved"
	case strings.Contains(runtimeType, ".part."):
		return "content"
	case strings.Contains(runtimeType, ".status"), strings.HasSuffix(runtimeType, ".idle"), strings.HasSuffix(runtimeType, ".error"):
		return "state"
	default:
		return "changed"
	}
}

func unwrapRuntimeEvent(raw map[string]any) map[string]any {
	if payload := eventMap(raw["payload"]); payload != nil {
		return payload
	}
	return raw
}

func projectRuntimeEvent(raw map[string]any) (liveEventView, bool) {
	event := unwrapRuntimeEvent(raw)
	runtimeType := eventString(event["type"])
	if runtimeType == "" {
		return liveEventView{}, false
	}
	props := eventMap(event["properties"])
	if props == nil {
		props = eventMap(event["data"])
	}
	if props == nil {
		props = map[string]any{}
	}

	sessionID := eventString(
		props["sessionID"],
		props["sessionId"],
		eventNestedString(props, "info", "sessionID"),
		eventNestedString(props, "part", "sessionID"),
		eventNestedString(props, "message", "sessionID"),
		eventNestedString(props, "session", "id"),
		eventNestedString(props, "session", "sessionID"),
	)
	messageID := eventString(
		props["messageID"],
		props["messageId"],
		eventNestedString(props, "info", "id"),
		eventNestedString(props, "part", "messageID"),
		eventNestedString(props, "message", "id"),
	)
	path := eventString(
		props["path"],
		props["file"],
		eventNestedString(props, "file", "path"),
	)

	projected := liveEventView{
		Version:   liveEventContractVersion,
		Action:    eventAction(runtimeType),
		SessionID: sessionID,
		MessageID: messageID,
		Path:      path,
	}

	switch {
	case runtimeType == "server.connected":
		projected.Type = "stream.ready"
		projected.Action = ""
	case strings.HasPrefix(runtimeType, "permission."):
		projected.Type = "attention.changed"
		projected.AttentionKind = "permission"
	case strings.HasPrefix(runtimeType, "question."):
		projected.Type = "attention.changed"
		projected.AttentionKind = "question"
	case strings.HasPrefix(runtimeType, "message."):
		projected.Type = "message.changed"
	case strings.HasPrefix(runtimeType, "session."):
		projected.Type = "session.changed"
	case strings.HasPrefix(runtimeType, "file."), strings.HasPrefix(runtimeType, "fs."), strings.HasPrefix(runtimeType, "watcher."):
		projected.Type = "workspace.changed"
	default:
		return liveEventView{}, false
	}
	return projected, true
}

func (c *liveEventContract) runtimeStream(ctx context.Context, directory string) (*http.Response, error) {
	target := *c.target
	target.Path = "/global/event"
	query := target.Query()
	if directory != "" {
		query.Set("directory", directory)
	}
	target.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("Accept", "text/event-stream")
	if directory != "" {
		req.Header.Set("x-kilo-directory", strings.ReplaceAll(url.QueryEscape(directory), "+", "%20"))
	}

	response, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		defer response.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		return nil, fmt.Errorf("runtime event stream failed with status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	return response, nil
}

func writeLiveEvent(w io.Writer, event liveEventView) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", data)
	return err
}

func projectSSEStream(w http.ResponseWriter, response *http.Response) error {
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 64*1024), 8<<20)
	dataLines := make([]string, 0, 2)
	flusher, _ := w.(http.Flusher)

	flushData := func() error {
		if len(dataLines) == 0 {
			return nil
		}
		payload := strings.Join(dataLines, "\n")
		dataLines = dataLines[:0]

		var decoded map[string]any
		if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
			return nil
		}
		event, ok := projectRuntimeEvent(decoded)
		if !ok {
			return nil
		}
		if err := writeLiveEvent(w, event); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := flushData(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			continue
		}
		if strings.HasPrefix(line, ":") && flusher != nil {
			_, _ = io.WriteString(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
	if err := flushData(); err != nil {
		return err
	}
	return scanner.Err()
}

func registerLiveEventRoutes(mux *http.ServeMux, contract *liveEventContract) {
	mux.HandleFunc("GET /local/events", func(w http.ResponseWriter, r *http.Request) {
		directory := contract.state.projectPath()
		response, err := contract.runtimeStream(r.Context(), directory)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, jsonError{Error: err.Error()})
			return
		}
		defer response.Body.Close()

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		_ = projectSSEStream(w, response)
	})
}
