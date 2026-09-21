package main

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestKiloRuntimeEnvironmentAppliesSafeDefaults(t *testing.T) {
	credentials := runtimeCredentials{Username: "runtime-user", Password: "runtime-pass"}
	env := kiloRuntimeEnvironment([]string{"PATH=/tmp/bin"}, credentials)

	for key, want := range map[string]string{
		"KILO_SERVER_USERNAME":     "runtime-user",
		"KILO_SERVER_PASSWORD":     "runtime-pass",
		"KILO_TELEMETRY_LEVEL":     "off",
		"DO_NOT_TRACK":              "1",
		"OTEL_SDK_DISABLED":         "true",
		"KILO_ENABLE_QUESTION_TOOL": "true",
		"KILO_PARENT_PID":           strconv.Itoa(currentProcessID()),
		"KILO_CONFIG_CONTENT":       `{"permission":{"edit":"ask"}}`,
	} {
		if got := runtimeEnvValue(env, key); got != want {
			t.Fatalf("%s=%q, want %q", key, got, want)
		}
	}
}

func TestKiloRuntimeEnvironmentPreservesExplicitConfig(t *testing.T) {
	custom := `{"permission":{"edit":"allow"}}`
	env := kiloRuntimeEnvironment(
		[]string{"KILO_CONFIG_CONTENT=" + custom},
		runtimeCredentials{Username: "runtime", Password: "secret"},
	)
	if got := runtimeEnvValue(env, "KILO_CONFIG_CONTENT"); got != custom {
		t.Fatalf("explicit KILO_CONFIG_CONTENT changed: %q", got)
	}
}

func TestKiloRuntimeEngineBuildsLocalServeCommand(t *testing.T) {
	engine := kiloRuntimeEngine{}
	cmd := engine.Command(
		context.Background(),
		"/tmp/kilo",
		4567,
		runtimeCredentials{Username: "runtime", Password: "secret"},
	)
	args := strings.Join(cmd.Args, " ")
	for _, required := range []string{"/tmp/kilo", "serve", "--hostname 127.0.0.1", "--port 4567"} {
		if !strings.Contains(args, required) {
			t.Fatalf("runtime command missing %q: %s", required, args)
		}
	}
}

func TestKiloRuntimeEngineOwnsRequestDecoration(t *testing.T) {
	engine := kiloRuntimeEngine{}
	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1/runtime", nil)
	if err != nil {
		t.Fatal(err)
	}
	engine.PrepareRequest(req, "/tmp/project folder", runtimeCredentials{Username: "runtime", Password: "secret"})

	user, pass, ok := req.BasicAuth()
	if !ok || user != "runtime" || pass != "secret" {
		t.Fatalf("unexpected runtime auth: %q %q %v", user, pass, ok)
	}
	if got := req.Header.Get("x-kilo-directory"); got == "" || !strings.Contains(got, "project%20folder") {
		t.Fatalf("missing encoded project scope header: %q", got)
	}
}

func currentProcessID() int {
	// Keep the assertion independent from any package init side effect.
	return processID()
}
