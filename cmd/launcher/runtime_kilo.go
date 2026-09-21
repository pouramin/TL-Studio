package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

type kiloRuntimeEngine struct{}

func defaultRuntimeEngine() runtimeEngine {
	return kiloRuntimeEngine{}
}

func (kiloRuntimeEngine) ID() string {
	return "kilo-code"
}

func (kiloRuntimeEngine) FindBinary(override string) (string, error) {
	candidates := []string{}
	if strings.TrimSpace(override) != "" {
		candidates = append(candidates, override)
	}
	if env := strings.TrimSpace(os.Getenv("TL_STUDIO_RUNTIME_BIN")); env != "" {
		candidates = append(candidates, env)
	} else if legacyEnv := strings.TrimSpace(os.Getenv("KILO_BIN")); legacyEnv != "" {
		candidates = append(candidates, legacyEnv)
	}
	if exe, err := os.Executable(); err == nil {
		base := filepath.Dir(exe)
		name := "kilo"
		if runtime.GOOS == "windows" {
			name = "kilo.exe"
		}
		candidates = append(candidates, filepath.Join(base, "bin", name), filepath.Join(base, name))
	}
	if path, err := exec.LookPath("kilo"); err == nil {
		candidates = append(candidates, path)
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		abs, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if info, err := os.Stat(abs); err == nil && !info.IsDir() {
			return abs, nil
		}
	}
	return "", errors.New("bundled agent runtime not found; reinstall TL Studio or use --runtime-bin for an advanced local override")
}

func (kiloRuntimeEngine) Command(ctx context.Context, binary string, port int, credentials runtimeCredentials) *exec.Cmd {
	cmd := exec.CommandContext(ctx, binary, "serve", "--hostname", "127.0.0.1", "--port", fmt.Sprint(port))
	cmd.Env = kiloRuntimeEnvironment(os.Environ(), credentials)
	return cmd
}

func (kiloRuntimeEngine) PrepareRequest(req *http.Request, project string, credentials runtimeCredentials) {
	req.SetBasicAuth(credentials.Username, credentials.Password)
	if strings.TrimSpace(project) != "" {
		req.Header.Set("x-kilo-directory", strings.ReplaceAll(url.QueryEscape(project), "+", "%20"))
	}
}

func kiloRuntimeEnvironment(base []string, credentials runtimeCredentials) []string {
	env := append([]string(nil), base...)
	env = setRuntimeEnv(env, "KILO_SERVER_USERNAME", credentials.Username)
	env = setRuntimeEnv(env, "KILO_SERVER_PASSWORD", credentials.Password)
	env = setRuntimeEnv(env, "KILO_TELEMETRY_LEVEL", "off")
	env = setRuntimeEnv(env, "DO_NOT_TRACK", "1")
	env = setRuntimeEnv(env, "OTEL_SDK_DISABLED", "true")
	env = setRuntimeEnv(env, "KILO_ENABLE_QUESTION_TOOL", "true")
	env = setRuntimeEnv(env, "KILO_PARENT_PID", strconv.Itoa(os.Getpid()))
	if strings.TrimSpace(runtimeEnvValue(env, "KILO_CONFIG_CONTENT")) == "" {
		env = setRuntimeEnv(env, "KILO_CONFIG_CONTENT", `{"permission":{"edit":"ask"}}`)
	}
	return env
}

func runtimeEnvValue(env []string, key string) string {
	prefix := key + "="
	for i := len(env) - 1; i >= 0; i-- {
		if strings.HasPrefix(env[i], prefix) {
			return strings.TrimPrefix(env[i], prefix)
		}
	}
	return ""
}

func setRuntimeEnv(env []string, key, value string) []string {
	prefix := key + "="
	result := make([]string, 0, len(env)+1)
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			continue
		}
		result = append(result, item)
	}
	return append(result, prefix+value)
}
