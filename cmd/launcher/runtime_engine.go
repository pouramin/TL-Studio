package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

type runtimeCredentials struct {
	Username string
	Password string
}

type runtimeEngine interface {
	ID() string
	FindBinary(override string) (string, error)
	Command(ctx context.Context, binary string, port int, credentials runtimeCredentials) *exec.Cmd
	PrepareRequest(req *http.Request, project string, credentials runtimeCredentials)
}

func defaultRuntimeEngine() runtimeEngine {
	return kiloRuntimeEngine{}
}

type runtimeBackend struct {
	state       *appState
	target      *url.URL
	credentials runtimeCredentials
	engine      runtimeEngine
	client      *http.Client
}

func newRuntimeBackend(state *appState, backendURL string, credentials runtimeCredentials, engine runtimeEngine) (*runtimeBackend, error) {
	target, err := url.Parse(backendURL)
	if err != nil {
		return nil, err
	}
	if engine == nil {
		engine = defaultRuntimeEngine()
	}
	return &runtimeBackend{
		state:       state,
		target:      target,
		credentials: credentials,
		engine:      engine,
		client:      &http.Client{},
	}, nil
}

func (b *runtimeBackend) newRequest(
	ctx context.Context,
	method string,
	route string,
	directory string,
	query url.Values,
	body io.Reader,
) (*http.Request, error) {
	target := *b.target
	target.Path = route
	if query != nil {
		target.RawQuery = query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return nil, err
	}
	b.engine.PrepareRequest(req, directory, b.credentials)
	return req, nil
}

func (b *runtimeBackend) reverseProxy() *httputil.ReverseProxy {
	proxy := httputil.NewSingleHostReverseProxy(b.target)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.URL.Path = strings.TrimPrefix(req.URL.Path, "/runtime")
		if req.URL.Path == "" {
			req.URL.Path = "/"
		}
		req.Host = b.target.Host
		project := ""
		if b.state != nil {
			project = b.state.projectPath()
		}
		b.engine.PrepareRequest(req, project, b.credentials)
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		writeJSON(w, http.StatusBadGateway, jsonError{Error: "Runtime backend unavailable: " + err.Error()})
	}
	return proxy
}

func startRuntime(
	ctx context.Context,
	engine runtimeEngine,
	binary string,
	port int,
	credentials runtimeCredentials,
) (*exec.Cmd, error) {
	cmd := engine.Command(ctx, binary, port, credentials)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if runtime.GOOS == "windows" {
		cmd.SysProcAttr = windowsHideProcess()
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}
