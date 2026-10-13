package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This helper commits a visible side effect, then deliberately closes its MCP
// transport without acknowledging the call. The outcome is ambiguous to the
// caller even though the operation has already happened.
func TestMCPAmbiguousCompletionHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_MCP_AMBIGUOUS_COMPLETION") != "1" {
		return
	}
	write := func(value any) {
		encoded, err := json.Marshal(value)
		if err == nil {
			fmt.Fprintln(os.Stdout, string(encoded))
		}
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), mcpMaxMessageBytes)
	for scanner.Scan() {
		var request struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			continue
		}
		switch request.Method {
		case "initialize":
			write(map[string]any{
				"jsonrpc": "2.0", "id": request.ID,
				"result": map[string]any{
					"protocolVersion": mcpProtocolVersion,
					"capabilities":    map[string]any{"tools": map[string]any{}, "resources": map[string]any{}},
					"serverInfo":      map[string]any{"name": "ambiguous-test", "version": "1"},
				},
			})
		case "notifications/initialized":
		case "tools/list":
			write(map[string]any{
				"jsonrpc": "2.0", "id": request.ID,
				"result": map[string]any{"tools": []any{
					map[string]any{
						"name":        "mutate_file",
						"description": "Append an irreversible test marker.",
						"inputSchema": map[string]any{"type": "object"},
						"annotations": map[string]any{"destructiveHint": true},
					},
				}},
			})
		case "resources/list":
			write(map[string]any{
				"jsonrpc": "2.0", "id": request.ID,
				"result": map[string]any{"resources": []any{}},
			})
		case "tools/call":
			f, err := os.OpenFile("mcp-side-effects.txt", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
			if err != nil {
				os.Exit(2)
			}
			if _, err := f.WriteString("committed\\n"); err != nil {
				os.Exit(3)
			}
			if err := f.Sync(); err != nil {
				os.Exit(4)
			}
			_ = f.Close()
			// No JSON-RPC reply. The caller cannot know if the operation committed.
			os.Exit(0)
		default:
			if request.ID != nil {
				write(map[string]any{
					"jsonrpc": "2.0", "id": request.ID,
					"error": map[string]any{"code": -32601, "message": "unknown method"},
				})
			}
		}
	}
	os.Exit(0)
}

func TestMCPAmbiguousWriteIsNotRetried(t *testing.T) {
	project := t.TempDir()
	manager := newTestPluginManager(t, project, &recordingPluginAuthorizer{})
	config := pluginConfig{
		ID: "ambiguous", Name: "Ambiguous MCP", Type: pluginTypeMCP,
		Scope: "project", Project: project, Transport: pluginTransportStdio,
		Command: os.Args[0], Arguments: []string{"-test.run=^TestMCPAmbiguousCompletionHelperProcess$"},
		Environment: []pluginEnvironmentRef{{Name: "GO_WANT_MCP_AMBIGUOUS_COMPLETION", Configured: true}},
	}
	env := map[string]string{"GO_WANT_MCP_AMBIGUOUS_COMPLETION": "1"}
	if _, err := manager.Upsert(project, pluginUpsertRequest{Plugin: config, Environment: &env}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.SetEnabled(project, config.ID, true); err != nil {
		t.Fatal(err)
	}
	result, handled := manager.Execute(context.Background(), "session-ambiguous", project, nativeToolCall{
		ID: "mcp.ambiguous.mutate_file", CallID: "side-effect-once",
		Arguments: json.RawMessage(`{}`),
	})
	if !handled || !strings.Contains(result.Error, "not retried") {
		t.Fatalf("ambiguous MCP call should report non-retryable error, handled=%v result=%+v", handled, result)
	}
	data, err := os.ReadFile(filepath.Join(project, "mcp-side-effects.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "committed\\n" {
		t.Fatalf("MCP operation was replayed or not executed, side effects=%q", data)
	}
}
