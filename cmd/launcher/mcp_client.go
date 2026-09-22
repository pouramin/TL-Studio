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
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	mcpProtocolVersion = "2024-11-05"
	mcpRequestTimeout  = 12 * time.Second
	mcpMaxMessageBytes = 8 << 20
)

type mcpTool struct {
	Name        string         `json:"name"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
	Annotations map[string]any `json:"annotations,omitempty"`
}

type mcpResource struct {
	URI         string `json:"uri"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	MIMEType    string `json:"mimeType,omitempty"`
}

type mcpRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    any    `json:"data,omitempty"`
	} `json:"error,omitempty"`
}

type mcpClient struct {
	config pluginConfig
	cwd    string
	env    map[string]string

	mu         sync.RWMutex
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	pending    map[int64]chan mcpRPCResponse
	tools      []mcpTool
	resources  []mcpResource
	toolIDs    map[string]string
	descriptors map[string]toolDescriptor
	stderr     strings.Builder
	healthy    bool
	closed     bool
	nextID     atomic.Int64
	writeMu    sync.Mutex
	done       chan struct{}
}

func newMCPClient(config pluginConfig, cwd string, env map[string]string) *mcpClient {
	copyEnv := map[string]string{}
	for key, value := range env {
		copyEnv[key] = value
	}
	return &mcpClient{
		config: config, cwd: cwd, env: copyEnv,
		pending: map[int64]chan mcpRPCResponse{},
		toolIDs: map[string]string{}, descriptors: map[string]toolDescriptor{},
		done: make(chan struct{}),
	}
}

func (c *mcpClient) Config() pluginConfig { return c.config }

func (c *mcpClient) Healthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.healthy && !c.closed
}

func (c *mcpClient) stderrText() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return strings.TrimSpace(c.stderr.String())
}

func (c *mcpClient) appendStderr(text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stderr.Len() > 64<<10 {
		current := c.stderr.String()
		c.stderr.Reset()
		if len(current) > 32<<10 {
			current = current[len(current)-(32<<10):]
		}
		c.stderr.WriteString(current)
	}
	c.stderr.WriteString(text)
}

func (c *mcpClient) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return errors.New("MCP client is closed")
	}
	if c.healthy {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	if _, ok := lookPathPluginExecutable(c.config.Command); !ok {
		return fmt.Errorf("MCP executable %q was not found", c.config.Command)
	}
	cmd := exec.Command(c.config.Command, c.config.Arguments...)
	configureManagedCommand(cmd)
	cmd.Dir = c.cwd
	cmd.Env = append([]string(nil), os.Environ()...)
	for key, value := range c.env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil { return err }
	stdout, err := cmd.StdoutPipe()
	if err != nil { return err }
	stderr, err := cmd.StderrPipe()
	if err != nil { return err }

	c.mu.Lock()
	c.cmd = cmd
	c.stdin = stdin
	c.mu.Unlock()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start MCP plugin: %w", err)
	}

	go c.readLoop(stdout)
	go c.stderrLoop(stderr)
	go c.waitLoop()

	startCtx, cancel := context.WithTimeout(ctx, mcpRequestTimeout)
	defer cancel()
	var initialized struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := c.request(startCtx, "initialize", map[string]any{
		"protocolVersion": mcpProtocolVersion,
		"capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "TL Studio", "version": version},
	}, &initialized); err != nil {
		return fmt.Errorf("MCP initialize: %w%s", err, c.stderrSuffix())
	}
	if err := c.notify("notifications/initialized", map[string]any{}); err != nil {
		return fmt.Errorf("MCP initialized notification: %w", err)
	}
	var toolResult struct { Tools []mcpTool `json:"tools"` }
	if err := c.request(startCtx, "tools/list", map[string]any{}, &toolResult); err != nil {
		return fmt.Errorf("MCP tools/list: %w%s", err, c.stderrSuffix())
	}
	var resourceResult struct { Resources []mcpResource `json:"resources"` }
	if err := c.request(startCtx, "resources/list", map[string]any{}, &resourceResult); err != nil {
		// Resources are optional. Method-not-found or unsupported resource capability
		// must not prevent a tools-only MCP server from connecting.
		resourceResult.Resources = nil
	}

	c.mu.Lock()
	c.tools = append([]mcpTool(nil), toolResult.Tools...)
	c.resources = append([]mcpResource(nil), resourceResult.Resources...)
	c.rebuildToolIndexLocked()
	c.healthy = true
	c.mu.Unlock()
	return nil
}

func (c *mcpClient) stderrSuffix() string {
	text := c.stderrText()
	if text == "" { return "" }
	if len(text) > 1000 { text = text[len(text)-1000:] }
	return ": " + text
}

func (c *mcpClient) waitLoop() {
	c.mu.RLock()
	cmd := c.cmd
	c.mu.RUnlock()
	if cmd == nil { return }
	err := cmd.Wait()
	c.mu.Lock()
	c.healthy = false
	if err != nil && !c.closed {
		c.stderr.WriteString("\n[TL Studio] MCP process exited: " + err.Error())
	}
	pending := c.pending
	c.pending = map[int64]chan mcpRPCResponse{}
	select {
	case <-c.done:
	default:
		close(c.done)
	}
	c.mu.Unlock()
	for _, ch := range pending {
		close(ch)
	}
}

func (c *mcpClient) stderrLoop(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		c.appendStderr(scanner.Text() + "\n")
	}
}

func parseMCPResponseID(raw json.RawMessage) (int64, bool) {
	if len(raw) == 0 { return 0, false }
	var number json.Number
	if err := json.Unmarshal(raw, &number); err == nil {
		value, err := strconv.ParseInt(number.String(), 10, 64)
		return value, err == nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		value, err := strconv.ParseInt(text, 10, 64)
		return value, err == nil
	}
	return 0, false
}

func (c *mcpClient) readLoop(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), mcpMaxMessageBytes)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" { continue }
		var response mcpRPCResponse
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			c.appendStderr("[TL Studio] invalid MCP stdout JSON: " + err.Error() + "\n")
			continue
		}
		id, ok := parseMCPResponseID(response.ID)
		if !ok { continue }
		c.mu.Lock()
		ch := c.pending[id]
		delete(c.pending, id)
		c.mu.Unlock()
		if ch != nil {
			ch <- response
			close(ch)
		}
	}
	if err := scanner.Err(); err != nil {
		c.appendStderr("[TL Studio] MCP stdout error: " + err.Error() + "\n")
	}
}

func (c *mcpClient) writeMessage(value any) error {
	data, err := json.Marshal(value)
	if err != nil { return err }
	data = append(data, '\n')
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.mu.RLock()
	stdin := c.stdin
	closed := c.closed
	c.mu.RUnlock()
	if stdin == nil || closed { return errors.New("MCP process is not available") }
	_, err = stdin.Write(data)
	return err
}

func (c *mcpClient) request(ctx context.Context, method string, params any, output any) error {
	id := c.nextID.Add(1)
	ch := make(chan mcpRPCResponse, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return errors.New("MCP client is closed")
	}
	c.pending[id] = ch
	c.mu.Unlock()
	if err := c.writeMessage(map[string]any{"jsonrpc":"2.0","id":id,"method":method,"params":params}); err != nil {
		c.mu.Lock(); delete(c.pending, id); c.mu.Unlock()
		return err
	}
	select {
	case response, ok := <-ch:
		if !ok {
			return errors.New("MCP process exited before responding")
		}
		if response.Error != nil {
			return fmt.Errorf("MCP error %d: %s", response.Error.Code, response.Error.Message)
		}
		if output != nil && len(response.Result) > 0 {
			if err := json.Unmarshal(response.Result, output); err != nil {
				return fmt.Errorf("decode MCP result: %w", err)
			}
		}
		return nil
	case <-ctx.Done():
		c.mu.Lock(); delete(c.pending, id); c.mu.Unlock()
		return ctx.Err()
	case <-c.done:
		c.mu.Lock(); delete(c.pending, id); c.mu.Unlock()
		return errors.New("MCP process exited")
	}
}

func (c *mcpClient) notify(method string, params any) error {
	return c.writeMessage(map[string]any{"jsonrpc":"2.0","method":method,"params":params})
}

func normalizeMCPToolComponent(name string) string {
	name = strings.TrimSpace(name)
	var b strings.Builder
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	value := strings.Trim(b.String(), "_")
	if value == "" { return "tool" }
	return value
}

func boolAnnotation(annotations map[string]any, key string) bool {
	value, _ := annotations[key].(bool)
	return value
}

func classifyMCPTool(config pluginConfig, tool mcpTool) toolDescriptor {
	name := strings.ToLower(tool.Name)
	permissionClass := "unknown"
	capabilities := toolCapabilities{Execute: true}
	if boolAnnotation(tool.Annotations, "readOnlyHint") {
		permissionClass = "read"
		capabilities = toolCapabilities{Read: true}
	} else if boolAnnotation(tool.Annotations, "destructiveHint") {
		permissionClass = "write"
		capabilities = toolCapabilities{Read: true, Write: true}
	} else {
		switch {
		case strings.Contains(name, "exec"), strings.Contains(name, "command"), strings.Contains(name, "shell"), strings.Contains(name, "run_"), strings.HasPrefix(name, "run"), strings.Contains(name, "build"):
			permissionClass = "execute"
			capabilities = toolCapabilities{Read: true, Write: true, Execute: true}
		case strings.Contains(name, "write"), strings.Contains(name, "delete"), strings.Contains(name, "remove"), strings.Contains(name, "update"), strings.Contains(name, "create"), strings.Contains(name, "edit"), strings.Contains(name, "apply"), strings.Contains(name, "set_"):
			permissionClass = "write"
			capabilities = toolCapabilities{Read: true, Write: true}
		case strings.Contains(name, "get"), strings.Contains(name, "list"), strings.Contains(name, "query"), strings.Contains(name, "search"), strings.Contains(name, "read"), strings.Contains(name, "stats"), strings.Contains(name, "neighbor"), strings.Contains(name, "path"), strings.Contains(name, "node"), strings.Contains(name, "community"):
			permissionClass = "read"
			capabilities = toolCapabilities{Read: true}
		}
	}
	if boolAnnotation(tool.Annotations, "openWorldHint") {
		capabilities.Network = true
		if permissionClass == "read" {
			permissionClass = "network"
		}
	}
	id := "mcp." + config.ID + "." + normalizeMCPToolComponent(tool.Name)
	description := strings.TrimSpace(tool.Description)
	if description == "" { description = "Tool discovered from the " + config.Name + " MCP plugin." }
	return toolDescriptor{
		ID: id, RuntimeIDs: []string{id}, Name: firstSessionString(tool.Title, tool.Name),
		Description: description, Category: "plugin", PermissionClass: permissionClass,
		Capabilities: capabilities, Presentation: "plugin-tool", InputSchema: tool.InputSchema,
		Source: "mcp", PluginID: config.ID,
	}
}

func (c *mcpClient) rebuildToolIndexLocked() {
	c.toolIDs = map[string]string{}
	c.descriptors = map[string]toolDescriptor{}
	used := map[string]string{}
	for _, tool := range c.tools {
		descriptor := classifyMCPTool(c.config, tool)
		base := descriptor.ID
		if previous, exists := used[base]; exists && previous != tool.Name {
			sum := sha256.Sum256([]byte(tool.Name))
			descriptor.ID = base + "_" + hex.EncodeToString(sum[:4])
			descriptor.RuntimeIDs = []string{descriptor.ID}
		}
		used[descriptor.ID] = tool.Name
		c.toolIDs[descriptor.ID] = tool.Name
		c.descriptors[descriptor.ID] = descriptor
	}
}

func (c *mcpClient) Tools() []mcpTool {
	c.mu.RLock(); defer c.mu.RUnlock()
	return append([]mcpTool(nil), c.tools...)
}
func (c *mcpClient) Resources() []mcpResource {
	c.mu.RLock(); defer c.mu.RUnlock()
	return append([]mcpResource(nil), c.resources...)
}
func (c *mcpClient) ToolIDs() []string {
	c.mu.RLock(); defer c.mu.RUnlock()
	ids := make([]string, 0, len(c.toolIDs))
	for id := range c.toolIDs { ids = append(ids, id) }
	sort.Strings(ids)
	return ids
}
func (c *mcpClient) ToolDescriptors() []toolDescriptor {
	c.mu.RLock(); defer c.mu.RUnlock()
	result := make([]toolDescriptor, 0, len(c.descriptors))
	for _, descriptor := range c.descriptors { result = append(result, descriptor) }
	sort.Slice(result, func(i,j int) bool { return result[i].ID < result[j].ID })
	return result
}
func (c *mcpClient) ToolDefinitions() []nativeModelToolDefinition {
	descriptors := c.ToolDescriptors()
	result := make([]nativeModelToolDefinition, 0, len(descriptors))
	for _, descriptor := range descriptors {
		schema := descriptor.InputSchema
		if schema == nil { schema = map[string]any{"type":"object"} }
		result = append(result, nativeModelToolDefinition{ID:descriptor.ID, Name:descriptor.Name, Description:descriptor.Description, InputSchema:schema})
	}
	return result
}
func (c *mcpClient) ResolveTool(id string) (toolDescriptor, string, bool) {
	c.mu.RLock(); defer c.mu.RUnlock()
	name, ok := c.toolIDs[id]
	if !ok { return toolDescriptor{}, "", false }
	return c.descriptors[id], name, true
}

func (c *mcpClient) CallTool(ctx context.Context, name string, arguments map[string]any) (any, error) {
	callCtx, cancel := context.WithTimeout(ctx, mcpRequestTimeout)
	defer cancel()
	var result struct {
		Content           []map[string]any `json:"content,omitempty"`
		StructuredContent any              `json:"structuredContent,omitempty"`
		IsError           bool             `json:"isError,omitempty"`
	}
	if err := c.request(callCtx, "tools/call", map[string]any{"name":name,"arguments":arguments}, &result); err != nil {
		return nil, err
	}
	output := map[string]any{"content": result.Content}
	if result.StructuredContent != nil { output["structuredContent"] = result.StructuredContent }
	if result.IsError {
		return output, errors.New("MCP tool returned an error result")
	}
	return output, nil
}

func (c *mcpClient) Close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.healthy = false
	stdin := c.stdin
	cmd := c.cmd
	c.mu.Unlock()
	if stdin != nil { _ = stdin.Close() }
	if cmd != nil && cmd.Process != nil {
		_ = terminateManagedProcess(cmd)
	}
	select {
	case <-c.done:
	case <-time.After(2 * time.Second):
		if cmd != nil && cmd.Process != nil { _ = cmd.Process.Kill() }
	}
}
