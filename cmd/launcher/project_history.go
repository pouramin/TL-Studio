package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

const maxProjectHistory = 50

type projectHistoryFile struct {
	Projects []string `json:"projects"`
}

type projectHistoryStore struct {
	mu       sync.Mutex
	loaded   bool
	filePath string
	projects []string
}

var recentProjects = &projectHistoryStore{}

func projectHistoryPath() string {
	if dir := strings.TrimSpace(os.Getenv("TL_STUDIO_STATE_DIR")); dir != "" {
		return filepath.Join(dir, "projects.json")
	}
	base, err := os.UserConfigDir()
	if err != nil || strings.TrimSpace(base) == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "TL Studio", "projects.json")
}

func (s *projectHistoryStore) path() string {
	if s.filePath != "" {
		return s.filePath
	}
	return projectHistoryPath()
}

func (s *projectHistoryStore) loadLocked() {
	if s.loaded {
		return
	}
	s.loaded = true
	data, err := os.ReadFile(s.path())
	if err != nil {
		return
	}
	var stored projectHistoryFile
	if json.Unmarshal(data, &stored) != nil {
		return
	}
	for _, project := range stored.Projects {
		project = strings.TrimSpace(project)
		if project == "" || containsProject(s.projects, project) {
			continue
		}
		s.projects = append(s.projects, filepath.Clean(project))
		if len(s.projects) >= maxProjectHistory {
			break
		}
	}
}

func (s *projectHistoryStore) remember(project string) {
	project = strings.TrimSpace(project)
	if project == "" {
		return
	}
	if absolute, err := filepath.Abs(project); err == nil {
		project = filepath.Clean(absolute)
	} else {
		project = filepath.Clean(project)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()

	next := make([]string, 0, len(s.projects)+1)
	next = append(next, project)
	for _, existing := range s.projects {
		if sameProjectPath(existing, project) {
			continue
		}
		next = append(next, existing)
		if len(next) >= maxProjectHistory {
			break
		}
	}
	s.projects = next
	s.persistLocked()
}

func (s *projectHistoryStore) list() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	result := make([]string, len(s.projects))
	copy(result, s.projects)
	return result
}

func (s *projectHistoryStore) persistLocked() {
	file := s.path()
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return
	}
	data, err := json.MarshalIndent(projectHistoryFile{Projects: s.projects}, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(file, data, 0o600)
}

func sameProjectPath(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func containsProject(items []string, target string) bool {
	for _, item := range items {
		if sameProjectPath(item, target) {
			return true
		}
	}
	return false
}

func registerProjectHistoryRoute(mux *http.ServeMux, state *appState) {
	mux.HandleFunc("GET /local/projects", func(w http.ResponseWriter, _ *http.Request) {
		recentProjects.remember(state.projectPath())
		writeJSON(w, http.StatusOK, projectHistoryFile{Projects: recentProjects.list()})
	})
}
