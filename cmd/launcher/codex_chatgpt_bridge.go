package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const codexChatGPTProviderProtocol = "codex-chatgpt"

type codexCommand struct {
	Executable string
	PrefixArgs []string
	Source     string
}

func (c codexCommand) valid() bool {
	return strings.TrimSpace(c.Executable) != ""
}

func codexHomeDirectory() string {
	return filepath.Join(tlStudioStateDirectory(), "codex-chatgpt")
}

func codexProcess(ctx context.Context, command codexCommand, args ...string) *exec.Cmd {
	allArgs := append(append([]string(nil), command.PrefixArgs...), args...)
	executable := strings.TrimSpace(command.Executable)
	if runtime.GOOS == "windows" {
		ext := strings.ToLower(filepath.Ext(executable))
		if ext == ".cmd" || ext == ".bat" {
			return exec.CommandContext(ctx, "cmd.exe", "/d", "/s", "/c", windowsBatchCommandLine(executable, allArgs))
		}
	}
	return exec.CommandContext(ctx, executable, allArgs...)
}

func windowsBatchCommandLine(executable string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, quoteWindowsBatchArg(executable))
	for _, arg := range args {
		parts = append(parts, quoteWindowsBatchArg(arg))
	}
	// cmd.exe /S /C strips the outer quote pair. Keep a second quote pair
	// around the batch file itself so paths containing spaces still execute.
	return "\"" + strings.Join(parts, " ") + "\""
}

func quoteWindowsBatchArg(value string) string {
	value = strings.ReplaceAll(value, "%", "%%")
	value = strings.ReplaceAll(value, "\"", "\"\"")
	return "\"" + value + "\""
}

func prepareCodexCommand(cmd *exec.Cmd) error {
	if cmd == nil {
		return errors.New("Codex process is unavailable")
	}
	home := codexHomeDirectory()
	if err := os.MkdirAll(home, 0o700); err != nil {
		return fmt.Errorf("create isolated Codex home: %w", err)
	}
	cmd.Env = append(os.Environ(),
		"CODEX_HOME="+home,
		"NO_COLOR=1",
	)
	return nil
}

type codexRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type codexRPCEnvelope struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *codexRPCError  `json:"error,omitempty"`
}

type boundedTextBuffer struct {
	mu   sync.Mutex
	data []byte
	max  int
}

func newBoundedTextBuffer(max int) *boundedTextBuffer {
	if max <= 0 {
		max = 8 << 10
	}
	return &boundedTextBuffer{max: max}
}

func (b *boundedTextBuffer) Write(p []byte) (int, error) {
	if b == nil {
		return len(p), nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, p...)
	if len(b.data) > b.max {
		b.data = append([]byte(nil), b.data[len(b.data)-b.max:]...)
	}
	return len(p), nil
}

func (b *boundedTextBuffer) String() string {
	if b == nil {
		return ""
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(string(b.data))
}

type codexAppServer struct {
	command codexCommand
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stderr  *boundedTextBuffer

	writeMu sync.Mutex
	nextID  atomic.Int64

	mu        sync.Mutex
	pending   map[int64]chan codexRPCEnvelope
	closed    bool
	closeErr  error
	done      chan struct{}
    waitDone  chan struct{} // closed only after cmd.Wait has reaped the real child
	notify    chan codexRPCEnvelope
}

func startCodexAppServer(command codexCommand) (*codexAppServer, error) {
	if !command.valid() {
		return nil, errors.New("official Codex CLI is not configured")
	}
	ctx := context.Background()
	cmd := codexProcess(ctx, command, "app-server", "--stdio")
    configureManagedCommand(cmd)
	if err := prepareCodexCommand(cmd); err != nil {
		return nil, err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open Codex app-server stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("open Codex app-server stdout: %w", err)
	}
	// Keep only a small in-memory diagnostic tail. It is never written to TL Studio
	// logs and is surfaced only if the helper exits before answering.
	stderr := newBoundedTextBuffer(8 << 10)
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("start official Codex app-server: %w", err)
	}
	server := &codexAppServer{
		command: command,
		cmd: cmd,
		stdin: stdin,
		stderr: stderr,
		pending: map[int64]chan codexRPCEnvelope{},
		done: make(chan struct{}),
        waitDone: make(chan struct{}),
		notify: make(chan codexRPCEnvelope, 64),
	}
	go server.readLoop(stdout)
	go func() {
		err := cmd.Wait()
		server.closeWithError(err)
        close(server.waitDone)
	}()

	initTimeout := 20 * time.Second
	if strings.HasPrefix(command.Source, "npx") {
		initTimeout = 60 * time.Second
	}
	initCtx, cancel := context.WithTimeout(context.Background(), initTimeout)
	defer cancel()
	var initialized map[string]any
	if err := server.request(initCtx, "initialize", map[string]any{
		"clientInfo": map[string]any{
			"name": "tl_studio",
			"title": "TL Studio",
			"version": strings.TrimSpace(version),
		},
		"capabilities": nil,
	}, &initialized); err != nil {
		server.Close()
		return nil, fmt.Errorf("initialize official Codex app-server: %w", err)
	}
	if err := server.notification("initialized", nil); err != nil {
		server.Close()
		return nil, fmt.Errorf("acknowledge Codex app-server initialization: %w", err)
	}
	return server, nil
}


func (s *codexAppServer) readLoop(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), 16<<20)
	for scanner.Scan() {
		var envelope codexRPCEnvelope
		if err := json.Unmarshal(scanner.Bytes(), &envelope); err != nil {
			continue
		}
		if len(envelope.ID) > 0 && string(envelope.ID) != "null" {
			id, err := strconv.ParseInt(strings.Trim(string(envelope.ID), "\""), 10, 64)
			if err == nil {
				s.mu.Lock()
				ch := s.pending[id]
				s.mu.Unlock()
				if ch != nil {
					select {
					case ch <- envelope:
					default:
					}
				}
			}
			continue
		}
		if strings.TrimSpace(envelope.Method) != "" {
			select {
			case s.notify <- envelope:
			default:
			}
		}
	}
	if err := scanner.Err(); err != nil {
		s.closeWithError(err)
	} else {
		s.closeWithError(io.EOF)
	}
}

func (s *codexAppServer) closeWithError(err error) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.closeErr = err
	close(s.done)
	for id, ch := range s.pending {
		delete(s.pending, id)
		close(ch)
	}
	s.mu.Unlock()
}

func (s *codexAppServer) writeMessage(payload any) error {
	if s == nil || s.stdin == nil {
		return errors.New("Codex app-server is unavailable")
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, err = s.stdin.Write(encoded)
	return err
}

func (s *codexAppServer) notification(method string, params any) error {
	payload := map[string]any{"method": strings.TrimSpace(method)}
	if params != nil {
		payload["params"] = params
	}
	return s.writeMessage(payload)
}

func (s *codexAppServer) request(ctx context.Context, method string, params any, output any) error {
	if s == nil {
		return errors.New("Codex app-server is unavailable")
	}
	id := s.nextID.Add(1)
	ch := make(chan codexRPCEnvelope, 1)
	s.mu.Lock()
	if s.closed {
		err := s.closeErr
		s.mu.Unlock()
		if err == nil {
			err = errors.New("Codex app-server is closed")
		}
		return err
	}
	s.pending[id] = ch
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
	}()
	if err := s.writeMessage(map[string]any{
		"id": id,
		"method": strings.TrimSpace(method),
		"params": params,
	}); err != nil {
		return err
	}
	select {
	case envelope, ok := <-ch:
		if !ok {
			return s.stoppedError()
		}
		if envelope.Error != nil {
			return fmt.Errorf("Codex app-server %s failed: %s", method, strings.TrimSpace(envelope.Error.Message))
		}
		if output == nil || len(envelope.Result) == 0 {
			return nil
		}
		if err := json.Unmarshal(envelope.Result, output); err != nil {
			return fmt.Errorf("decode Codex app-server %s response: %w", method, err)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
		return s.stoppedError()
	}
}

func (s *codexAppServer) stoppedError() error {
	if s == nil {
		return errors.New("Codex app-server stopped before responding")
	}
	s.mu.Lock()
	closeErr := s.closeErr
	s.mu.Unlock()
	message := "Codex app-server stopped before responding"
	if closeErr != nil && !errors.Is(closeErr, io.EOF) {
		message += ": " + closeErr.Error()
	}
	if detail := s.stderr.String(); detail != "" {
		message += " — " + detail
	}
	return errors.New(message)
}

func (s *codexAppServer) isClosed() bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *codexAppServer) Close() {
    if s == nil { return }
    if s.stdin != nil { _ = s.stdin.Close() }
    if s.cmd != nil && s.cmd.Process != nil {
        select {
        case <-s.waitDone:
            // Already fully reaped.
        default:
            // Stop the entire command tree, including cmd.exe/npx children
            // on Windows. Killing only the parent leaves background Node and
            // Git processes writing to state after the test/user closes.
            _ = terminateManagedProcess(s.cmd)
            select {
            case <-s.waitDone:
            case <-time.After(3 * time.Second):
                _ = s.cmd.Process.Kill()
                select {
                case <-s.waitDone:
                case <-time.After(2 * time.Second):
                }
            }
        }
    }
    s.closeWithError(nil)
}

type codexAccountInfo struct {
	Type     string `json:"type"`
	Email    string `json:"email,omitempty"`
	PlanType string `json:"planType,omitempty"`
}

type codexAccountReadResponse struct {
	Account *codexAccountInfo `json:"account"`
}

type codexReasoningEffortOption struct {
	ReasoningEffort string `json:"reasoningEffort"`
	Description     string `json:"description,omitempty"`
}

type codexModelListItem struct {
	ID                        string                       `json:"id"`
	Model                     string                       `json:"model"`
	DisplayName               string                       `json:"displayName"`
	Hidden                    bool                         `json:"hidden"`
	IsDefault                 bool                         `json:"isDefault"`
	SupportedReasoningEfforts []codexReasoningEffortOption `json:"supportedReasoningEfforts"`
	DefaultReasoningEffort    string                       `json:"defaultReasoningEffort"`
}

type codexModelListResponse struct {
	Data       []codexModelListItem `json:"data"`
	NextCursor *string              `json:"nextCursor"`
}

func codexReadAccount(ctx context.Context, command codexCommand, refresh bool) (*codexAccountInfo, error) {
	server, err := startCodexAppServer(command)
	if err != nil {
		return nil, err
	}
	defer server.Close()
	var response codexAccountReadResponse
	if err := server.request(ctx, "account/read", map[string]any{"refreshToken": refresh}, &response); err != nil {
		return nil, err
	}
	return response.Account, nil
}

func codexListModels(ctx context.Context, command codexCommand) ([]codexModelListItem, error) {
	server, err := startCodexAppServer(command)
	if err != nil {
		return nil, err
	}
	defer server.Close()
	return codexListModelsWithServer(ctx, server)
}

func codexListModelsWithServer(ctx context.Context, server *codexAppServer) ([]codexModelListItem, error) {
	var all []codexModelListItem
	var cursor *string
	for page := 0; page < 20; page++ {
		params := map[string]any{
			"includeHidden": false,
			"limit": 100,
		}
		if cursor != nil && strings.TrimSpace(*cursor) != "" {
			params["cursor"] = strings.TrimSpace(*cursor)
		}
		var response codexModelListResponse
		if err := server.request(ctx, "model/list", params, &response); err != nil {
			return nil, err
		}
		all = append(all, response.Data...)
		if response.NextCursor == nil || strings.TrimSpace(*response.NextCursor) == "" {
			break
		}
		cursor = response.NextCursor
	}
	if len(all) == 0 {
		return nil, errors.New("Codex returned no available ChatGPT models")
	}
	return all, nil
}

type codexBridgeToolCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type codexBridgeOutput struct {
	Text      string                `json:"text"`
	ToolCalls []codexBridgeToolCall `json:"toolCalls"`
}

func codexBridgeSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"additionalProperties": false,
		"required": []string{"text", "toolCalls"},
		"properties": map[string]any{
			"text": map[string]any{"type": "string"},
			"toolCalls": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"additionalProperties": false,
					"required": []string{"name", "arguments"},
					"properties": map[string]any{
						"name": map[string]any{"type": "string"},
						"arguments": map[string]any{
							"type": "string",
							"description": "JSON-encoded object containing the TL Studio tool arguments.",
						},
					},
				},
			},
		},
	}
}

func codexBridgePrompt(request nativeModelRequest) (string, error) {
	payload := map[string]any{
		"selectedModel": map[string]any{
			"id": strings.TrimSpace(request.Model.ID),
			"name": strings.TrimSpace(request.Model.Name),
			"provider": chatGPTAccountProviderID,
		},
		"system": request.System,
		"messages": request.Messages,
		"tools": request.Tools,
	}
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(`
You are the model transport for TL Studio. TL Studio itself owns the coding-agent loop and tool execution.

For this turn:
- Do not inspect the filesystem.
- Do not execute shell commands.
- Do not browse the web.
- Do not invoke Codex built-in tools, MCP tools, subagents, or file-edit tools.
- Do not modify any files.
- Decide only the next assistant output for the supplied conversation.
- The model identity for this turn is the selectedModel supplied by TL Studio. If the user asks which model or model version is being used, report selectedModel.id exactly and do not replace it with a generic Codex family name.
- If a TL Studio tool is needed, return it in toolCalls and stop. TL Studio will execute it.
- Every toolCalls[].name must exactly match one of the supplied tool IDs.
- toolCalls[].arguments must be a JSON string encoding one object that matches that tool's input schema.
- If no tool is needed, return the assistant text in text and an empty toolCalls array.
- Return only data matching the required output schema.

TL Studio turn payload:
`) + "\n" + string(encoded), nil
}

type codexThreadStartResponse struct {
	Thread struct {
		ID string `json:"id"`
	} `json:"thread"`
}

type codexTurnStartResponse struct {
	Turn struct {
		ID string `json:"id"`
	} `json:"turn"`
}

type codexItemCompletedNotification struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	Item     struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"item"`
}

type codexTurnCompletedNotification struct {
	ThreadID string `json:"threadId"`
	Turn     struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error,omitempty"`
	} `json:"turn"`
}

func codexStructuredThreadConfig() map[string]any {
	return map[string]any{
		"features.apps": false,
		"features.code_mode": false,
		"features.code_mode_only": false,
		"features.context_management": false,
		"features.current_time_reminder": false,
		"features.deferred_executor": false,
		"features.enable_fanout": false,
		"features.goals": false,
		"features.hooks": false,
		"features.image_generation": false,
		"features.memories": false,
		"features.multi_agent": false,
		"features.multi_agent_v2": false,
		"features.plugins": false,
		"features.request_permissions_tool": false,
		"features.shell_snapshot": false,
		"features.shell_tool": false,
		"features.standalone_web_search": false,
		"features.token_budget": false,
		"features.tool_suggest": false,
		"features.unified_exec": false,
		"features.view_image": false,
		"cloud.skills.enabled": false,
		"skills.include_instructions": false,
		"tools.experimental_request_user_input.enabled": false,
		"tools.update_plan.enabled": false,
		"web_search": "disabled",
	}
}

func codexBridgeResponse(request nativeModelRequest, output codexBridgeOutput, onTextDelta func(string)) (nativeModelResponse, error) {
	allowed := map[string]nativeModelToolDefinition{}
	for _, tool := range request.Tools {
		allowed[tool.ID] = tool
		allowed[nativeToolWireName(tool.ID)] = tool
	}
	response := nativeModelResponse{
		Text: strings.TrimSpace(output.Text),
		FinishReason: "completed",
		RoutedModel: strings.TrimSpace(request.Model.ID),
		ToolCalls: []nativeModelToolCall{},
	}
	for _, call := range output.ToolCalls {
		name := strings.TrimSpace(call.Name)
		tool, ok := allowed[name]
		if !ok {
			return nativeModelResponse{}, fmt.Errorf("Codex bridge requested unknown TL Studio tool %q", name)
		}
		argumentsText := strings.TrimSpace(call.Arguments)
		if argumentsText == "" {
			argumentsText = "{}"
		}
		var argumentsObject map[string]any
		if err := json.Unmarshal([]byte(argumentsText), &argumentsObject); err != nil {
			return nativeModelResponse{}, fmt.Errorf("Codex bridge returned invalid JSON arguments for TL Studio tool %q", name)
		}
		arguments, err := json.Marshal(argumentsObject)
		if err != nil {
			return nativeModelResponse{}, err
		}
		callID, err := randomBase64URL(12)
		if err != nil {
			return nativeModelResponse{}, err
		}
		response.ToolCalls = append(response.ToolCalls, nativeModelToolCall{
			ID: "codex_" + callID,
			Name: tool.ID,
			Arguments: arguments,
		})
	}
	if len(response.ToolCalls) > 0 {
		response.FinishReason = "tool_calls"
	}
	if response.Text == "" && len(response.ToolCalls) == 0 {
		return nativeModelResponse{}, errors.New("official Codex bridge returned an empty response")
	}
	if onTextDelta != nil && response.Text != "" {
		onTextDelta(response.Text)
	}
	return response, nil
}

func collectCodexStructuredTurn(ctx context.Context, server *codexAppServer, threadID, turnID string) (string, error) {
	var response string
	for {
		select {
		case notification := <-server.notify:
			switch notification.Method {
			case "item/completed":
				var completed codexItemCompletedNotification
				if err := json.Unmarshal(notification.Params, &completed); err != nil {
					continue
				}
				if completed.ThreadID == threadID && completed.TurnID == turnID &&
					completed.Item.Type == "agentMessage" {
					if len(completed.Item.Text) > 2<<20 {
						return "", errors.New("official Codex bridge response is too large")
					}
					response = completed.Item.Text
				}
			case "turn/completed":
				var completed codexTurnCompletedNotification
				if err := json.Unmarshal(notification.Params, &completed); err != nil {
					continue
				}
				if completed.ThreadID != threadID || completed.Turn.ID != turnID {
					continue
				}
				if completed.Turn.Status != "completed" {
					message := "official Codex structured turn ended with status " + completed.Turn.Status
					if completed.Turn.Error != nil && strings.TrimSpace(completed.Turn.Error.Message) != "" {
						message += ": " + strings.TrimSpace(completed.Turn.Error.Message)
					}
					return "", errors.New(message)
				}
				if strings.TrimSpace(response) == "" {
					return "", errors.New("official Codex structured turn completed without a response")
				}
				return response, nil
			}
		case <-ctx.Done():
			return "", ctx.Err()
		case <-server.done:
			return "", server.stoppedError()
		}
	}
}

func (a *chatGPTAccountAdapter) completeModelTurnWithServer(
	ctx context.Context,
	server *codexAppServer,
	request nativeModelRequest,
	onTextDelta func(string),
) (nativeModelResponse, error) {
	prompt, err := codexBridgePrompt(request)
	if err != nil {
		return nativeModelResponse{}, err
	}
	tempDir, err := os.MkdirTemp("", "tl-studio-codex-turn-*")
	if err != nil {
		return nativeModelResponse{}, err
	}
	defer os.RemoveAll(tempDir)

	var thread codexThreadStartResponse
	if err := server.request(ctx, "thread/start", map[string]any{
		"model": strings.TrimSpace(request.Model.ID),
		"modelProvider": "openai",
		"cwd": tempDir,
		"approvalPolicy": "never",
		"sandbox": "read-only",
		"ephemeral": true,
		"config": codexStructuredThreadConfig(),
	}, &thread); err != nil {
		return nativeModelResponse{}, err
	}
	threadID := strings.TrimSpace(thread.Thread.ID)
	if threadID == "" {
		return nativeModelResponse{}, errors.New("official Codex structured thread returned no thread id")
	}
	defer func() {
		unsubscribeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.request(unsubscribeCtx, "thread/unsubscribe", map[string]any{"threadId": threadID}, nil)
	}()

	turnParams := map[string]any{
		"threadId": threadID,
		"input": []map[string]any{{
			"type": "text",
			"text": prompt,
			"text_elements": []any{},
		}},
		"outputSchema": codexBridgeSchema(),
	}
	if effort := strings.TrimSpace(request.Model.ReasoningEffort); effort != "" {
		turnParams["effort"] = effort
	}
	var turn codexTurnStartResponse
	if err := server.request(ctx, "turn/start", turnParams, &turn); err != nil {
		return nativeModelResponse{}, err
	}
	turnID := strings.TrimSpace(turn.Turn.ID)
	if turnID == "" {
		return nativeModelResponse{}, errors.New("official Codex structured turn returned no turn id")
	}
	data, err := collectCodexStructuredTurn(ctx, server, threadID, turnID)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			a.resetBridgeServerLocked()
		}
		return nativeModelResponse{}, err
	}
	var output codexBridgeOutput
	if err := json.Unmarshal([]byte(data), &output); err != nil {
		return nativeModelResponse{}, errors.New("official Codex bridge returned invalid structured output")
	}
	return codexBridgeResponse(request, output, onTextDelta)
}

func (a *chatGPTAccountAdapter) completeModelTurn(ctx context.Context, request nativeModelRequest, onTextDelta func(string)) (nativeModelResponse, error) {
	if a == nil {
		return nativeModelResponse{}, errors.New("ChatGPT account transport is unavailable")
	}
	command, err := a.resolveCommand()
	if err != nil {
		return nativeModelResponse{}, err
	}
	a.bridgeMu.Lock()
	defer a.bridgeMu.Unlock()
	server, err := a.bridgeServerLocked(command)
	if err != nil {
		return nativeModelResponse{}, err
	}
	response, err := a.completeModelTurnWithServer(ctx, server, request, onTextDelta)
	if err != nil && server.isClosed() {
		a.resetBridgeServerLocked()
	}
	return response, err
}
