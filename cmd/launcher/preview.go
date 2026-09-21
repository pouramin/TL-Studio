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
	"strings"
	"sync"
	"time"
)

type previewCapability struct {
	Available bool                     `json:"available"`
	Kind      string                   `json:"kind,omitempty"`
	Command   string                   `json:"command,omitempty"`
	Entry     string                   `json:"entry,omitempty"`
	EntryMeta *previewEntryDescriptor  `json:"entryMeta,omitempty"`
	Entries   []previewEntryDescriptor `json:"entries,omitempty"`
	Reason    string                   `json:"reason,omitempty"`
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

func projectDevCommand(project string) string {
	packagePath := filepath.Join(project, "package.json")
	data, err := os.ReadFile(packagePath)
	if err != nil {
		return ""
	}
	var manifest struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal(data, &manifest) != nil || strings.TrimSpace(manifest.Scripts["dev"]) == "" {
		return ""
	}
	return "npm run dev"
}

func filePreviewCapability(entry previewEntryDescriptor, candidates []previewEntryDescriptor) previewCapability {
	entryCopy := entry
	result := previewCapability{
		Available: true,
		Kind:      "file",
		Entry:     entry.Path,
		EntryMeta: &entryCopy,
	}
	if len(candidates) > 1 {
		result.Entries = candidates
	}
	return result
}

func detectPreviewCapability(project, preferredEntry string) previewCapability {
	project = strings.TrimSpace(project)
	if project == "" {
		return previewCapability{Reason: "Open a project before starting Preview."}
	}

	candidates := previewFileCandidates(project)
	preferred, hasPreferred := validPreviewEntry(project, preferredEntry)
	devCommand := projectDevCommand(project)

	if hasPreferred {
		if preferred.Kind == "html" && devCommand != "" {
			return previewCapability{Available: true, Kind: "dev-server", Command: devCommand}
		}
		return filePreviewCapability(preferred, candidates)
	}

	if devCommand != "" {
		return previewCapability{Available: true, Kind: "dev-server", Command: devCommand}
	}

	if index, ok := defaultHTMLPreviewEntry(candidates); ok {
		return filePreviewCapability(index, candidates)
	}

	switch len(candidates) {
	case 0:
		return previewCapability{Reason: "No previewable file or package.json dev script was found in this project."}
	case 1:
		return filePreviewCapability(candidates[0], candidates)
	default:
		return previewCapability{
			Available: true,
			Kind:      "file",
			Entries:   candidates,
			Reason:    "Choose a file to preview.",
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

func previewEntryURL(baseURL string, entry previewEntryDescriptor) string {
	if entry.Renderer == "markdown" {
		return strings.TrimRight(baseURL, "/") + "/.tl-preview/markdown?file=" + url.QueryEscape(entry.Path)
	}
	if strings.EqualFold(filepath.ToSlash(entry.Path), "index.html") || strings.TrimSpace(entry.Path) == "" {
		return baseURL
	}
	parts := strings.Split(filepath.ToSlash(entry.Path), "/")
	for index, part := range parts {
		parts[index] = url.PathEscape(part)
	}
	return strings.TrimRight(baseURL, "/") + "/" + strings.Join(parts, "/")
}

func (m *previewManager) startStatic(project string, entry previewEntryDescriptor) error {
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

	project := m.state.projectPath()
	capability := detectPreviewCapability(project, preferredEntry)
	if !capability.Available {
		return previewSnapshot{previewCapability: capability, Project: project}, fmt.Errorf(capability.Reason)
	}

	if capability.Kind == "file" && (capability.Entry == "" || capability.EntryMeta == nil) {
		return previewSnapshot{previewCapability: capability, Project: project}, fmt.Errorf(capability.Reason)
	}

	if m.project != "" && sameProjectPath(m.project, project) && m.kind == "file" && capability.Kind == "file" && m.listener != nil && capability.EntryMeta != nil {
		m.entry = capability.Entry
		m.previewURL = previewEntryURL("http://"+m.listener.Addr().String()+"/", *capability.EntryMeta)
		return m.snapshotLocked(capability), nil
	}

	m.stopLocked()
	m.project = project
	m.kind = capability.Kind
	m.command = capability.Command
	m.entry = capability.Entry
	if capability.Kind == "file" && capability.EntryMeta != nil {
		if err := m.startStatic(project, *capability.EntryMeta); err != nil {
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
	if m.project == "" {
		capability := detectPreviewCapability(project, preferredEntry)
		return previewSnapshot{previewCapability: capability, Project: project}
	}

	if m.kind == "file" {
		capability := previewCapability{Available: true, Kind: "file", Entry: m.entry}
		if current, ok := validPreviewEntry(project, m.entry); ok {
			capability.EntryMeta = &current
		}
		capability.Entries = previewFileCandidates(project)
		return m.snapshotLocked(capability)
	}

	capability := previewCapability{Available: true, Kind: "dev-server", Command: m.command}
	return m.snapshotLocked(capability)
}

func safeStaticPreviewHandler(project string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "preview is read-only", http.StatusMethodNotAllowed)
			return
		}
		if serveMarkdownPreview(w, r, project) {
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

	mux.HandleFunc("GET /local/preview/capabilities", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, previewRegistry())
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
