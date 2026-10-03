package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type graphifyPluginIntegration struct{}

func (graphifyPluginIntegration) Matches(config pluginConfig) bool {
	name := strings.ToLower(filepath.Base(strings.TrimSpace(config.Command)))
	return name == "graphify-mcp" || name == "graphify-mcp.exe" || strings.EqualFold(config.Metadata["integration"], "graphify")
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

type graphifyFiles struct {
	available  bool
	modifiedAt string
	graphPath  string
	htmlPath   string
	reportPath string
	cli        bool
	mcp        bool
}

func graphifyCLIInvocation(config pluginConfig) (string, []string, bool) {
	if strings.EqualFold(config.Metadata["catalogRuntime"], "python") {
		moduleIndex := -1
		for index, arg := range config.Arguments {
			if arg == "-m" {
				moduleIndex = index
				break
			}
		}
		if moduleIndex >= 0 {
			prefix := append([]string(nil), config.Arguments[:moduleIndex]...)
			return config.Command, append(prefix, "-m", "graphify"), true
		}
	}
	if explicit := strings.TrimSpace(config.Metadata["graphifyCLI"]); explicit != "" {
		if path, ok := lookPathPluginExecutable(explicit); ok {
			return path, nil, true
		}
	}
	if strings.ContainsAny(config.Command, `/\\`) {
		sibling := filepath.Join(filepath.Dir(config.Command), catalogExecutableName("graphify"))
		if path, ok := lookPathPluginExecutable(sibling); ok {
			return path, nil, true
		}
	}
	if path, ok := lookPathPluginExecutable("graphify"); ok {
		return path, nil, true
	}
	return "", nil, false
}

func graphifyGraphPath(config pluginConfig) string {
	if value := strings.TrimSpace(config.Metadata["graphPath"]); value != "" {
		return filepath.ToSlash(value)
	}
	if !strings.EqualFold(config.Metadata["catalogRuntime"], "python") &&
		len(config.Arguments) > 0 && strings.TrimSpace(config.Arguments[0]) != "" {
		return filepath.ToSlash(config.Arguments[0])
	}
	return "graphify-out/graph.json"
}

func graphifyFilesFor(config pluginConfig, project string) graphifyFiles {
	if config.Scope == "project" {
		project = config.Project
	}
	status := graphifyFiles{}
	_, _, status.cli = graphifyCLIInvocation(config)
	_, status.mcp = lookPathPluginExecutable(config.Command)

	graphRel := graphifyGraphPath(config)
	if target, rel, err := resolveProjectEntry(project, graphRel); err == nil {
		if info, statErr := os.Stat(target); statErr == nil && info.Mode().IsRegular() {
			status.available = true
			status.graphPath = filepath.ToSlash(rel)
			status.modifiedAt = info.ModTime().UTC().Format(time.RFC3339)
		}
	}
	for _, candidate := range []struct {
		rel   string
		field *string
	}{
		{"graphify-out/graph.html", &status.htmlPath},
		{"graphify-out/GRAPH_REPORT.md", &status.reportPath},
	} {
		if target, rel, err := resolveProjectEntry(project, candidate.rel); err == nil {
			if info, statErr := os.Stat(target); statErr == nil && info.Mode().IsRegular() {
				*candidate.field = filepath.ToSlash(rel)
			}
		}
	}
	return status
}

func (graphifyPluginIntegration) Snapshot(config pluginConfig, project string) *pluginIntegrationView {
	files := graphifyFilesFor(config, project)
	view := &pluginIntegrationView{
		ID: "graphify",
		Details: map[string]string{
			"cliAvailable": boolText(files.cli),
			"mcpAvailable": boolText(files.mcp),
		},
	}
	if files.available {
		view.Status = "Ready"
		view.Summary = "Graph ready"
		if files.modifiedAt != "" {
			view.Summary += " · " + files.modifiedAt
			view.Details["modifiedAt"] = files.modifiedAt
		}
		view.Details["graphPath"] = files.graphPath
	} else {
		view.Status = "Graph Missing"
		view.Summary = "Graph missing"
	}
	if files.reportPath != "" {
		view.Details["reportPath"] = files.reportPath
	}
	view.Actions = append(view.Actions, pluginActionDescriptor{
		ID: "build-graph",
		Label: func() string {
			if files.available { return "Rebuild Graph" }
			return "Build Graph"
		}(),
		Kind: "server",
		RequiresConfirmation: true,
		Confirmation: "Run this local command in the current project?\n\ngraphify extract . --code-only",
	})
	if files.htmlPath != "" {
		view.Details["htmlPath"] = files.htmlPath
		view.Actions = append(view.Actions, pluginActionDescriptor{
			ID: "open-graph",
			Label: "Open Graph",
			Kind: "preview",
			Target: files.htmlPath,
		})
	}
	return view
}

func boolText(value bool) string {
	if value { return "true" }
	return "false"
}

func (graphifyPluginIntegration) ValidateStart(config pluginConfig, project string) error {
	files := graphifyFilesFor(config, project)
	if !files.available {
		return errors.New("Graphify graph is missing; build the graph before enabling MCP queries")
	}
	return nil
}

func runGraphifyBuild(ctx context.Context, config pluginConfig, project string) (string, error) {
	command, prefix, ok := graphifyCLIInvocation(config)
	if !ok {
		return "", errors.New("graphify executable was not found")
	}
	root := project
	if config.Scope == "project" && strings.TrimSpace(config.Project) != "" {
		root = config.Project
	}
	args := append(append([]string(nil), prefix...), "extract", ".", "--code-only")
	output, err := runPluginCatalogCommand(ctx, root, command, args...)
	if err != nil {
		return output, err
	}
	return output, nil
}

func (graphifyPluginIntegration) RunAction(ctx context.Context, manager *pluginManager, config pluginConfig, project, actionID string) (any, error) {
	if actionID != "build-graph" {
		return nil, errors.New("unsupported Graphify integration action")
	}
	output, runErr := runGraphifyBuild(ctx, config, project)
	manager.mu.Lock()
	manager.stopLocked(config)
	manager.mu.Unlock()
	return map[string]any{"output": output}, runErr
}
