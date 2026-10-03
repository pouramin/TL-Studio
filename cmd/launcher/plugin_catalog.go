package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type pluginCatalogEntry struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Category    string            `json:"category"`
	Icon        string            `json:"icon,omitempty"`
	Scope       string            `json:"scope"`
	Upstream    string            `json:"upstream,omitempty"`

	Type        string            `json:"-"`
	Transport   string            `json:"-"`
	PackageSpec string            `json:"-"`
	Executable  string            `json:"-"`
	Module      string            `json:"-"`
	Arguments   []string          `json:"-"`
	Environment map[string]string `json:"-"`
	Metadata    map[string]string `json:"-"`
}

type pluginCatalogRuntime struct {
	Command   string
	Arguments []string
	Metadata  map[string]string
}

func availablePluginCatalog() []pluginCatalogEntry {
	return []pluginCatalogEntry{
		{
			ID:          "graphify",
			Name:        "Graphify",
			Description: "Build a local code knowledge graph and expose it to the Agent through MCP.",
			Category:    "Code intelligence",
			Icon:        "/plugin-graphify.svg",
			Type:        pluginTypeMCP,
			Scope:       "project",
			Transport:   pluginTransportStdio,
			PackageSpec: "graphifyy[mcp]",
			Executable:  "graphify-mcp",
			Module:      "graphify.serve",
			Arguments:   []string{"graphify-out/graph.json"},
			Upstream:    "https://github.com/Graphify-Labs/graphify",
			Metadata: map[string]string{
				"integration": "graphify",
				"graphPath":   "graphify-out/graph.json",
			},
		},
		{
			ID:          "laya",
			Name:        "Laya",
			Description: "Run local typed decisions for routing, scoring, yes/no, triage, and guardrails.",
			Category:    "Decision engine",
			Icon:        "/plugin-laya.svg",
			Type:        pluginTypeMCP,
			Scope:       "global",
			Transport:   pluginTransportStdio,
			PackageSpec: "laya[mcp]",
			Executable:  "laya-mcp-server",
			Module:      "laya.mcp.server",
			Upstream:    "https://github.com/NandhaKishorM/laya",
			Environment: map[string]string{
				// Keep Add responsive. Laya loads model weights lazily on first use.
				"LAYA_PRELOAD": "0",
			},
		},
	}
}

func pluginCatalogEntryByID(id string) (pluginCatalogEntry, bool) {
	id = normalizePluginID(id)
	for _, entry := range availablePluginCatalog() {
		if entry.ID == id {
			return entry, true
		}
	}
	return pluginCatalogEntry{}, false
}

var runPluginCatalogCommand = runPluginCatalogCommandDefault

func runPluginCatalogCommandDefault(ctx context.Context, directory, command string, arguments ...string) (string, error) {
	cmd := exec.CommandContext(ctx, command, arguments...)
	if strings.TrimSpace(directory) != "" {
		cmd.Dir = directory
	}
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if len(text) > 6000 {
		text = text[len(text)-6000:]
	}
	if err != nil {
		if text != "" {
			return text, fmt.Errorf("%s failed: %w: %s", filepath.Base(command), err, text)
		}
		return text, fmt.Errorf("%s failed: %w", filepath.Base(command), err)
	}
	return text, nil
}

func catalogExecutableName(name string) string {
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(name), ".exe") {
		return name + ".exe"
	}
	return name
}

func discoverUVExecutable() (string, bool) {
	if path, err := exec.LookPath("uv"); err == nil {
		return path, true
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	for _, candidate := range []string{
		filepath.Join(home, ".local", "bin", catalogExecutableName("uv")),
		filepath.Join(home, ".cargo", "bin", catalogExecutableName("uv")),
	} {
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return candidate, true
		}
	}
	return "", false
}

type catalogPython struct {
	Command string
	Prefix  []string
}

func discoverCatalogPython() (catalogPython, bool) {
	for _, candidate := range []struct {
		name   string
		prefix []string
	}{
		{"python", nil},
		{"python3", nil},
		{"py", []string{"-3"}},
	} {
		path, err := exec.LookPath(candidate.name)
		if err == nil {
			return catalogPython{Command: path, Prefix: append([]string(nil), candidate.prefix...)}, true
		}
	}
	return catalogPython{}, false
}

func installPluginCatalogRuntime(ctx context.Context, entry pluginCatalogEntry) (pluginCatalogRuntime, error) {
	if uv, ok := discoverUVExecutable(); ok {
		if _, err := runPluginCatalogCommand(ctx, "", uv, "tool", "install", "--force", entry.PackageSpec); err != nil {
			return pluginCatalogRuntime{}, fmt.Errorf("could not install %s with uv: %w", entry.Name, err)
		}
		binOutput, err := runPluginCatalogCommand(ctx, "", uv, "tool", "dir", "--bin")
		if err != nil {
			return pluginCatalogRuntime{}, fmt.Errorf("could not locate the uv tool bin directory: %w", err)
		}
		binDir := strings.TrimSpace(strings.Split(binOutput, "\n")[len(strings.Split(binOutput, "\n"))-1])
		command := filepath.Join(binDir, catalogExecutableName(entry.Executable))
		if info, statErr := os.Stat(command); statErr != nil || info.IsDir() {
			return pluginCatalogRuntime{}, fmt.Errorf("%s installed, but %s was not found in %s", entry.Name, catalogExecutableName(entry.Executable), binDir)
		}
		metadata := map[string]string{"catalog": entry.ID, "catalogRuntime": "uv-tool"}
		for key, value := range entry.Metadata {
			metadata[key] = value
		}
		if entry.ID == "graphify" {
			metadata["graphifyCLI"] = filepath.Join(binDir, catalogExecutableName("graphify"))
		}
		return pluginCatalogRuntime{
			Command:   command,
			Arguments: append([]string(nil), entry.Arguments...),
			Metadata:  metadata,
		}, nil
	}

	python, ok := discoverCatalogPython()
	if !ok {
		return pluginCatalogRuntime{}, errors.New("automatic plugin installation requires uv or Python 3")
	}
	installArgs := append(append([]string(nil), python.Prefix...), "-m", "pip", "install", "--user", entry.PackageSpec)
	if _, err := runPluginCatalogCommand(ctx, "", python.Command, installArgs...); err != nil {
		return pluginCatalogRuntime{}, fmt.Errorf("could not install %s with pip: %w", entry.Name, err)
	}
	arguments := append(append([]string(nil), python.Prefix...), "-m", entry.Module)
	arguments = append(arguments, entry.Arguments...)
	metadata := map[string]string{"catalog": entry.ID, "catalogRuntime": "python"}
	for key, value := range entry.Metadata {
		metadata[key] = value
	}
	return pluginCatalogRuntime{
		Command:   python.Command,
		Arguments: arguments,
		Metadata:  metadata,
	}, nil
}

func (m *pluginManager) InstallCatalogPlugin(ctx context.Context, project, id string) (pluginView, error) {
	entry, ok := pluginCatalogEntryByID(id)
	if !ok {
		return pluginView{}, os.ErrNotExist
	}
	if entry.Scope == "project" && strings.TrimSpace(project) == "" {
		return pluginView{}, errors.New("open a project before installing this plugin")
	}
	if _, found, err := m.store.find(project, entry.ID); err != nil {
		return pluginView{}, err
	} else if found {
		return pluginView{}, errors.New("plugin is already added")
	}

	runtimeConfig, err := installPluginCatalogRuntime(ctx, entry)
	if err != nil {
		return pluginView{}, err
	}
	config := pluginConfig{
		ID:          entry.ID,
		Name:        entry.Name,
		Description: entry.Description,
		Type:        entry.Type,
		Scope:       entry.Scope,
		Transport:   entry.Transport,
		Command:     runtimeConfig.Command,
		Arguments:   runtimeConfig.Arguments,
		Metadata:    runtimeConfig.Metadata,
	}

	if entry.ID == "graphify" {
		if _, err := runGraphifyBuild(ctx, config, project); err != nil {
			return pluginView{}, fmt.Errorf("Graphify installed, but the initial project graph could not be built: %w", err)
		}
	}

	environment := map[string]string{}
	for key, value := range entry.Environment {
		environment[key] = value
	}
	if _, err := m.Upsert(project, pluginUpsertRequest{Plugin: config, Environment: &environment}); err != nil {
		return pluginView{}, err
	}
	view, err := m.SetEnabled(project, entry.ID, true)
	if err != nil {
		_ = m.Remove(project, entry.ID)
		return view, err
	}
	return view, nil
}
