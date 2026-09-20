package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
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

	"embed"
)

var version = "dev"

//go:embed web/*
var webFS embed.FS

type appState struct {
	mu          sync.RWMutex
	project     string
	backendURL  string
	frontendURL string
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
	var kiloOverride string
	flag.StringVar(&projectArg, "project", "", "project directory to open")
	flag.BoolVar(&noBrowser, "no-browser", false, "do not open the browser automatically")
	flag.StringVar(&listenAddr, "listen", "127.0.0.1", "frontend listen address")
	flag.StringVar(&kiloOverride, "runtime-bin", "", "override path to the bundled agent runtime (advanced)")
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

	kiloPath, err := findKiloBinary(kiloOverride)
	if err != nil {
		log.Fatal(err)
	}

	backendPort, err := freePort("127.0.0.1")
	if err != nil {
		log.Fatalf("find backend port: %v", err)
	}
	frontendPort, err := freePort(listenAddr)
	if err != nil {
		log.Fatalf("find frontend port: %v", err)
	}

	username := "runtime"
	password, err := randomSecret(24)
	if err != nil {
		log.Fatalf("create server password: %v", err)
	}

	backendURL := fmt.Sprintf("http://127.0.0.1:%d", backendPort)
	frontendURL := "http://" + net.JoinHostPort(listenAddr, fmt.Sprint(frontendPort))
	state := &appState{
		project:     project,
		backendURL:  backendURL,
		frontendURL: frontendURL,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	kiloCmd, err := startKilo(ctx, kiloPath, backendPort, username, password)
	if err != nil {
		log.Fatalf("start bundled runtime: %v", err)
	}
	defer stopProcess(kiloCmd)

	if err := waitForPort(ctx, "127.0.0.1", backendPort, 12*time.Second); err != nil {
		stopProcess(kiloCmd)
		log.Fatalf("bundled runtime did not start: %v", err)
	}

	server, err := newServer(state, backendURL, username, password)
	if err != nil {
		stopProcess(kiloCmd)
		log.Fatalf("create local server: %v", err)
	}

	httpServer := &http.Server{
		Addr:              net.JoinHostPort(listenAddr, fmt.Sprint(frontendPort)),
		Handler:           server,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer shutdownCancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	fmt.Printf("TL Studio %s\n", version)
	fmt.Printf("  Project: %s\n", project)
	fmt.Printf("  Local:   %s\n", frontendURL)
	fmt.Printf("  Runtime: bundled\n")

	if !noBrowser {
		go func() {
			time.Sleep(250 * time.Millisecond)
			if err := openBrowser(frontendURL); err != nil {
				log.Printf("open browser: %v", err)
			}
		}()
	}

	err = httpServer.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("local server: %v", err)
	}
}

func newServer(state *appState, backendURL, username, password string) (http.Handler, error) {
	target, err := url.Parse(backendURL)
	if err != nil {
		return nil, err
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.URL.Path = strings.TrimPrefix(req.URL.Path, "/runtime")
		if req.URL.Path == "" {
			req.URL.Path = "/"
		}
		req.Host = target.Host
		req.SetBasicAuth(username, password)
		if project := state.projectPath(); project != "" {
			req.Header.Set("x-kilo-directory", strings.ReplaceAll(url.QueryEscape(project), "+", "%20"))
		}
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		writeJSON(w, http.StatusBadGateway, jsonError{Error: "Runtime backend unavailable: " + err.Error()})
	}

	providerManager, err := newRuntimeProviderManager(state, backendURL, username, password)
	if err != nil {
		return nil, fmt.Errorf("create runtime provider manager: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /local/status", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, state.snapshot())
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
		writeJSON(w, http.StatusOK, state.snapshot())
	})
	registerLocalFileRoutes(mux, state)
	registerProjectSearchRoutes(mux, state)
	registerLocalProcessRoutes(mux, state)
	registerRuntimeProviderRoutes(mux, providerManager)
	mux.Handle("/runtime/", proxy)
	mux.Handle("/runtime", proxy)

	assets, err := fs.Sub(webFS, "web")
	if err != nil {
		return nil, err
	}
	indexHTML, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		return nil, fmt.Errorf("read embedded index.html: %w", err)
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
	return localOnly(securityHeaders(mux)), nil
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

func findKiloBinary(override string) (string, error) {
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

func startKilo(ctx context.Context, kiloPath string, port int, username, password string) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, kiloPath, "serve", "--hostname", "127.0.0.1", "--port", fmt.Sprint(port))
	cmd.Env = append(os.Environ(),
		"KILO_SERVER_USERNAME="+username,
		"KILO_SERVER_PASSWORD="+password,
	)
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

func stopProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Signal(os.Interrupt)
	done := make(chan struct{})
	go func() {
		_, _ = cmd.Process.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
	}
}

func freePort(host string) (int, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

func waitForPort(ctx context.Context, host string, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	address := net.JoinHostPort(host, fmt.Sprint(port))
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", address, 250*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(120 * time.Millisecond):
		}
	}
	return fmt.Errorf("timed out waiting for %s", address)
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
