package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

var version = "dev"

//go:embed web/*
var webFS embed.FS

type appState struct {
	mu          sync.RWMutex
	project     string
	frontendURL string
	authSecret  string
	ctx         context.Context
}

func (s *appState) snapshot() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]any{
		"version":     version,
		"project":     s.project,
		"frontendURL": s.frontendURL,
		"platform":    runtime.GOOS,
		"arch":        runtime.GOARCH,
		"runtime":     map[string]any{"mode": "native"},
	}
}

func (s *appState) setProject(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.project = path
}

func (s *appState) projectPath() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.project
}

type jsonError struct {
	Error string `json:"error"`
}

func main() {
	var projectArg string
	var noBrowser bool
	var listenAddr string
	flag.StringVar(&projectArg, "project", "", "project directory to open")
	flag.BoolVar(&noBrowser, "no-browser", false, "do not open the browser automatically")
	flag.StringVar(&listenAddr, "listen", "127.0.0.1", "frontend listen address")
	flag.Parse()

	if !isLoopbackHost(listenAddr) {
		log.Fatalf("listen: %q is not a loopback address; this UI intentionally binds only to localhost", listenAddr)
	}
	if projectArg == "" && flag.NArg() > 0 {
		projectArg = flag.Arg(0)
	}
	project, err := normalizeProject(projectArg)
	if err != nil {
		log.Fatalf("project: %v", err)
	}
	frontendPort, err := freePort(listenAddr)
	if err != nil {
		log.Fatalf("find frontend port: %v", err)
	}
	frontendURL := "http://" + net.JoinHostPort(listenAddr, fmt.Sprint(frontendPort))
	launchSecret, err := randomSecret(32)
	if err != nil {
		log.Fatalf("initialize local API authentication: %v", err)
	}
	state := &appState{project: project, frontendURL: frontendURL, authSecret: launchSecret}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	state.ctx = ctx

	stateLock, err := acquireStateDirectoryLock(tlStudioStateDirectory())
	if err != nil {
		log.Fatalf("state ownership: %v", err)
	}
	defer stateLock.Close()
	server, closeServices, err := newServerWithLifecycle(state)
	if err != nil {
		log.Fatalf("create local server: %v", err)
	}
	defer closeServices()
	httpServer := &http.Server{
		Addr:              net.JoinHostPort(listenAddr, fmt.Sprint(frontendPort)),
		Handler:           server,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		closeServices()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer shutdownCancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	fmt.Printf("TL Studio %s\n", version)
	fmt.Printf("  Project: %s\n", project)
	fmt.Printf("  Local:   %s\n", frontendURL)
	if noBrowser || runtime.GOOS != "windows" {
		// A non-browser CLI launch needs a credential for its own local API calls.
		// Do not expose a bootstrap URL in Unix process arguments.
		fmt.Printf("  Auth:    %s\n", launchSecret)
	}

	if !noBrowser {
		go func() {
			time.Sleep(250 * time.Millisecond)
			address := frontendURL
			if runtime.GOOS == "windows" {
				address = frontendURL + "/#tl_auth=" + launchSecret
			}
			if err := openBrowser(address); err != nil {
				log.Printf("open browser: %v", err)
			}
		}()
	}
	err = httpServer.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("local server: %v", err)
	}
}

func newServer(state *appState) (http.Handler, error) {
	handler, _, err := newServerWithLifecycle(state)
	return handler, err
}

func newServerWithLifecycle(state *appState) (http.Handler, func(), error) {
	providerManager := newProviderManager(state)
	openRouterAccount := newOpenRouterAccountAdapter(state, providerManager)
	huggingFaceAccount := newHuggingFaceAccountAdapter(state, providerManager)
	googleGeminiAccount := newGoogleGeminiAccountAdapter(state, providerManager)
	chatGPTAccount := newChatGPTAccountAdapter(state, providerManager)
	claudeAccount := newClaudeAccountAdapter(state, providerManager)
	claudeWebBridge := newClaudeWebExtensionBridge(state)
	claudeWebAccount := newClaudeWebAccountAdapterWithTransport(state, providerManager, claudeWebBridge)
	providerManager.registerAccountAdapter(openRouterAccount)
	providerManager.registerAccountAdapter(huggingFaceAccount)
	providerManager.registerAccountAdapter(googleGeminiAccount)
	providerManager.registerAccountAdapter(chatGPTAccount)
	providerManager.registerAccountAdapter(claudeAccount)
	providerManager.registerAccountAdapter(claudeWebAccount)
	providerAccounts := newProviderAccountService(
		openRouterAccount,
		huggingFaceAccount,
		googleGeminiAccount,
		chatGPTAccount,
		claudeAccount,
		claudeWebAccount,
		newGitHubCopilotAccountBoundaryAdapter(),
	)
	jevRouter := newJevRouterService(providerManager)
	decisionEngines := newDecisionEngineService(providerManager)
	permissionEngine := newPermissionEngine(state)
	liveEvents := newLiveEventContract(state)
	permissionEngine.setEventBus(liveEvents.bus)
	questions := newQuestionContract(state, liveEvents.bus)

	processes := newProcessManager(state.projectPath)
	plugins := newPluginManager(state, processes, permissionEngine)
	layaRouter := newLayaRouterService(providerManager, plugins)
	jevDirectRouter := newJevDirectRouterService(providerManager, plugins, layaRouter)
	providerManager.setCatalogDecorator(func(ctx context.Context, directory string, result *providerCatalogResponse) error {
		if err := layaRouter.decorateCatalog(ctx, directory, result); err != nil {
			return err
		}
		return jevDirectRouter.decorateCatalog(ctx, directory, result)
	})
	nativeTools := newNativeToolExecutor(processes, permissionEngine)
	nativeTools.setPluginManager(plugins)
	nativeTools.setQuestionManager(questions)

	sessionRead := newSessionReadContract(state)
	nativeAgent := newNativeAgentRuntime(providerManager, newNativeModelClient(chatGPTAccount, claudeAccount, claudeWebAccount), nativeTools, sessionRead.store, liveEvents.bus)
	nativeAgent.appContext = state.ctx
	nativeAgent.setRequestRouter(newNativeRequestRouterMux(layaRouter, jevDirectRouter))
	sessionRead.setNativeStatusProvider(nativeAgent)
	sessionCommands := newSessionCommandContract(state, sessionRead, nativeAgent)
	var cleanupOnce sync.Once
	closeServices := func() {
		cleanupOnce.Do(func() {
			nativeAgent.Close()
			processes.Close()
			plugins.Close()
			chatGPTAccount.resetBridgeServer()
			chatGPTAccount.mu.Lock()
			for _, login := range chatGPTAccount.logins {
				if login.Server != nil {
					login.Server.Close()
				}
			}
			chatGPTAccount.mu.Unlock()
			claudeAccount.mu.Lock()
			for _, login := range claudeAccount.logins {
				if login.Command != nil {
					_ = terminateManagedProcess(login.Command)
				}
			}
			claudeAccount.mu.Unlock()
			sessionRead.store.Close()
		})
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /local/status", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, state.snapshot())
	})
	mux.HandleFunc("GET /local/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"healthy": true, "mode": "native"})
	})
	registerUpdateRoutes(mux, state)
	mux.HandleFunc("GET /local/path", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"directory": state.projectPath()})
	})
	mux.HandleFunc("GET /local/agents", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, []map[string]any{{
			"id": "code", "name": "code", "displayName": "Code",
			"description": "TL Studio native coding agent", "mode": "primary", "hidden": false,
		}})
	})
	mux.HandleFunc("POST /local/project", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Path string `json:"path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, jsonError{Error: "invalid JSON body"})
			return
		}
		project, err := normalizeProject(body.Path)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, jsonError{Error: err.Error()})
			return
		}
		state.setProject(project)
		plugins.SwitchProject(project)
		writeJSON(w, http.StatusOK, state.snapshot())
	})
	mux.HandleFunc("POST /local/pick-directory", func(w http.ResponseWriter, _ *http.Request) {
		path, err := pickDirectory(state.projectPath())
		if err != nil {
			writeJSON(w, http.StatusNotImplemented, jsonError{Error: err.Error()})
			return
		}
		if path == "" {
			writeJSON(w, http.StatusOK, state.snapshot())
			return
		}
		project, err := normalizeProject(path)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, jsonError{Error: err.Error()})
			return
		}
		state.setProject(project)
		plugins.SwitchProject(project)
		writeJSON(w, http.StatusOK, state.snapshot())
	})

	registerLocalFileRoutes(mux, state)
	registerProjectSearchRoutes(mux, state)
	registerLocalProcessRoutesWithManager(mux, state, processes)
	registerProviderRoutes(mux, providerManager)
	registerProviderAccountRoutes(mux, providerAccounts)
	registerClaudeWebUIRelayRoutes(mux, claudeWebBridge)
	registerProviderDiscoveryRoutes(mux, providerManager)
	registerJevRouterRoutes(mux, jevRouter)
	registerDecisionEngineRoutes(mux, decisionEngines)
	registerPluginRoutes(mux, state, plugins)
	registerLayaRouterRoutes(mux, state, layaRouter)
	registerToolRegistryRoutesWithPlugins(mux, plugins, state.projectPath)
	registerSessionReadRoutes(mux, sessionRead)
	registerSessionCommandRoutes(mux, sessionCommands)
	registerQuestionRoutes(mux, questions)
	registerLiveEventRoutes(mux, liveEvents)
	registerPermissionRoutes(mux, permissionEngine)

	assets, err := fs.Sub(webFS, "web")
	if err != nil {
		return nil, closeServices, err
	}
	indexHTML, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		return nil, closeServices, fmt.Errorf("read embedded index.html: %w", err)
	}
	fileServer := http.FileServer(http.FS(assets))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusOK)
			if r.Method == http.MethodGet {
				_, _ = w.Write(indexHTML)
			}
			return
		}
		fileServer.ServeHTTP(w, r)
	})
	var handler http.Handler = mux
	if state.authSecret != "" {
		gate, err := newLocalAPIGate(state.authSecret)
		if err != nil {
			return nil, closeServices, fmt.Errorf("initialize local API gate: %w", err)
		}
		handler = gate.handler(handler)
	}
	return localOnly(securityHeaders(handler)), closeServices, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		if !isLoopbackHost(host) {
			http.Error(w, "localhost only", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || !isLoopbackHost(u.Hostname()) || !strings.EqualFold(u.Host, r.Host) {
				http.Error(w, "cross-origin request blocked", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; img-src 'self' data:; connect-src 'self'; font-src 'self' data:")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func normalizeProject(input string) (string, error) {
	if strings.TrimSpace(input) == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		input = cwd
	}
	abs, err := filepath.Abs(input)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("%s: %w", abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", abs)
	}
	return filepath.Clean(abs), nil
}

func freePort(host string) (int, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

func randomSecret(bytes int) (string, error) {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func openBrowser(address string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", address)
	case "darwin":
		cmd = exec.Command("open", address)
	default:
		cmd = exec.Command("xdg-open", address)
	}
	return cmd.Start()
}
