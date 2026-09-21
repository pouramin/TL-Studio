package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type boundaryTestRuntimeEngine struct{}

func (boundaryTestRuntimeEngine) ID() string { return "test-engine" }

func (boundaryTestRuntimeEngine) FindBinary(override string) (string, error) {
	return override, nil
}

func (boundaryTestRuntimeEngine) Command(ctx context.Context, binary string, _ int, _ runtimeCredentials) *exec.Cmd {
	return exec.CommandContext(ctx, binary)
}

func (boundaryTestRuntimeEngine) PrepareRequest(req *http.Request, project string, credentials runtimeCredentials) {
	req.Header.Set("X-TL-Test-Project", project)
	req.Header.Set("X-TL-Test-Auth", credentials.Username+":"+credentials.Password)
}

func TestRuntimeBackendDelegatesRequestDecorationToEngine(t *testing.T) {
	project := t.TempDir()
	runtimeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-TL-Test-Project"); got != project {
			t.Fatalf("engine did not receive project scope: %q", got)
		}
		if got := r.Header.Get("X-TL-Test-Auth"); got != "runtime:secret" {
			t.Fatalf("engine did not receive runtime credentials: %q", got)
		}
		if got := r.Header.Get("x-kilo-directory"); got != "" {
			t.Fatalf("generic runtime backend injected engine-specific project header: %q", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer runtimeServer.Close()

	state := &appState{project: project}
	backend, err := newRuntimeBackend(
		state,
		runtimeServer.URL,
		runtimeCredentials{Username: "runtime", Password: "secret"},
		boundaryTestRuntimeEngine{},
	)
	if err != nil {
		t.Fatal(err)
	}
	req, err := backend.newRequest(context.Background(), http.MethodGet, "/health", project, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := backend.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("unexpected runtime status: %d", res.StatusCode)
	}
}

func TestServerRuntimeProxyUsesSelectedEngineAdapter(t *testing.T) {
	project := t.TempDir()
	runtimeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/global/health" {
			t.Fatalf("unexpected runtime path: %s", r.URL.Path)
		}
		if got := r.Header.Get("X-TL-Test-Project"); got != project {
			t.Fatalf("runtime proxy bypassed engine adapter: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer runtimeServer.Close()

	state := &appState{project: project, backendURL: runtimeServer.URL, frontendURL: "http://127.0.0.1"}
	handler, err := newServerWithRuntime(
		state,
		runtimeServer.URL,
		runtimeCredentials{Username: "runtime", Password: "secret"},
		boundaryTestRuntimeEngine{},
	)
	if err != nil {
		t.Fatal(err)
	}
	frontend := httptest.NewServer(handler)
	defer frontend.Close()

	res, err := http.Get(frontend.URL + "/runtime/global/health")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("unexpected proxy status: %d", res.StatusCode)
	}
}

func TestRuntimeEngineBoundaryKeepsKiloRoutingOutOfGenericLauncher(t *testing.T) {
	root := releaseRepoRoot(t)
	for _, relative := range []string{
		"cmd/launcher/main.go",
		"cmd/launcher/runtime_engine.go",
		"cmd/launcher/session_contract.go",
		"cmd/launcher/session_command_contract.go",
		"cmd/launcher/live_event_contract.go",
		"cmd/launcher/permission_engine.go",
	} {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatal(err)
		}
		source := string(data)
		for _, forbidden := range []string{
			"x-kilo-directory",
			"KILO_SERVER_",
			"KILO_PARENT_PID",
			"findKiloBinary",
			"startKilo",
			"configureKiloRuntimeDefaults",
		} {
			if strings.Contains(source, forbidden) {
				t.Fatalf("%s leaked Kilo lifecycle/routing detail %q", relative, forbidden)
			}
		}
	}
}
