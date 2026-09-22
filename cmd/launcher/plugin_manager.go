package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	pluginStoreVersion = 1
	pluginTypeMCP      = "mcp"
	pluginTransportStdio = "stdio"
)

type pluginEnvironmentRef struct {
	Name       string `json:"name"`
	Configured bool   `json:"configured,omitempty"`
}

type pluginConfig struct {
	ID               string                 `json:"id"`
	Name             string                 `json:"name"`
	Description      string                 `json:"description,omitempty"`
	Type             string                 `json:"type"`
	Enabled          bool                   `json:"enabled"`
	Scope            string                 `json:"scope"`
	Project          string                 `json:"project,omitempty"`
	Transport        string                 `json:"transport"`
	Command          string                 `json:"command"`
	Arguments        []string               `json:"arguments,omitempty"`
	WorkingDirectory string                 `json:"workingDirectory,omitempty"`
	Environment      []pluginEnvironmentRef `json:"environment,omitempty"`
	Metadata         map[string]string      `json:"metadata,omitempty"`
}

type pluginStoreFile struct {
	Version int            `json:"version"`
	Plugins []pluginConfig `json:"plugins"`
}

type pluginStore struct {
	mu       sync.Mutex
	filePath string
	loaded   bool
	plugins  []pluginConfig
}

type pluginGraphStatus struct {
	Available    bool   `json:"available"`
	ModifiedAt   string `json:"modifiedAt,omitempty"`
	GraphPath    string `json:"graphPath,omitempty"`
	HTMLPath     string `json:"htmlPath,omitempty"`
	ReportPath   string `json:"reportPath,omitempty"`
	CLIAvailable bool   `json:"cliAvailable,omitempty"`
	MCPAvailable bool   `json:"mcpAvailable,omitempty"`
}

type pluginView struct {
	pluginConfig
	Status          string            `json:"status"`
	Error           string            `json:"error,omitempty"`
	DiscoveredTools int               `json:"discoveredTools"`
	Resources       int               `json:"resources"`
	Tools           []string          `json:"tools,omitempty"`
	Graph           *pluginGraphStatus `json:"graph,omitempty"`
}

type pluginUpsertRequest struct {
	Plugin      pluginConfig       `json:"plugin"`
	Environment *map[string]string `json:"environment,omitempty"`
}

type pluginManager struct {
	state       *appState
	store       *pluginStore
	credentials providerCredentialStore
	processes   *processManager
	permissions nativeToolAuthorizer

	mu      sync.Mutex
	clients map[string]*mcpClient
	errors  map[string]string
}

func pluginStorePath() string {
	return filepath.Join(tlStudioStateDirectory(), "plugins.json")
}

func newPluginStore(path string) *pluginStore {
	return &pluginStore{filePath: path}
}

func normalizePluginProject(project string) string {
	project = filepath.Clean(strings.TrimSpace(project))
	if runtime.GOOS == "windows" {
		project = strings.ToLower(project)
	}
	return project
}

func normalizePluginID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-'
		if !ok {
			if b.Len() > 0 && !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
			continue
		}
		if r == '-' {
			if b.Len() == 0 || lastDash {
				continue
			}
			lastDash = true
		} else {
			lastDash = false
		}
		b.WriteRune(r)
	}
	return strings.Trim(b.String(), "-_")
}

func normalizePluginEnvName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	for index, r := range name {
		if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r == '_' || index > 0 && r >= '0' && r <= '9' {
			continue
		}
		return ""
	}
	return name
}

func pluginCredentialID(config pluginConfig, envName string) string {
	value := strings.Join([]string{config.Scope, normalizePluginProject(config.Project), config.ID, envName}, "\x00")
	sum := sha256.Sum256([]byte(value))
	return "plugin_" + hex.EncodeToString(sum[:12])
}

func normalizePluginConfig(input pluginConfig, currentProject string) (pluginConfig, error) {
	input.ID = normalizePluginID(input.ID)
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	input.Type = strings.ToLower(strings.TrimSpace(input.Type))
	input.Scope = strings.ToLower(strings.TrimSpace(input.Scope))
	input.Transport = strings.ToLower(strings.TrimSpace(input.Transport))
	input.Command = strings.TrimSpace(input.Command)
	input.WorkingDirectory = strings.TrimSpace(filepath.ToSlash(input.WorkingDirectory))

	if input.Name == "" {
		return pluginConfig{}, errors.New("plugin name is required")
	}
	if input.ID == "" {
		input.ID = normalizePluginID(input.Name)
	}
	if input.ID == "" || !validProviderID(input.ID) {
		return pluginConfig{}, errors.New("plugin ID must use lowercase letters, numbers, dashes, or underscores")
	}
	if input.Type == "" {
		input.Type = pluginTypeMCP
	}
	if input.Type != pluginTypeMCP {
		return pluginConfig{}, errors.New("only MCP plugins are supported in this version")
	}
	if input.Transport == "" {
		input.Transport = pluginTransportStdio
	}
	if input.Transport != pluginTransportStdio {
		return pluginConfig{}, errors.New("only stdio MCP transport is supported in this version")
	}
	if input.Scope == "" {
		input.Scope = "project"
	}
	switch input.Scope {
	case "project":
		project := strings.TrimSpace(input.Project)
		if project == "" {
			project = currentProject
		}
		if project == "" {
			return pluginConfig{}, errors.New("open a project before saving a project-scoped plugin")
		}
		abs, err := filepath.Abs(project)
		if err != nil {
			return pluginConfig{}, err
		}
		input.Project = normalizePluginProject(abs)
	case "global":
		input.Project = ""
	default:
		return pluginConfig{}, errors.New("plugin scope must be project or global")
	}
	if input.Command == "" {
		return pluginConfig{}, errors.New("plugin command is required")
	}
	args := make([]string, 0, len(input.Arguments))
	for _, arg := range input.Arguments {
		args = append(args, strings.TrimSpace(arg))
	}
	input.Arguments = args
	seenEnv := map[string]bool{}
	env := make([]pluginEnvironmentRef, 0, len(input.Environment))
	for _, item := range input.Environment {
		name := normalizePluginEnvName(item.Name)
		if name == "" || seenEnv[name] {
			continue
		}
		seenEnv[name] = true
		env = append(env, pluginEnvironmentRef{Name: name, Configured: item.Configured})
	}
	sort.Slice(env, func(i, j int) bool { return env[i].Name < env[j].Name })
	input.Environment = env
	if input.Metadata == nil {
		input.Metadata = map[string]string{}
	}
	return input, nil
}

func (s *pluginStore) loadLocked() error {
	if s.loaded {
		return nil
	}
	s.loaded = true
	s.plugins = nil
	data, err := os.ReadFile(s.filePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var stored pluginStoreFile
	if err := json.Unmarshal(data, &stored); err != nil {
		return fmt.Errorf("decode plugin store: %w", err)
	}
	if stored.Version != 0 && stored.Version != pluginStoreVersion {
		return fmt.Errorf("unsupported plugin store version %d", stored.Version)
	}
	for _, plugin := range stored.Plugins {
		normalized, err := normalizePluginConfig(plugin, plugin.Project)
		if err == nil {
			s.plugins = append(s.plugins, normalized)
		}
	}
	return nil
}

func (s *pluginStore) persistLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.filePath), 0o700); err != nil {
		return err
	}
	plugins := append([]pluginConfig(nil), s.plugins...)
	sort.Slice(plugins, func(i, j int) bool {
		a := plugins[i].Scope + "\x00" + plugins[i].Project + "\x00" + plugins[i].ID
		b := plugins[j].Scope + "\x00" + plugins[j].Project + "\x00" + plugins[j].ID
		return a < b
	})
	data, err := json.MarshalIndent(pluginStoreFile{Version: pluginStoreVersion, Plugins: plugins}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.filePath), 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(s.filePath), "plugins-*.tmp")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, s.filePath); err != nil {
		return os.WriteFile(s.filePath, data, 0o600)
	}
	return nil
}

func pluginMatchesProject(config pluginConfig, project string) bool {
	if config.Scope == "global" {
		return true
	}
	return normalizePluginProject(config.Project) == normalizePluginProject(project)
}

func pluginKey(config pluginConfig) string {
	return config.Scope + "\x00" + normalizePluginProject(config.Project) + "\x00" + config.ID
}

func (s *pluginStore) list(project string) ([]pluginConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return nil, err
	}
	result := []pluginConfig{}
	for _, plugin := range s.plugins {
		if pluginMatchesProject(plugin, project) {
			result = append(result, plugin)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name)
	})
	return result, nil
}

func (s *pluginStore) find(project, id string) (pluginConfig, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return pluginConfig{}, false, err
	}
	id = normalizePluginID(id)
	var global *pluginConfig
	for _, plugin := range s.plugins {
		if plugin.ID != id {
			continue
		}
		if plugin.Scope == "project" && pluginMatchesProject(plugin, project) {
			return plugin, true, nil
		}
		if plugin.Scope == "global" {
			copy := plugin
			global = &copy
		}
	}
	if global != nil {
		return *global, true, nil
	}
	return pluginConfig{}, false, nil
}

func (s *pluginStore) upsert(config pluginConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return err
	}
	key := pluginKey(config)
	for index, existing := range s.plugins {
		if pluginKey(existing) == key {
			s.plugins[index] = config
			return s.persistLocked()
		}
	}
	s.plugins = append(s.plugins, config)
	return s.persistLocked()
}

func (s *pluginStore) remove(project, id string) (pluginConfig, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return pluginConfig{}, false, err
	}
	id = normalizePluginID(id)
	index := -1
	for i, plugin := range s.plugins {
		if plugin.ID == id && plugin.Scope == "project" && pluginMatchesProject(plugin, project) {
			index = i
			break
		}
	}
	if index < 0 {
		for i, plugin := range s.plugins {
			if plugin.ID == id && plugin.Scope == "global" {
				index = i
				break
			}
		}
	}
	if index < 0 {
		return pluginConfig{}, false, nil
	}
	removed := s.plugins[index]
	s.plugins = append(s.plugins[:index], s.plugins[index+1:]...)
	if err := s.persistLocked(); err != nil {
		return pluginConfig{}, false, err
	}
	return removed, true, nil
}

func newPluginManager(state *appState, processes *processManager, permissions nativeToolAuthorizer) *pluginManager {
	if processes == nil {
		processes = newProcessManager(state.projectPath)
	}
	manager := &pluginManager{
		state: state, store: newPluginStore(pluginStorePath()), credentials: newProviderCredentialStore(),
		processes: processes, permissions: permissions, clients: map[string]*mcpClient{}, errors: map[string]string{},
	}
	if state != nil && state.ctx != nil {
		go func() {
			<-state.ctx.Done()
			manager.Close()
		}()
	}
	return manager
}

func (m *pluginManager) configEnvironment(config pluginConfig) (map[string]string, error) {
	env := map[string]string{}
	for _, item := range config.Environment {
		value, err := m.credentials.Get(pluginCredentialID(config, item.Name))
		if errors.Is(err, errCredentialNotFound) {
			return nil, fmt.Errorf("environment variable %s is not configured", item.Name)
		}
		if err != nil {
			return nil, err
		}
		env[item.Name] = value
	}
	return env, nil
}

func pluginWorkingDirectory(config pluginConfig, project string) (string, error) {
	if config.Scope == "project" {
		project = config.Project
	}
	if strings.TrimSpace(project) == "" {
		return "", errors.New("plugin requires an active project")
	}
	if config.WorkingDirectory == "" || config.WorkingDirectory == "." {
		return project, nil
	}
	target, _, err := resolveProjectEntry(project, config.WorkingDirectory)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("plugin working directory is not a directory")
	}
	return target, nil
}

func isGraphifyPlugin(config pluginConfig) bool {
	name := strings.ToLower(filepath.Base(strings.TrimSpace(config.Command)))
	return name == "graphify-mcp" || name == "graphify-mcp.exe" || strings.EqualFold(config.Metadata["integration"], "graphify")
}

func graphifyStatus(config pluginConfig, project string) *pluginGraphStatus {
	if !isGraphifyPlugin(config) {
		return nil
	}
	if config.Scope == "project" {
		project = config.Project
	}
	status := &pluginGraphStatus{}
	_, status.CLIAvailable = lookPathPluginExecutable("graphify")
	_, status.MCPAvailable = lookPathPluginExecutable(config.Command)
	graphRel := "graphify-out/graph.json"
	if len(config.Arguments) > 0 && strings.TrimSpace(config.Arguments[0]) != "" {
		graphRel = filepath.ToSlash(config.Arguments[0])
	}
	target, rel, err := resolveProjectEntry(project, graphRel)
	if err == nil {
		if info, statErr := os.Stat(target); statErr == nil && info.Mode().IsRegular() {
			status.Available = true
			status.GraphPath = filepath.ToSlash(rel)
			status.ModifiedAt = info.ModTime().UTC().Format(time.RFC3339)
		}
	}
	for _, candidate := range []struct{ rel string; field *string }{
		{"graphify-out/graph.html", &status.HTMLPath},
		{"graphify-out/GRAPH_REPORT.md", &status.ReportPath},
	} {
		if target, rel, err := resolveProjectEntry(project, candidate.rel); err == nil {
			if info, statErr := os.Stat(target); statErr == nil && info.Mode().IsRegular() {
				*candidate.field = filepath.ToSlash(rel)
			}
		}
	}
	return status
}

func lookPathPluginExecutable(command string) (string, bool) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", false
	}
	if strings.ContainsAny(command, `/\\`) {
		info, err := os.Stat(command)
		return command, err == nil && !info.IsDir()
	}
	path, err := exec.LookPath(command)
	return path, err == nil
}

func (m *pluginManager) clientKey(config pluginConfig) string {
	return pluginKey(config)
}

func (m *pluginManager) stopLocked(config pluginConfig) {
	key := m.clientKey(config)
	if client := m.clients[key]; client != nil {
		client.Close()
		delete(m.clients, key)
	}
}

func (m *pluginManager) ensureClientLocked(ctx context.Context, config pluginConfig, project string) (*mcpClient, error) {
	key := m.clientKey(config)
	if existing := m.clients[key]; existing != nil && existing.Healthy() {
		return existing, nil
	}
	if existing := m.clients[key]; existing != nil {
		existing.Close()
		delete(m.clients, key)
	}
	if !config.Enabled {
		return nil, errors.New("plugin is disabled")
	}
	if graph := graphifyStatus(config, project); graph != nil && !graph.Available {
		return nil, errors.New("Graphify graph is missing; build the graph before enabling MCP queries")
	}
	env, err := m.configEnvironment(config)
	if err != nil {
		return nil, err
	}
	cwd, err := pluginWorkingDirectory(config, project)
	if err != nil {
		return nil, err
	}
	client := newMCPClient(config, cwd, env)
	if err := client.Start(ctx); err != nil {
		client.Close()
		m.errors[key] = err.Error()
		return nil, err
	}
	delete(m.errors, key)
	m.clients[key] = client
	return client, nil
}

func (m *pluginManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, client := range m.clients {
		client.Close()
		delete(m.clients, key)
	}
}

func (m *pluginManager) SwitchProject(project string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, client := range m.clients {
		config := client.Config()
		if config.Scope == "project" && !pluginMatchesProject(config, project) {
			client.Close()
			delete(m.clients, key)
		}
	}
}

func (m *pluginManager) saveEnvironment(config pluginConfig, values *map[string]string, previous pluginConfig) (pluginConfig, error) {
	if values == nil {
		config.Environment = previous.Environment
		return config, nil
	}
	previousNames := map[string]pluginEnvironmentRef{}
	for _, item := range previous.Environment {
		previousNames[item.Name] = item
	}
	next := make([]pluginEnvironmentRef, 0, len(*values))
	keep := map[string]bool{}
	for rawName, value := range *values {
		name := normalizePluginEnvName(rawName)
		if name == "" {
			return pluginConfig{}, fmt.Errorf("invalid environment variable name %q", rawName)
		}
		value = strings.TrimSpace(value)
		credentialID := pluginCredentialID(config, name)
		if value == "" {
			if _, existed := previousNames[name]; !existed {
				continue
			}
		} else if err := m.credentials.Put(credentialID, value); err != nil {
			return pluginConfig{}, err
		}
		keep[name] = true
		next = append(next, pluginEnvironmentRef{Name: name, Configured: true})
	}
	for _, item := range previous.Environment {
		if keep[item.Name] {
			continue
		}
		_ = m.credentials.Delete(pluginCredentialID(previous, item.Name))
	}
	sort.Slice(next, func(i, j int) bool { return next[i].Name < next[j].Name })
	config.Environment = next
	return config, nil
}

func (m *pluginManager) Upsert(project string, request pluginUpsertRequest) (pluginView, error) {
	config, err := normalizePluginConfig(request.Plugin, project)
	if err != nil {
		return pluginView{}, err
	}
	previous, found, err := m.store.find(project, config.ID)
	if err != nil {
		return pluginView{}, err
	}
	if found && pluginKey(previous) != pluginKey(config) {
		return pluginView{}, errors.New("changing plugin scope or project requires removing and re-adding the plugin")
	}
	config, err = m.saveEnvironment(config, request.Environment, previous)
	if err != nil {
		return pluginView{}, err
	}
	if err := m.store.upsert(config); err != nil {
		return pluginView{}, err
	}
	m.mu.Lock()
	m.stopLocked(config)
	m.mu.Unlock()
	return m.View(project, config.ID, true)
}

func (m *pluginManager) Remove(project, id string) error {
	config, found, err := m.store.remove(project, id)
	if err != nil {
		return err
	}
	if !found {
		return os.ErrNotExist
	}
	m.mu.Lock()
	m.stopLocked(config)
	delete(m.errors, m.clientKey(config))
	m.mu.Unlock()
	for _, item := range config.Environment {
		_ = m.credentials.Delete(pluginCredentialID(config, item.Name))
	}
	return nil
}

func (m *pluginManager) SetEnabled(project, id string, enabled bool) (pluginView, error) {
	config, found, err := m.store.find(project, id)
	if err != nil {
		return pluginView{}, err
	}
	if !found {
		return pluginView{}, os.ErrNotExist
	}
	config.Enabled = enabled
	if err := m.store.upsert(config); err != nil {
		return pluginView{}, err
	}
	m.mu.Lock()
	m.stopLocked(config)
	m.mu.Unlock()
	return m.View(project, id, true)
}

func (m *pluginManager) TestConfig(ctx context.Context, project string, request pluginUpsertRequest) (pluginView, error) {
	config, err := normalizePluginConfig(request.Plugin, project)
	if err != nil {
		return pluginView{}, err
	}
	config.Enabled = true
	env := map[string]string{}
	if request.Environment != nil {
		for rawName, value := range *request.Environment {
			name := normalizePluginEnvName(rawName)
			if name == "" {
				return pluginView{}, fmt.Errorf("invalid environment variable name %q", rawName)
			}
			if strings.TrimSpace(value) != "" {
				env[name] = value
				continue
			}
			if stored, getErr := m.credentials.Get(pluginCredentialID(config, name)); getErr == nil {
				env[name] = stored
			}
		}
	}
	if graph := graphifyStatus(config, project); graph != nil && !graph.Available {
		return pluginView{pluginConfig: config, Status: "Graph Missing", Graph: graph}, errors.New("Graphify graph is missing")
	}
	cwd, err := pluginWorkingDirectory(config, project)
	if err != nil {
		return pluginView{}, err
	}
	client := newMCPClient(config, cwd, env)
	if err := client.Start(ctx); err != nil {
		client.Close()
		return pluginView{pluginConfig: config, Status: "Error", Error: err.Error(), Graph: graphifyStatus(config, project)}, err
	}
	defer client.Close()
	return pluginView{
		pluginConfig: config,
		Status: "Connected", DiscoveredTools: len(client.Tools()), Resources: len(client.Resources()),
		Tools: client.ToolIDs(), Graph: graphifyStatus(config, project),
	}, nil
}

func (m *pluginManager) TestSaved(ctx context.Context, project, id string) (pluginView, error) {
	config, found, err := m.store.find(project, id)
	if err != nil {
		return pluginView{}, err
	}
	if !found {
		return pluginView{}, os.ErrNotExist
	}
	config.Enabled = true
	env, err := m.configEnvironment(config)
	if err != nil {
		return pluginView{}, err
	}
	cwd, err := pluginWorkingDirectory(config, project)
	if err != nil {
		return pluginView{}, err
	}
	client := newMCPClient(config, cwd, env)
	if err := client.Start(ctx); err != nil {
		client.Close()
		return pluginView{pluginConfig: config, Status: "Error", Error: err.Error(), Graph: graphifyStatus(config, project)}, err
	}
	defer client.Close()
	return pluginView{
		pluginConfig: config, Status: "Connected", DiscoveredTools: len(client.Tools()), Resources: len(client.Resources()),
		Tools: client.ToolIDs(), Graph: graphifyStatus(config, project),
	}, nil
}

func (m *pluginManager) viewConfig(project string, config pluginConfig, start bool) pluginView {
	view := pluginView{pluginConfig: config, Status: "Disabled", Graph: graphifyStatus(config, project)}
	if !config.Enabled {
		return view
	}
	if view.Graph != nil && !view.Graph.Available {
		view.Status = "Graph Missing"
		return view
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := m.clientKey(config)
	client := m.clients[key]
	if start && (client == nil || !client.Healthy()) {
		var err error
		client, err = m.ensureClientLocked(context.Background(), config, project)
		if err != nil {
			view.Status = "Error"
			view.Error = err.Error()
			return view
		}
	}
	if client != nil && client.Healthy() {
		view.Status = "Connected"
		view.DiscoveredTools = len(client.Tools())
		view.Resources = len(client.Resources())
		view.Tools = client.ToolIDs()
		return view
	}
	view.Status = "Starting"
	if errText := m.errors[key]; errText != "" {
		view.Status = "Error"
		view.Error = errText
	}
	return view
}

func (m *pluginManager) List(project string, start bool) ([]pluginView, error) {
	configs, err := m.store.list(project)
	if err != nil {
		return nil, err
	}
	views := make([]pluginView, 0, len(configs))
	for _, config := range configs {
		views = append(views, m.viewConfig(project, config, start))
	}
	return views, nil
}

func (m *pluginManager) View(project, id string, start bool) (pluginView, error) {
	config, found, err := m.store.find(project, id)
	if err != nil {
		return pluginView{}, err
	}
	if !found {
		return pluginView{}, os.ErrNotExist
	}
	return m.viewConfig(project, config, start), nil
}

func (m *pluginManager) ToolDescriptors(project string) []toolDescriptor {
	configs, err := m.store.list(project)
	if err != nil {
		return nil
	}
	var descriptors []toolDescriptor
	for _, config := range configs {
		if !config.Enabled {
			continue
		}
		m.mu.Lock()
		client, err := m.ensureClientLocked(context.Background(), config, project)
		if err == nil {
			descriptors = append(descriptors, client.ToolDescriptors()...)
		}
		m.mu.Unlock()
	}
	sort.Slice(descriptors, func(i, j int) bool { return descriptors[i].ID < descriptors[j].ID })
	return descriptors
}

func (m *pluginManager) ToolDefinitions(project string) []nativeModelToolDefinition {
	configs, err := m.store.list(project)
	if err != nil {
		return nil
	}
	var definitions []nativeModelToolDefinition
	for _, config := range configs {
		if !config.Enabled {
			continue
		}
		m.mu.Lock()
		client, err := m.ensureClientLocked(context.Background(), config, project)
		if err == nil {
			definitions = append(definitions, client.ToolDefinitions()...)
		}
		m.mu.Unlock()
	}
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].ID < definitions[j].ID })
	return definitions
}

func (m *pluginManager) Descriptor(project, id string) (toolDescriptor, bool) {
	for _, descriptor := range m.ToolDescriptors(project) {
		if descriptor.ID == id {
			return descriptor, true
		}
	}
	return toolDescriptor{}, false
}

func (m *pluginManager) Execute(ctx context.Context, sessionID, project string, call nativeToolCall) (nativeToolResult, bool) {
	if !strings.HasPrefix(strings.TrimSpace(call.ID), "mcp.") {
		return nativeToolResult{}, false
	}
	configs, err := m.store.list(project)
	if err != nil {
		return nativeToolResult{ToolID: call.ID, CallID: call.CallID, Error: err.Error()}, true
	}
	for _, config := range configs {
		if !config.Enabled {
			continue
		}
		m.mu.Lock()
		client, startErr := m.ensureClientLocked(ctx, config, project)
		if startErr != nil {
			m.mu.Unlock()
			continue
		}
		descriptor, originalName, ok := client.ResolveTool(call.ID)
		m.mu.Unlock()
		if !ok {
			continue
		}
		input, decodeErr := decodeNativeToolArguments(call.Arguments)
		if decodeErr != nil {
			return nativeToolResult{ToolID: call.ID, CallID: call.CallID, Error: decodeErr.Error()}, true
		}
		if m.permissions != nil {
			if permissionErr := m.permissions.AuthorizeNativeTool(ctx, sessionID, project, descriptor, input); permissionErr != nil {
				return nativeToolResult{ToolID: call.ID, CallID: call.CallID, Error: permissionErr.Error()}, true
			}
		}
		start := time.Now()
		output, callErr := client.CallTool(ctx, originalName, input)
		result := nativeToolResult{ToolID: call.ID, CallID: call.CallID, Output: output, Duration: time.Since(start).Milliseconds()}
		if callErr != nil {
			// Reconnect once after transport/process failure.
			m.mu.Lock()
			m.stopLocked(config)
			restarted, restartErr := m.ensureClientLocked(ctx, config, project)
			m.mu.Unlock()
			if restartErr == nil {
				output, callErr = restarted.CallTool(ctx, originalName, input)
				result.Output = output
			}
		}
		if callErr != nil {
			result.Error = callErr.Error()
		}
		return result, true
	}
	return nativeToolResult{ToolID: call.ID, CallID: call.CallID, Error: "MCP tool is unavailable or its plugin is disabled"}, true
}

func (m *pluginManager) BuildGraphify(ctx context.Context, project, id string) (processSnapshot, pluginView, error) {
	config, found, err := m.store.find(project, id)
	if err != nil {
		return processSnapshot{}, pluginView{}, err
	}
	if !found {
		return processSnapshot{}, pluginView{}, os.ErrNotExist
	}
	if !isGraphifyPlugin(config) {
		return processSnapshot{}, pluginView{}, errors.New("plugin is not a Graphify integration")
	}
	if _, ok := lookPathPluginExecutable("graphify"); !ok {
		return processSnapshot{}, m.viewConfig(project, config, false), errors.New("graphify executable was not found in PATH")
	}
	root := project
	if config.Scope == "project" {
		root = config.Project
	}
	snapshot, runErr := m.processes.run(ctx, "graphify extract . --code-only", root)
	m.mu.Lock()
	m.stopLocked(config)
	m.mu.Unlock()
	view := m.viewConfig(project, config, config.Enabled)
	if runErr != nil {
		return snapshot, view, runErr
	}
	return snapshot, view, nil
}
