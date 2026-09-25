package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

type bundledTestCredentialStore struct {
	mu      sync.Mutex
	secrets map[string]string
}

func newBundledTestCredentialStore() *bundledTestCredentialStore {
	return &bundledTestCredentialStore{secrets: map[string]string{}}
}

func (s *bundledTestCredentialStore) Put(id, secret string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.secrets[id] = secret
	return nil
}

func (s *bundledTestCredentialStore) Get(id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.secrets[id]
	if !ok {
		return "", errCredentialNotFound
	}
	return value, nil
}

func (s *bundledTestCredentialStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.secrets, id)
	return nil
}

func (s *bundledTestCredentialStore) Backend() string { return "memory" }

func withBundledPluginManifests(t *testing.T, manifests []bundledPluginManifest) {
	t.Helper()
	previous := bundledPluginManifests
	bundledPluginManifests = manifests
	t.Cleanup(func() { bundledPluginManifests = previous })
}

func TestBundledPluginExecutableLayout(t *testing.T) {
	manifest := bundledPluginManifest{
		ID: "example",
		Name: "Example",
		Version: "1.2.3",
		Executable: "example-mcp",
		Transport: "stdio",
	}
	normalized, err := normalizeBundledPluginManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join("tmp", "tl-studio")
	path := bundledPluginExecutableAt(root, normalized, "linux")
	if want := filepath.Join(root, "plugins", "example", "bin", "example-mcp"); path != want {
		t.Fatalf("unexpected linux bundled path %q, want %q", path, want)
	}
	windows := bundledPluginExecutableAt(root, normalized, "windows")
	if want := filepath.Join(root, "plugins", "example", "bin", "example-mcp.exe"); windows != want {
		t.Fatalf("unexpected windows bundled path %q, want %q", windows, want)
	}
}

func TestMinimalBundledPluginEnvironmentExcludesUnrelatedSecrets(t *testing.T) {
	t.Setenv("TL_STUDIO_TEST_SECRET", "DO_NOT_INHERIT")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "DO_NOT_INHERIT_EITHER")
	t.Setenv("PATH", os.Getenv("PATH"))

	joined := strings.Join(minimalBundledPluginEnvironment(), "\n")
	if strings.Contains(joined, "TL_STUDIO_TEST_SECRET") || strings.Contains(joined, "AWS_SECRET_ACCESS_KEY") ||
		strings.Contains(joined, "DO_NOT_INHERIT") {
		t.Fatalf("bundled process inherited unrelated secret environment: %s", joined)
	}
	if !strings.Contains(joined, "PATH=") {
		t.Fatalf("bundled process should retain ordinary runtime PATH: %s", joined)
	}
}

func TestBundledPluginRunsThroughGenericMCPManager(t *testing.T) {
	stateDir := t.TempDir()
	project := filepath.Join(stateDir, "project")
	packageRoot := filepath.Join(stateDir, "package")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TL_STUDIO_STATE_DIR", stateDir)
	t.Setenv("TL_STUDIO_BUNDLED_PLUGIN_ROOT", packageRoot)

	executableName := bundledPluginExecutableName(bundledPluginManifest{Executable: "fake-bundled"}, runtime.GOOS)
	executable := filepath.Join(packageRoot, "plugins", "fake-bundled", "bin", executableName)
	if err := os.MkdirAll(filepath.Dir(executable), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(os.Args[0], executable); err != nil {
		// Some filesystems do not support hard links. A byte copy is acceptable
		// for this test and keeps the resolver contract cross-platform.
		data, readErr := os.ReadFile(os.Args[0])
		if readErr != nil {
			t.Fatal(readErr)
		}
		if writeErr := os.WriteFile(executable, data, 0o755); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(executable, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	withBundledPluginManifests(t, []bundledPluginManifest{{
		ID: "fake-bundled",
		Name: "Fake Bundled MCP",
		Description: "Bundled MCP regression fixture.",
		Version: "1.0.0",
		Executable: "fake-bundled",
		Arguments: []string{"-test.run=TestMCPHelperProcess"},
		Environment: []string{"GO_WANT_MCP_HELPER"},
		Transport: "stdio",
		License: "MIT",
		Upstream: "test",
	}})

	credentials := newBundledTestCredentialStore()
	config, found, err := bundledPluginConfigByID("fake-bundled")
	if err != nil || !found {
		t.Fatalf("bundled config lookup failed: %#v %v %v", config, found, err)
	}
	if config.Metadata["origin"] != "bundled" || config.Metadata["bundledVersion"] != "1.0.0" {
		t.Fatalf("bundled metadata missing: %#v", config.Metadata)
	}
	if err := credentials.Put(pluginCredentialID(config, "GO_WANT_MCP_HELPER"), "1"); err != nil {
		t.Fatal(err)
	}

	manager := &pluginManager{
		store: newPluginStore(filepath.Join(stateDir, "plugins.json")),
		credentials: credentials,
		clients: map[string]mcpPluginClient{},
		errors: map[string]string{},
	}
	view, err := manager.SetEnabled(project, "fake-bundled", true)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	if view.Origin != "bundled" || view.Version != "1.0.0" {
		t.Fatalf("unexpected bundled view metadata: %#v", view)
	}
	if view.Status != "Connected" || view.DiscoveredTools != 3 {
		t.Fatalf("bundled plugin did not use generic MCP discovery: %#v", view)
	}
	definitions := manager.ToolDefinitions(project)
	if len(definitions) != 3 {
		t.Fatalf("expected bundled MCP tools in native definitions, got %#v", definitions)
	}

	if _, err := manager.Upsert(project, pluginUpsertRequest{Plugin: pluginConfig{
		ID: "fake-bundled", Name: "Spoof", Type: "mcp", Scope: "global", Transport: "stdio", Command: "spoof",
	}}); err == nil || !strings.Contains(err.Error(), "reserved by a bundled plugin") {
		t.Fatalf("user config should not be able to shadow bundled plugin identity: %v", err)
	}

	disabled, err := manager.SetEnabled(project, "fake-bundled", false)
	if err != nil {
		t.Fatal(err)
	}
	if disabled.Enabled || disabled.Status != "Disabled" {
		t.Fatalf("bundled plugin did not disable cleanly: %#v", disabled)
	}
	manager.mu.Lock()
	clientCount := len(manager.clients)
	manager.mu.Unlock()
	if clientCount != 0 {
		t.Fatalf("disabled bundled plugin left %d MCP clients running", clientCount)
	}
}

func TestBundledPluginsCannotBeRemoved(t *testing.T) {
	t.Setenv("TL_STUDIO_STATE_DIR", t.TempDir())
	withBundledPluginManifests(t, []bundledPluginManifest{{
		ID: "keep-bundled", Name: "Keep", Version: "1", Executable: "keep", Transport: "stdio",
	}})
	manager := &pluginManager{store: newPluginStore(filepath.Join(t.TempDir(), "plugins.json"))}
	err := manager.Remove(t.TempDir(), "keep-bundled")
	if err == nil || !strings.Contains(err.Error(), "cannot be removed") {
		t.Fatalf("expected bundled remove protection, got %v", err)
	}
}
