package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	pathpkg "path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type previewCapability struct {
	Available bool     `json:"available"`
	Kind      string   `json:"kind,omitempty"`
	Command   string   `json:"command,omitempty"`
	Entry     string   `json:"entry,omitempty"`
	Entries   []string `json:"entries,omitempty"`
	Reason    string   `json:"reason,omitempty"`
}

type previewSnapshot struct {
	previewCapability
	Project string `json:"project,omitempty"`
	Running bool   `json:"running"`
	URL     string `json:"url,omitempty"`
	Output  string `json:"output,omitempty"`
	Error   string `json:"error,omitempty"`
}

type previewManager struct {
	mu         sync.Mutex
	state      *appState
	processes  *processManager
	project    string
	kind       string
	command    string
	entry      string
	processID  string
	previewURL string
	server     *http.Server
	listener   net.Listener
}

var previewURLPattern = regexp.MustCompile(`https?://(?:localhost|127\.0\.0\.1|0\.0\.0\.0|\[::1\]):[0-9]+`)

func newPreviewManager(state *appState) *previewManager {
	return &previewManager{state: state, processes: newProcessManager(state.projectPath)}
}

func isHTMLPreviewEntry(value string) bool {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(value))) {
	case ".html", ".htm":
		return true
	default:
		return false
	}
}

func validStaticPreviewEntry(project, requested string) (string, bool) {
	if !isHTMLPreviewEntry(requested) {
		return "", false
	}
	target, rel, err := resolveProjectEntry(project, requested)
	if err != nil {
		return "", false
	}
	info, err := os.Stat(target)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

func previewHTMLCandidates(project string) []string {
	root, err := canonicalProjectRoot(project)
	if err != nil {
		return nil
	}
	candidates := make([]string, 0, 8)
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if path == root {
			return nil
		}
		if entry.IsDir() {
			name := strings.ToLower(entry.Name())
			if name == ".git" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || !isHTMLPreviewEntry(entry.Name()) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		candidates = append(candidates, filepath.ToSlash(rel))
		if len(candidates) >= 64 {
			return fs.SkipAll
		}
		return nil
	})
	sort.Slice(candidates, func(i, j int) bool {
		return strings.ToLower(candidates[i]) < strings.ToLower(candidates[j])
	})
	return candidates
}

func detectPreviewCapability(project, preferredEntry string) previewCapability {
	project = strings.TrimSpace(project)
	if project == "" {
		return previewCapability{Reason: "Open a project before starting Preview."}
	}

	packagePath := filepath.Join(project, "package.json")
	if data, err := os.ReadFile(packagePath); err == nil {
		var manifest struct {
			Scripts map[string]string `json:"scripts"`
		}
		if json.Unmarshal(data, &manifest) == nil {
			if strings.TrimSpace(manifest.Scripts["dev"]) != "" {
				return previewCapability{Available: true, Kind: "dev-server", Command: "npm run dev"}
			}
		}
	}

	indexPath := filepath.Join(project, "index.html")
	if info, err := os.Stat(indexPath); err == nil && info.Mode().IsRegular() {
		return previewCapability{Available: true, Kind: "static", Entry: "index.html"}
	}

	if entry, ok := validStaticPreviewEntry(project, preferredEntry); ok {
		return previewCapability{Available: true, Kind: "static", Entry: entry}
	}

	candidates := previewHTMLCandidates(project)
	switch len(candidates) {
	case 0:
		return previewCapability{Reason: "Preview supports projects with a package.json dev script or an HTML file."}
	case 1:
		return previewCapability{Available: true, Kind: "static", Entry: candidates[0]}
	default:
		return previewCapability{
			Available: true,
			Kind:      "static",
			Entries:   candidates,
			Reason:    "Choose an HTML file to preview.",
		}
	}
}

func normalizePreviewURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := parsed.Hostname()
	if host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	if !isLoopbackHost(host) {
		return ""
	}
	port := parsed.Port()
	if port == "" {
		return ""
	}
	return "http://" + net.JoinHostPort(host, port) + "/"
}

func detectPreviewURL(output string) string {
	matches := previewURLPattern.FindAllString(output, -1)
	for _, match := range matches {
		if normalized := normalizePreviewURL(match); normalized != "" {
			return normalized
		}
	}
	return ""
}

func (m *previewManager) stopLocked() {
	if m.processID != "" {
		_ = m.processes.stop(m.processID)
	}
	if m.server != nil {
		_ = m.server.Close()
	}
	if m.listener != nil {
		_ = m.listener.Close()
	}
	m.project = ""
	m.kind = ""
	m.command = ""
	m.entry = ""
	m.processID = ""
	m.previewURL = ""
	m.server = nil
	m.listener = nil
}

func (m *previewManager) stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked()
}

func (m *previewManager) syncProjectLocked() {
	current := m.state.projectPath()
	if m.project != "" && !sameProjectPath(m.project, current) {
		m.stopLocked()
	}
}

func previewEntryURL(baseURL, entry string) string {
	if strings.EqualFold(filepath.ToSlash(entry), "index.html") || strings.TrimSpace(entry) == "" {
		return baseURL
	}
	parts := strings.Split(filepath.ToSlash(entry), "/")
	for index, part := range parts {
		parts[index] = url.PathEscape(part)
	}
	return strings.TrimRight(baseURL, "/") + "/" + strings.Join(parts, "/")
}

func (m *previewManager) startStatic(project, entry string) error {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	server := &http.Server{
		Handler:           safeStaticPreviewHandler(project),
		ReadHeaderTimeout: 10 * time.Second,
	}
	m.listener = listener
	m.server = server
	m.previewURL = previewEntryURL("http://"+listener.Addr().String()+"/", entry)
	go func() {
		_ = server.Serve(listener)
	}()
	return nil
}

func (m *previewManager) start(preferredEntry string) (previewSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.syncProjectLocked()
	m.stopLocked()

	project := m.state.projectPath()
	capability := detectPreviewCapability(project, preferredEntry)
	if !capability.Available {
		return previewSnapshot{previewCapability: capability, Project: project}, fmt.Errorf(capability.Reason)
	}

	if capability.Kind == "static" && capability.Entry == "" {
		return previewSnapshot{previewCapability: capability, Project: project}, fmt.Errorf(capability.Reason)
	}

	m.project = project
	m.kind = capability.Kind
	m.command = capability.Command
	m.entry = capability.Entry
	if capability.Kind == "static" {
		if err := m.startStatic(project, capability.Entry); err != nil {
			m.stopLocked()
			return previewSnapshot{}, err
		}
		return m.snapshotLocked(capability), nil
	}

	process, err := m.processes.start(capability.Command)
	if err != nil {
		m.stopLocked()
		return previewSnapshot{}, err
	}
	m.processID = process.id
	return m.snapshotLocked(capability), nil
}

func (m *previewManager) snapshotLocked(capability previewCapability) previewSnapshot {
	snapshot := previewSnapshot{
		previewCapability: capability,
		Project:           m.state.projectPath(),
		Running:           m.project != "",
		URL:               m.previewURL,
	}
	if m.kind != "dev-server" || m.processID == "" {
		return snapshot
	}
	process, ok := m.processes.get(m.processID)
	if !ok {
		snapshot.Running = false
		snapshot.Error = "Preview process is no longer available."
		return snapshot
	}
	processState := process.snapshot()
	snapshot.Output = processState.Output
	if m.previewURL == "" {
		m.previewURL = detectPreviewURL(processState.Output)
		snapshot.URL = m.previewURL
	}
	snapshot.Running = processState.Running
	if !processState.Running && m.previewURL == "" {
		snapshot.Error = "The dev server exited before TL Studio could detect a local preview URL."
	}
	return snapshot
}

func (m *previewManager) snapshot(preferredEntry string) previewSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.syncProjectLocked()
	project := m.state.projectPath()
	if m.project != "" && m.kind == "static" && m.entry != "" {
		preferredEntry = m.entry
	}
	capability := detectPreviewCapability(project, preferredEntry)
	if m.project == "" {
		return previewSnapshot{previewCapability: capability, Project: project}
	}
	return m.snapshotLocked(capability)
}

func safeStaticPreviewHandler(project string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "preview is read-only", http.StatusMethodNotAllowed)
			return
		}
		rel := strings.TrimPrefix(pathpkg.Clean("/"+r.URL.Path), "/")
		if rel == "" || rel == "." {
			rel = "index.html"
		}
		target, _, err := resolveProjectEntry(project, rel)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		info, err := os.Stat(target)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if info.IsDir() {
			target, _, err = resolveProjectEntry(project, filepath.ToSlash(filepath.Join(rel, "index.html")))
			if err != nil {
				http.NotFound(w, r)
				return
			}
			if info, err = os.Stat(target); err != nil || !info.Mode().IsRegular() {
				http.NotFound(w, r)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeFile(w, r, target)
	})
}

func registerLivePreviewRoutes(mux *http.ServeMux, state *appState) {
	manager := newPreviewManager(state)

	// The application shell keeps a strict CSP, but explicitly allows frames from
	// loopback-only preview servers. The preview itself never shares TL Studio's origin.
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		indexHTML, err := fs.ReadFile(webFS, "web/index.html")
		if err != nil {
			http.Error(w, "TL Studio UI unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; img-src 'self' data:; connect-src 'self'; font-src 'self' data:; frame-src http://127.0.0.1:* http://localhost:*")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(indexHTML)
	})

	mux.HandleFunc("GET /local/preview", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, manager.snapshot(r.URL.Query().Get("entry")))
	})
	mux.HandleFunc("POST /local/preview", func(w http.ResponseWriter, r *http.Request) {
		snapshot, err := manager.start(r.URL.Query().Get("entry"))
		if err != nil {
			if snapshot.Project == "" {
				snapshot.Project = state.projectPath()
			}
			snapshot.Error = err.Error()
			writeJSON(w, http.StatusBadRequest, snapshot)
			return
		}
		writeJSON(w, http.StatusCreated, snapshot)
	})
	mux.HandleFunc("DELETE /local/preview", func(w http.ResponseWriter, _ *http.Request) {
		manager.stop()
		w.WriteHeader(http.StatusNoContent)
	})
}
