package main

import (
	"net/http"
	"strings"
)

const toolRegistryVersion = 2

type toolCapabilities struct {
	Read    bool `json:"read"`
	Write   bool `json:"write"`
	Execute bool `json:"execute"`
	Network bool `json:"network"`
}

type toolDescriptor struct {
	ID              string           `json:"id"`
	RuntimeIDs      []string         `json:"runtimeIDs"`
	Name            string           `json:"name"`
	Description     string           `json:"description"`
	Category        string           `json:"category"`
	PermissionClass string           `json:"permissionClass"`
	Capabilities    toolCapabilities `json:"capabilities"`
	Presentation    string           `json:"presentation"`
	InputSchema     map[string]any   `json:"inputSchema,omitempty"`
	Source          string           `json:"source,omitempty"`
	PluginID        string           `json:"pluginID,omitempty"`
}

type toolRegistryPayload struct {
	Version int              `json:"version"`
	Tools   []toolDescriptor `json:"tools"`
	Unknown toolDescriptor   `json:"unknown"`
}

func builtInToolRegistry() []toolDescriptor {
	return []toolDescriptor{
		{ID: "files.read", RuntimeIDs: []string{"read"}, Name: "Read file", Description: "Read file content from the workspace.", Category: "files", PermissionClass: "read", Capabilities: toolCapabilities{Read: true}, Presentation: "file-read"},
		{ID: "files.list", RuntimeIDs: []string{"list"}, Name: "List files", Description: "List files and directories.", Category: "files", PermissionClass: "read", Capabilities: toolCapabilities{Read: true}, Presentation: "file-list"},
		{ID: "search.files", RuntimeIDs: []string{"glob"}, Name: "Find files", Description: "Find files by path pattern.", Category: "search", PermissionClass: "read", Capabilities: toolCapabilities{Read: true}, Presentation: "search-files"},
		{ID: "search.content", RuntimeIDs: []string{"grep"}, Name: "Search content", Description: "Search text inside workspace files.", Category: "search", PermissionClass: "read", Capabilities: toolCapabilities{Read: true}, Presentation: "search-content"},
		{ID: "search.code", RuntimeIDs: []string{"codesearch"}, Name: "Code search", Description: "Search the codebase using the runtime's code-search capability.", Category: "search", PermissionClass: "read", Capabilities: toolCapabilities{Read: true}, Presentation: "search-code"},
		{ID: "search.semantic", RuntimeIDs: []string{"semantic_search"}, Name: "Semantic search", Description: "Search the codebase by semantic meaning.", Category: "search", PermissionClass: "read", Capabilities: toolCapabilities{Read: true}, Presentation: "search-semantic"},
		{ID: "files.write", RuntimeIDs: []string{"write"}, Name: "Write file", Description: "Create or replace file content.", Category: "files", PermissionClass: "write", Capabilities: toolCapabilities{Write: true}, Presentation: "file-write"},
		{ID: "files.edit", RuntimeIDs: []string{"edit"}, Name: "Edit file", Description: "Modify existing file content.", Category: "files", PermissionClass: "write", Capabilities: toolCapabilities{Write: true}, Presentation: "file-edit"},
		{ID: "files.patch", RuntimeIDs: []string{"apply_patch"}, Name: "Apply patch", Description: "Apply a structured patch to workspace files.", Category: "files", PermissionClass: "write", Capabilities: toolCapabilities{Write: true}, Presentation: "file-patch"},
		{ID: "terminal.command", RuntimeIDs: []string{"bash"}, Name: "Run command", Description: "Execute a local shell command through the agent runtime.", Category: "terminal", PermissionClass: "execute", Capabilities: toolCapabilities{Read: true, Write: true, Execute: true, Network: true}, Presentation: "terminal"},
		{ID: "terminal.background", RuntimeIDs: []string{"background_process"}, Name: "Background process", Description: "Run a local process in the background through the agent runtime.", Category: "terminal", PermissionClass: "execute", Capabilities: toolCapabilities{Read: true, Write: true, Execute: true, Network: true}, Presentation: "terminal-background"},
		{ID: "network.fetch", RuntimeIDs: []string{"webfetch"}, Name: "Fetch URL", Description: "Retrieve content from a network URL.", Category: "network", PermissionClass: "network", Capabilities: toolCapabilities{Read: true, Network: true}, Presentation: "network-fetch"},
		{ID: "network.search", RuntimeIDs: []string{"websearch"}, Name: "Search web", Description: "Search external web sources.", Category: "network", PermissionClass: "network", Capabilities: toolCapabilities{Read: true, Network: true}, Presentation: "network-search"},
		{ID: "agent.subtask", RuntimeIDs: []string{"task"}, Name: "Run subtask", Description: "Delegate a scoped task to another agent context.", Category: "agent", PermissionClass: "delegate", Capabilities: toolCapabilities{}, Presentation: "agent-subtask"},
		{ID: "agent.skill", RuntimeIDs: []string{"skill"}, Name: "Use skill", Description: "Load or invoke an agent skill.", Category: "agent", PermissionClass: "delegate", Capabilities: toolCapabilities{}, Presentation: "agent-skill"},
		{ID: "agent.plan-enter", RuntimeIDs: []string{"plan_enter"}, Name: "Enter plan", Description: "Enter the runtime's planning workflow.", Category: "agent", PermissionClass: "workflow", Capabilities: toolCapabilities{}, Presentation: "plan"},
		{ID: "agent.plan-exit", RuntimeIDs: []string{"plan_exit"}, Name: "Exit plan", Description: "Exit the runtime's planning workflow.", Category: "agent", PermissionClass: "workflow", Capabilities: toolCapabilities{}, Presentation: "plan"},
		{ID: "session.todo-write", RuntimeIDs: []string{"todowrite"}, Name: "Update tasks", Description: "Update the agent session task list.", Category: "session", PermissionClass: "session", Capabilities: toolCapabilities{Write: true}, Presentation: "tasks-write"},
		{ID: "session.todo-read", RuntimeIDs: []string{"todoread"}, Name: "Read tasks", Description: "Read the agent session task list.", Category: "session", PermissionClass: "session", Capabilities: toolCapabilities{Read: true}, Presentation: "tasks-read"},
		{ID: "interaction.question", RuntimeIDs: []string{"question"}, Name: "Ask question", Description: "Request interactive input from the user.", Category: "interaction", PermissionClass: "interactive", Capabilities: toolCapabilities{}, Presentation: "question"},
		{ID: "interaction.suggest", RuntimeIDs: []string{"suggest"}, Name: "Suggest action", Description: "Present a runtime suggestion to the user.", Category: "interaction", PermissionClass: "interactive", Capabilities: toolCapabilities{}, Presentation: "suggest"},
		{ID: "code.lsp", RuntimeIDs: []string{"lsp"}, Name: "Code intelligence", Description: "Query language-server code intelligence.", Category: "code", PermissionClass: "read", Capabilities: toolCapabilities{Read: true}, Presentation: "code-intelligence"},
		{ID: "presentation.chart", RuntimeIDs: []string{"chart"}, Name: "Create chart", Description: "Create a structured chart result.", Category: "presentation", PermissionClass: "runtime", Capabilities: toolCapabilities{}, Presentation: "chart"},
		{ID: "orchestration.batch", RuntimeIDs: []string{"batch"}, Name: "Run tool batch", Description: "Run a runtime-managed batch of tool operations.", Category: "orchestration", PermissionClass: "runtime", Capabilities: toolCapabilities{Read: true, Write: true, Execute: true, Network: true}, Presentation: "batch"},
	}
}

func unknownToolDescriptor() toolDescriptor {
	return toolDescriptor{
		ID:              "runtime.unknown",
		Name:            "Runtime tool",
		Description:     "Tool not recognized by this TL Studio registry version. Runtime permission enforcement remains authoritative.",
		Category:        "runtime",
		PermissionClass: "runtime",
		Capabilities:    toolCapabilities{},
		Presentation:    "tool",
	}
}

func toolDescriptorForRuntimeID(runtimeID string) (toolDescriptor, bool) {
	runtimeID = strings.TrimSpace(runtimeID)
	for _, descriptor := range builtInToolRegistry() {
		for _, candidate := range descriptor.RuntimeIDs {
			if candidate == runtimeID {
				return descriptor, true
			}
		}
	}
	fallback := unknownToolDescriptor()
	if runtimeID != "" {
		fallback.RuntimeIDs = []string{runtimeID}
	}
	return fallback, false
}

func currentToolRegistry() toolRegistryPayload {
	return toolRegistryPayload{
		Version: toolRegistryVersion,
		Tools:   builtInToolRegistry(),
		Unknown: unknownToolDescriptor(),
	}
}

func currentToolRegistryWithPlugins(plugins *pluginManager, project string) toolRegistryPayload {
	payload := currentToolRegistry()
	if plugins != nil {
		payload.Tools = append(payload.Tools, plugins.ToolDescriptors(project)...)
	}
	return payload
}

func registerToolRegistryRoutes(mux *http.ServeMux) {
	registerToolRegistryRoutesWithPlugins(mux, nil, nil)
}

func registerToolRegistryRoutesWithPlugins(mux *http.ServeMux, plugins *pluginManager, project func() string) {
	mux.HandleFunc("GET /local/tools", func(w http.ResponseWriter, _ *http.Request) {
		currentProject := ""
		if project != nil {
			currentProject = project()
		}
		writeJSON(w, http.StatusOK, currentToolRegistryWithPlugins(plugins, currentProject))
	})
}
