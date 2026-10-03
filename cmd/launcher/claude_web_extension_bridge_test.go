package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClaudeWebExtensionBridgeStartsDisconnectedUntilPaired(t *testing.T) {
	bridge := newClaudeWebExtensionBridge(&appState{frontendURL: "http://127.0.0.1:32123"})
	probe, err := bridge.Probe(context.Background())
	if !errors.Is(err, errClaudeWebExtensionNotPaired) {
		t.Fatalf("expected unpaired bridge error, got probe=%#v err=%v", probe, err)
	}
}

func TestClaudeWebExtensionBridgeProbeRoundTrip(t *testing.T) {
	bridge := newClaudeWebExtensionBridge(&appState{frontendURL: "http://127.0.0.1:32123"})
	bridge.token = "pair-token"
	bridge.paired = true

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result := make(chan claudeWebProbe, 1)
	fail := make(chan error, 1)
	go func() {
		probe, err := bridge.Probe(ctx)
		if err != nil { fail <- err; return }
		result <- probe
	}()

	deadline := time.Now().Add(time.Second)
	var command *claudeWebExtensionCommand
	for time.Now().Before(deadline) {
		bridge.mu.Lock()
		if len(bridge.queue) > 0 {
			item := bridge.queue[0]
			bridge.queue = bridge.queue[1:]
			command = &item
		}
		bridge.mu.Unlock()
		if command != nil { break }
		time.Sleep(10 * time.Millisecond)
	}
	if command == nil || command.Kind != "probe" {
		t.Fatalf("probe command was not queued: %#v", command)
	}
	if !bridge.accept("pair-token", claudeWebExtensionResult{ID: command.ID, OK: true, Connected: true, OrganizationID: "org_test"}) {
		t.Fatal("bridge rejected valid result")
	}
	select {
	case err := <-fail:
		t.Fatal(err)
	case probe := <-result:
		if !probe.Connected || probe.OrganizationID != "org_test" { t.Fatalf("unexpected probe: %#v", probe) }
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestClaudeWebUIRelayPairsPollsAndReturnsResult(t *testing.T) {
	bridge := newClaudeWebExtensionBridge(&appState{frontendURL: "http://127.0.0.1:32123"})
	bridge.token = "pair-token"
	mux := http.NewServeMux()
	registerClaudeWebUIRelayRoutes(mux, bridge)

	pairReq := httptest.NewRequest(http.MethodPost, "/local/claude-web-ui/pair?token=pair-token", nil)
	pairRes := httptest.NewRecorder()
	mux.ServeHTTP(pairRes, pairReq)
	if pairRes.Code != http.StatusOK { t.Fatalf("pair returned HTTP %d: %s", pairRes.Code, pairRes.Body.String()) }

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := bridge.Complete(ctx, "hello", "claude-sonnet-5-5"); done <- err }()

	deadline := time.Now().Add(time.Second)
	var command claudeWebExtensionCommand
	for time.Now().Before(deadline) {
		req := httptest.NewRequest(http.MethodGet, "/local/claude-web-ui/poll?token=pair-token", nil)
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		if res.Code != http.StatusOK { t.Fatalf("poll returned HTTP %d: %s", res.Code, res.Body.String()) }
		var payload struct { Command *claudeWebExtensionCommand `json:"command"` }
		if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil { t.Fatal(err) }
		if payload.Command != nil { command = *payload.Command; break }
		time.Sleep(10 * time.Millisecond)
	}
	if command.ID == "" || command.Kind != "complete" || command.Prompt != "hello" || command.Model != "claude-sonnet-5-5" {
		t.Fatalf("completion command was not relayed: %#v", command)
	}
	body, err := json.Marshal(claudeWebExtensionResult{ID: command.ID, OK: true, Text: "done"})
	if err != nil { t.Fatal(err) }
	resultReq := httptest.NewRequest(http.MethodPost, "/local/claude-web-ui/result?token=pair-token", bytes.NewReader(body))
	resultRes := httptest.NewRecorder()
	mux.ServeHTTP(resultRes, resultReq)
	if resultRes.Code != http.StatusOK { t.Fatalf("result returned HTTP %d: %s", resultRes.Code, resultRes.Body.String()) }
	select {
	case err := <-done:
		if err != nil { t.Fatal(err) }
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestClaudeWebExtensionReusesPersistentPageContext(t *testing.T) {
	manifest := readRepoText(t, "integrations/claude-web-extension/manifest.json")
	background := readRepoText(t, "integrations/claude-web-extension/background.js")
	accounts := readBrowserSource(t, "provider-account-ui.ts")

	for _, required := range []string{
		`"permissions": ["scripting"]`,
		`"https://claude.ai/*"`,
		`"externally_connectable"`,
		`"http://127.0.0.1/*"`,
		`"http://localhost/*"`,
	} {
		if !strings.Contains(manifest, required) { t.Fatalf("Claude Web manifest missing %q", required) }
	}
	for _, forbidden := range []string{`"cookies"`, `"debugger"`, "declarativeNetRequest"} {
		if strings.Contains(manifest, forbidden) { t.Fatalf("Claude Web manifest must not request %q", forbidden) }
	}

	for _, required := range []string{
		"chrome.tabs.query",
		"chrome.tabs.create",
		"transportTabId",
		"transportTabOwned",
		"chrome.scripting.executeScript",
		`world: "MAIN"`,
		`credentials: "include"`,
		`"/api/organizations"`,
		`"/chat_conversations/"`,
		`"/completion"`,
		`method: "DELETE"`,
		`event.type === "message_stop"`,
		`event.type === "completion_stop"`,
		`stopReason === "stop_sequence"`,
		`stopReason === "end_turn"`,
		`stopReason === "max_tokens"`,
		"await reader.cancel()",
		`BRIDGE_VERSION = "0.6.3-persistent-page"`,
		`"claude-fable-5-1"`,
		`"claude-opus-5-5"`,
		`"claude-sonnet-5-5"`,
		`"claude-haiku-4-5"`,
		"tlstudio-unpair",
	} {
		if !strings.Contains(background, required) { t.Fatalf("Claude Web persistent page transport missing %q", required) }
	}
	if strings.Count(background, "chrome.tabs.create(") != 1 {
		t.Fatalf("Claude Web transport must have exactly one tab-creation site, got %d", strings.Count(background, "chrome.tabs.create("))
	}
	for _, forbidden := range []string{
		"chrome.cookies", "sessionKey", "document.cookie", "Network.getAllCookies",
		"Storage.getCookies", "CryptUnprotectData", "chrome.debugger",
		"powershell.exe", "UIAutomationClient", "SendKeys", "Clipboard",
		"--remote-debugging-port",
	} {
		if strings.Contains(background, forbidden) { t.Fatalf("Claude Web transport contains forbidden browser/control path %q", forbidden) }
	}
	for _, required := range []string{
		`CLAUDE_WEB_EXTENSION_IDS = [`,
		`"cpellhbmfdhcgkblnmnppndmeiigmjcg"`,
		`"hklkkfhbcohbfpojbcanhgmfanjhnfna"`,
		`let claudeWebExtensionID = ""`,
	} {
		if !strings.Contains(accounts, required) {
			t.Fatalf("TL Studio Claude Web extension identity contract missing %q", required)
		}
	}
	for _, required := range []string{
		"tlstudio-ping", "tlstudio-pair-direct", "tlstudio-execute-direct", "tlstudio-unpair",
		"token: cleanToken", `CLAUDE_WEB_BRIDGE_VERSION = "0.6.3-persistent-page"`,
	} {
		if !strings.Contains(accounts, required) { t.Fatalf("provider UI missing extension relay contract %q", required) }
	}
}

func TestClaudeWebExtensionHasNoTLStudioAgentAuthority(t *testing.T) {
	background := readRepoText(t, "integrations/claude-web-extension/background.js")
	bridge := readRepoText(t, "cmd/launcher/claude_web_extension_bridge.go")
	for _, forbidden := range []string{
		"/local/files", "/local/process", "/local/permission", "/local/session",
		"/local/tools", "/local/plugins", "exec.Command", "os.ReadFile", "os.WriteFile",
	} {
		if strings.Contains(background, forbidden) || strings.Contains(bridge, forbidden) {
			t.Fatalf("Claude Web transport must not own TL Studio Agent capability %q", forbidden)
		}
	}
	for _, required := range []string{`Kind: "probe"`, `Kind: "complete"`} {
		if !strings.Contains(bridge, required) { t.Fatalf("inference-only bridge command missing %q", required) }
	}
	mainSource := readRepoText(t, "cmd/launcher/main.go")
	for _, required := range []string{
		"newNativeAgentRuntime",
		"newNativeToolExecutor",
		"newPermissionEngine",
		"registerClaudeWebUIRelayRoutes",
	} {
		if !strings.Contains(mainSource, required) { t.Fatalf("TL Studio ownership contract missing %q", required) }
	}
}
