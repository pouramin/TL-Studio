package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMCPHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_MCP_HELPER") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), mcpMaxMessageBytes)
	write := func(value any) {
		data, _ := json.Marshal(value)
		fmt.Fprintln(os.Stdout, string(data))
	}
	for scanner.Scan() {
		var message map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			continue
		}
		method, _ := message["method"].(string)
		id, hasID := message["id"]
		switch method {
		case "initialize":
			if hasID {
				write(map[string]any{
					"jsonrpc": "2.0",
					"id": id,
					"result": map[string]any{
						"protocolVersion": mcpProtocolVersion,
						"capabilities": map[string]any{
							"tools": map[string]any{},
							"resources": map[string]any{},
						},
						"serverInfo": map[string]any{"name":"fake-mcp","version":"1.0"},
					},
				})
			}
		case "notifications/initialized":
			// notification
		case "tools/list":
			write(map[string]any{
				"jsonrpc":"2.0","id":id,
				"result":map[string]any{"tools":[]any{
					map[string]any{
						"name":"echo","description":"Echo structured input.",
						"inputSchema":map[string]any{
							"type":"object",
							"properties":map[string]any{"text":map[string]any{"type":"string"}},
						},
						"annotations":map[string]any{"readOnlyHint":true},
					},
					map[string]any{
						"name":"update_file","description":"Fake mutating tool.",
						"inputSchema":map[string]any{"type":"object"},
						"annotations":map[string]any{"destructiveHint":true},
					},
					map[string]any{
						"name":"sleep","description":"Wait for cancellation.",
						"inputSchema":map[string]any{"type":"object"},
					},
				}},
			})
		case "resources/list":
			write(map[string]any{
				"jsonrpc":"2.0","id":id,
				"result":map[string]any{"resources":[]any{
					map[string]any{"uri":"fake://resource","name":"Fake resource","mimeType":"text/plain"},
				}},
			})
		case "tools/call":
			params, _ := message["params"].(map[string]any)
			name, _ := params["name"].(string)
			args, _ := params["arguments"].(map[string]any)
			if name == "sleep" {
				time.Sleep(2 * time.Second)
			}
			write(map[string]any{
				"jsonrpc":"2.0","id":id,
				"result":map[string]any{
					"content":[]any{map[string]any{"type":"text","text":"ok:"+name}},
					"structuredContent":map[string]any{"name":name,"arguments":args},
				},
			})
		default:
			if hasID {
				write(map[string]any{
					"jsonrpc":"2.0","id":id,
					"error":map[string]any{"code":-32601,"message":"method not found"},
				})
			}
		}
	}
	os.Exit(0)
}

func fakeMCPConfig(project string, enabled bool) pluginConfig {
	return pluginConfig{
		ID: "fake",
		Name: "Fake MCP",
		Type: pluginTypeMCP,
		Enabled: enabled,
		Scope: "project",
		Project: project,
		Transport: pluginTransportStdio,
		Command: os.Args[0],
		Arguments: []string{"-test.run=TestMCPHelperProcess"},
		Environment: []pluginEnvironmentRef{
			{Name:"GO_WANT_MCP_HELPER",Configured:true},
			{Name:"TEST_PLUGIN_SECRET",Configured:true},
		},
	}
}

func fakeMCPEnvironment() map[string]string {
	return map[string]string{
		"GO_WANT_MCP_HELPER":"1",
		"TEST_PLUGIN_SECRET":"SUPER_SECRET_VALUE",
	}
}

func TestPluginStorePersistsProjectScope(t *testing.T) {
	temp := t.TempDir()
	project := filepath.Join(temp, "project")
	other := filepath.Join(temp, "other")
	if err := os.MkdirAll(project, 0o755); err != nil { t.Fatal(err) }
	if err := os.MkdirAll(other, 0o755); err != nil { t.Fatal(err) }

	store := newPluginStore(filepath.Join(temp, "plugins.json"))
	config, err := normalizePluginConfig(pluginConfig{
		ID:"example", Name:"Example", Type:"mcp", Scope:"project", Transport:"stdio",
		Command:"example-mcp", Project:project,
	}, project)
	if err != nil { t.Fatal(err) }
	if err := store.upsert(config); err != nil { t.Fatal(err) }

	reloaded := newPluginStore(filepath.Join(temp, "plugins.json"))
	items, err := reloaded.list(project)
	if err != nil { t.Fatal(err) }
	if len(items) != 1 || items[0].ID != "example" {
		t.Fatalf("unexpected project plugins: %#v", items)
	}
	items, err = reloaded.list(other)
	if err != nil { t.Fatal(err) }
	if len(items) != 0 {
		t.Fatalf("project plugin leaked into another project: %#v", items)
	}
}

func TestMCPClientInitializesDiscoversAndCallsTools(t *testing.T) {
	project := t.TempDir()
	config := fakeMCPConfig(project, true)
	client := newMCPClient(config, project, fakeMCPEnvironment())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	if got := client.ToolIDs(); len(got) != 3 {
		t.Fatalf("expected 3 discovered tools, got %#v", got)
	}
	descriptor, original, ok := client.ResolveTool("mcp.fake.echo")
	if !ok || original != "echo" {
		t.Fatalf("echo tool was not namespaced/resolved: %#v %q %v", descriptor, original, ok)
	}
	if descriptor.PermissionClass != "read" || !descriptor.Capabilities.Read {
		t.Fatalf("read-only MCP tool was classified incorrectly: %#v", descriptor)
	}
	writeDescriptor, _, ok := client.ResolveTool("mcp.fake.update_file")
	if !ok || writeDescriptor.PermissionClass != "write" || !writeDescriptor.Capabilities.Write {
		t.Fatalf("mutating MCP tool was classified incorrectly: %#v", writeDescriptor)
	}
	if len(client.Resources()) != 1 {
		t.Fatalf("expected optional resource discovery, got %#v", client.Resources())
	}
	output, err := client.CallTool(ctx, "echo", map[string]any{"text":"hello"})
	if err != nil { t.Fatal(err) }
	encoded, _ := json.Marshal(output)
	if !strings.Contains(string(encoded), "hello") {
		t.Fatalf("tool result did not round-trip structured arguments: %s", encoded)
	}
}

func TestMCPClientCancellation(t *testing.T) {
	project := t.TempDir()
	client := newMCPClient(fakeMCPConfig(project, true), project, fakeMCPEnvironment())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Start(ctx); err != nil { t.Fatal(err) }
	defer client.Close()

	callCtx, callCancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer callCancel()
	_, err := client.CallTool(callCtx, "sleep", map[string]any{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

type recordingPluginAuthorizer struct {
	mu sync.Mutex
	calls []toolDescriptor
}

func (a *recordingPluginAuthorizer) AuthorizeNativeTool(_ context.Context, _ string, _ string, descriptor toolDescriptor, _ map[string]any) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, descriptor)
	return nil
}

func (a *recordingPluginAuthorizer) count() int {
	a.mu.Lock(); defer a.mu.Unlock()
	return len(a.calls)
}

func newTestPluginManager(t *testing.T, project string, authorizer nativeToolAuthorizer) *pluginManager {
	t.Helper()
	stateDir := t.TempDir()
	t.Setenv("TL_STUDIO_STATE_DIR", stateDir)
	state := &appState{project:project}
	manager := newPluginManager(state, newProcessManager(state.projectPath), authorizer)
	manager.store = newPluginStore(filepath.Join(stateDir, "plugins.json"))
	manager.credentials = privateFileCredentialStore{}
	t.Cleanup(manager.Close)
	return manager
}

func TestPluginManagerDisabledEnabledAgentExecutionAndSecretRedaction(t *testing.T) {
	project := t.TempDir()
	auth := &recordingPluginAuthorizer{}
	manager := newTestPluginManager(t, project, auth)
	config := fakeMCPConfig(project, false)
	env := fakeMCPEnvironment()
	request := pluginUpsertRequest{Plugin:config, Environment:&env}
	view, err := manager.Upsert(project, request)
	if err != nil { t.Fatal(err) }
	if view.Status != "Disabled" {
		t.Fatalf("disabled plugin unexpectedly started: %#v", view)
	}
	if len(manager.clients) != 0 {
		t.Fatalf("disabled plugin created a client: %#v", manager.clients)
	}
	encoded, _ := json.Marshal(view)
	if strings.Contains(string(encoded), "SUPER_SECRET_VALUE") {
		t.Fatalf("secret leaked through plugin view: %s", encoded)
	}

	view, err = manager.SetEnabled(project, config.ID, true)
	if err != nil { t.Fatal(err) }
	if view.Status != "Connected" || view.DiscoveredTools != 3 {
		t.Fatalf("enabled plugin did not connect/discover tools: %#v", view)
	}
	definitions := manager.ToolDefinitions(project)
	found := false
	for _, definition := range definitions {
		if definition.ID == "mcp.fake.echo" { found = true }
	}
	if !found {
		t.Fatalf("enabled plugin tool was not exposed to native Agent: %#v", definitions)
	}

	result, handled := manager.Execute(context.Background(), "session-test", project, nativeToolCall{
		ID:"mcp.fake.echo", CallID:"call-1", Arguments:json.RawMessage(`{"text":"from-agent"}`),
	})
	if !handled || result.Error != "" {
		t.Fatalf("MCP tool execution failed: handled=%v result=%#v", handled, result)
	}
	if auth.count() != 1 {
		t.Fatalf("MCP tool bypassed permission authorizer: %d calls", auth.count())
	}

	view, err = manager.SetEnabled(project, config.ID, false)
	if err != nil { t.Fatal(err) }
	if view.Status != "Disabled" {
		t.Fatalf("disable did not update status: %#v", view)
	}
	if len(manager.clients) != 0 {
		t.Fatalf("disable did not stop MCP process")
	}
}

func TestGraphifyConnectionTestDoesNotRequireBuiltGraph(t *testing.T) {
	project := t.TempDir()
	manager := newTestPluginManager(t, project, &recordingPluginAuthorizer{})
	config := fakeMCPConfig(project, false)
	config.Metadata = map[string]string{"integration": "graphify"}
	env := fakeMCPEnvironment()

	view, err := manager.TestConfig(context.Background(), project, pluginUpsertRequest{
		Plugin: config,
		Environment: &env,
	})
	if err != nil {
		t.Fatalf("connection test should validate MCP connectivity independently from Graphify graph readiness: %v", err)
	}
	if view.Status != "Connected" || view.DiscoveredTools != 3 {
		t.Fatalf("unexpected Graphify connection-test result: %#v", view)
	}
	if view.Integration == nil || view.Integration.Status != "Graph Missing" {
		t.Fatalf("connection test should still report integration readiness separately: %#v", view.Integration)
	}
}

func TestGraphifyCannotEnableBeforeGraphExists(t *testing.T) {
	project := t.TempDir()
	manager := newTestPluginManager(t, project, &recordingPluginAuthorizer{})
	config := fakeMCPConfig(project, false)
	config.Metadata = map[string]string{"integration": "graphify"}
	env := fakeMCPEnvironment()

	if _, err := manager.Upsert(project, pluginUpsertRequest{Plugin: config, Environment: &env}); err != nil {
		t.Fatal(err)
	}
	view, err := manager.SetEnabled(project, config.ID, true)
	if err == nil || !strings.Contains(err.Error(), "Graphify graph is missing") {
		t.Fatalf("expected graph readiness error before enable, got view=%#v err=%v", view, err)
	}
	stored, found, findErr := manager.store.find(project, config.ID)
	if findErr != nil || !found {
		t.Fatalf("saved plugin disappeared after rejected enable: found=%v err=%v", found, findErr)
	}
	if stored.Enabled {
		t.Fatal("rejected enable must not persist an enabled plugin")
	}
}

func TestUnknownMCPToolUsesSaferPermissionClass(t *testing.T) {
	descriptor := classifyMCPTool(pluginConfig{ID:"x",Name:"X"}, mcpTool{
		Name:"mystery_capability",
		InputSchema:map[string]any{"type":"object"},
	})
	if descriptor.PermissionClass != "unknown" || !descriptor.Capabilities.Execute {
		t.Fatalf("unknown tool should use safer permission path: %#v", descriptor)
	}
}

func TestPluginManagerReportsMissingExecutable(t *testing.T) {
	project := t.TempDir()
	manager := newTestPluginManager(t, project, &recordingPluginAuthorizer{})
	config := pluginConfig{
		ID:"missing",Name:"Missing",Type:"mcp",Enabled:true,Scope:"project",Project:project,
		Transport:"stdio",Command:"definitely-not-a-real-mcp-executable-12345",
	}
	if err := manager.store.upsert(config); err != nil { t.Fatal(err) }
	views, err := manager.List(project, true)
	if err != nil { t.Fatal(err) }
	if len(views) != 1 || views[0].Status != "Error" {
		t.Fatalf("missing process did not surface useful error status: %#v", views)
	}
}

func TestNativeToolsRemainAvailableWithoutPlugins(t *testing.T) {
	executor := newNativeToolExecutor(nil, nil)
	ids := map[string]bool{}
	for _, tool := range executor.ToolDefinitions() {
		ids[tool.ID] = true
	}
	for _, required := range []string{"files.read","files.list","files.write","files.edit","search.content","terminal.command"} {
		if !ids[required] {
			t.Fatalf("native tool %s disappeared after plugin integration", required)
		}
	}
}
