package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	nativeToolDefaultTimeout      = 2 * time.Minute
	nativeToolMaxTimeout          = 5 * time.Minute
	nativeAgentReadDefaultLines   = 400
	nativeAgentReadMaxLines       = 800
)

type nativeToolCall struct {
	ID        string
	CallID    string
	Arguments json.RawMessage
}

type nativeToolResult struct {
	ToolID   string              `json:"toolID"`
	CallID   string              `json:"callID,omitempty"`
	Output   any                 `json:"output,omitempty"`
	Changes  []sessionChangeView `json:"changes,omitempty"`
	Error    string              `json:"error,omitempty"`
	Duration int64               `json:"durationMs,omitempty"`
}

type nativeToolAuthorizer interface {
	AuthorizeNativeTool(ctx context.Context, sessionID, project string, descriptor toolDescriptor, input map[string]any) error
}

type nativeToolExecutor struct {
	processes   *processManager
	permissions nativeToolAuthorizer
	plugins     *pluginManager
	questions   *questionContract
}

func newNativeToolExecutor(processes *processManager, permissions nativeToolAuthorizer) *nativeToolExecutor {
	if processes == nil {
		processes = newProcessManager(nil)
	}
	return &nativeToolExecutor{processes: processes, permissions: permissions}
}

func (e *nativeToolExecutor) setPluginManager(plugins *pluginManager) {
	e.plugins = plugins
}

func (e *nativeToolExecutor) setQuestionManager(questions *questionContract) {
	e.questions = questions
}

func (e *nativeToolExecutor) Descriptor(project, id string) (toolDescriptor, bool) {
	if descriptor, ok := toolDescriptorForID(id); ok {
		return descriptor, true
	}
	if e.plugins != nil {
		return e.plugins.Descriptor(project, id)
	}
	return unknownToolDescriptor(), false
}

func nativeExecutableToolIDs() []string {
	return []string{
		"files.read",
		"files.list",
		"files.write",
		"files.edit",
		"files.delete",
		"search.content",
		"terminal.command",
		"interaction.question",
	}
}

func toolDescriptorForID(id string) (toolDescriptor, bool) {
	id = strings.TrimSpace(id)
	for _, descriptor := range builtInToolRegistry() {
		if descriptor.ID == id {
			return descriptor, true
		}
	}
	return unknownToolDescriptor(), false
}

func nativeToolInputSchema(id string) map[string]any {
	stringProperty := func(description string) map[string]any {
		return map[string]any{"type": "string", "description": description}
	}
	switch id {
	case "files.read":
		return map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": stringProperty("Project-relative file path."),
				"startLine": map[string]any{
					"type": "integer",
					"minimum": 1,
					"description": "Optional 1-based first line to read. Use with large files instead of rereading the whole file.",
				},
				"endLine": map[string]any{
					"type": "integer",
					"minimum": 1,
					"description": fmt.Sprintf("Optional inclusive last line. A single read is capped at %d lines.", nativeAgentReadMaxLines),
				},
			},
			"required": []string{"path"},
			"additionalProperties": false,
		}
	case "files.list":
		return map[string]any{
			"type": "object",
			"properties": map[string]any{"path": stringProperty("Project-relative directory path. Empty means project root.")},
			"additionalProperties": false,
		}
	case "files.write":
		return map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    stringProperty("Project-relative file path."),
				"content": stringProperty("Complete file content to write."),
				"expectedSha256": stringProperty("Required when overwriting an existing file: exact sha256 from a recent files.read result. Omit only for creating a new file. If a conflict occurs, read again before retrying."),
			},
			"required": []string{"path", "content"},
			"additionalProperties": false,
		}
	case "files.edit":
		return map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": stringProperty("Project-relative file path."),
				"old":  stringProperty("Exact text to replace."),
				"new":  stringProperty("Replacement text."),
				"expectedSha256": stringProperty("Required sha256 from a recent files.read result for this file. Prevents edits based on stale content. If a conflict occurs, read again before retrying."),
				"all":  map[string]any{"type": "boolean", "description": "Replace every exact occurrence instead of exactly one."},
			},
			"required": []string{"path", "old", "new", "expectedSha256"},
			"additionalProperties": false,
		}
	case "files.delete":
		return map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": stringProperty("Project-relative path to one file (directories and symlinks are not supported)."),
				"expectedSha256": stringProperty("Required SHA-256 from files.read for exactly this file. If it has changed, stop and read again."),
			},
			"required": []string{"path", "expectedSha256"},
			"additionalProperties": false,
		}
	case "search.content":
		return map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query":         stringProperty("Literal text to search for."),
				"include":       stringProperty("Optional comma-separated include patterns."),
				"exclude":       stringProperty("Optional comma-separated exclude patterns."),
				"caseSensitive": map[string]any{"type": "boolean"},
				"limit":         map[string]any{"type": "integer", "minimum": 1, "maximum": maxProjectSearchResults},
			},
			"required": []string{"query"},
			"additionalProperties": false,
		}
	case "terminal.command":
		return map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command":        stringProperty("Shell command to run in the selected project."),
				"timeoutSeconds": map[string]any{
					"type": "integer",
					"minimum": 1,
					"maximum": int(nativeToolMaxTimeout / time.Second),
					"description": "Maximum allowed runtime before TL Studio cancels the command. This is a deadline, not a sleep or delay duration.",
				},
			},
			"required": []string{"command"},
			"additionalProperties": false,
		}
	case "interaction.question":
		return map[string]any{
			"type": "object",
			"properties": map[string]any{
				"questions": map[string]any{
					"type": "array",
					"minItems": 1,
					"maxItems": 20,
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"header": stringProperty("Optional short label for the question."),
							"question": stringProperty("Question text shown to the user."),
							"options": map[string]any{
								"type": "array",
								"items": map[string]any{
									"type": "object",
									"properties": map[string]any{
										"label": stringProperty("Selectable answer label."),
										"description": stringProperty("Optional explanation for the option."),
									},
									"required": []string{"label"},
									"additionalProperties": false,
								},
							},
							"multiple": map[string]any{"type": "boolean"},
							"custom": map[string]any{"type": "boolean"},
							"default": stringProperty("Optional default custom answer."),
						},
						"required": []string{"question"},
						"additionalProperties": false,
					},
				},
			},
			"required": []string{"questions"},
			"additionalProperties": false,
		}
	default:
		return map[string]any{"type": "object", "additionalProperties": false}
	}
}

func (e *nativeToolExecutor) ToolDefinitions() []nativeModelToolDefinition {
	return e.ToolDefinitionsForProject("")
}

func (e *nativeToolExecutor) ToolDefinitionsForProject(project string) []nativeModelToolDefinition {
	ids := nativeExecutableToolIDs()
	definitions := make([]nativeModelToolDefinition, 0, len(ids)+8)
	for _, id := range ids {
		descriptor, ok := toolDescriptorForID(id)
		if !ok {
			continue
		}
		definitions = append(definitions, nativeModelToolDefinition{
			ID:          descriptor.ID,
			Name:        descriptor.Name,
			Description: descriptor.Description,
			InputSchema: nativeToolInputSchema(descriptor.ID),
		})
	}
	if e.plugins != nil && strings.TrimSpace(project) != "" {
		definitions = append(definitions, e.plugins.ToolDefinitions(project)...)
	}
	return definitions
}

func decodeNativeToolArguments(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var input map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&input); err != nil {
		return nil, fmt.Errorf("invalid tool arguments: %w", err)
	}
	if input == nil {
		return map[string]any{}, nil
	}
	return input, nil
}

func nativeRequiredString(input map[string]any, key string, allowEmpty bool) (string, error) {
	value, ok := input[key]
	if !ok {
		return "", fmt.Errorf("%s is required", key)
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", key)
	}
	if !allowEmpty && strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("%s cannot be empty", key)
	}
	return text, nil
}

func nativeOptionalString(input map[string]any, key string) (string, error) {
	value, ok := input[key]
	if !ok || value == nil {
		return "", nil
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", key)
	}
	return text, nil
}

func nativeOptionalBool(input map[string]any, key string) (bool, error) {
	value, ok := input[key]
	if !ok || value == nil {
		return false, nil
	}
	flag, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("%s must be a boolean", key)
	}
	return flag, nil
}

func nativeOptionalInt(input map[string]any, key string, fallback int) (int, error) {
	value, ok := input[key]
	if !ok || value == nil {
		return fallback, nil
	}
	switch number := value.(type) {
	case json.Number:
		parsed, err := number.Int64()
		if err != nil {
			return 0, fmt.Errorf("%s must be an integer", key)
		}
		return int(parsed), nil
	case float64:
		if number != float64(int(number)) {
			return 0, fmt.Errorf("%s must be an integer", key)
		}
		return int(number), nil
	case int:
		return number, nil
	default:
		return 0, fmt.Errorf("%s must be an integer", key)
	}
}

func (e *nativeToolExecutor) nativeAskQuestion(ctx context.Context, sessionID string, input map[string]any) (any, error) {
	if e.questions == nil {
		return nil, errors.New("native interactive questions are unavailable")
	}
	raw, ok := input["questions"]
	if !ok {
		return nil, errors.New("questions are required")
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var prompts []questionPromptView
	if err := json.Unmarshal(encoded, &prompts); err != nil {
		return nil, errors.New("questions must use the native question schema")
	}
	answers, rejected, err := e.questions.Ask(ctx, sessionID, prompts)
	if err != nil {
		return nil, err
	}
	if rejected {
		return map[string]any{"rejected": true, "answers": [][]string{}}, nil
	}
	return map[string]any{"rejected": false, "answers": answers}, nil
}

func (e *nativeToolExecutor) Execute(ctx context.Context, sessionID, project string, call nativeToolCall) nativeToolResult {
	start := time.Now()
	result := nativeToolResult{ToolID: strings.TrimSpace(call.ID), CallID: strings.TrimSpace(call.CallID)}
	defer func() {
		result.Duration = time.Since(start).Milliseconds()
	}()

	if e.plugins != nil && strings.HasPrefix(result.ToolID, "mcp.") {
		if pluginResult, handled := e.plugins.Execute(ctx, sessionID, project, call); handled {
			return pluginResult
		}
	}

	descriptor, known := toolDescriptorForID(result.ToolID)
	if !known || descriptor.ID == "runtime.unknown" {
		result.Error = "unknown TL Studio tool: " + result.ToolID
		return result
	}
	executable := false
	for _, id := range nativeExecutableToolIDs() {
		if id == descriptor.ID {
			executable = true
			break
		}
	}
	if !executable {
		result.Error = "tool is not executable by the TL Studio native tool executor: " + descriptor.ID
		return result
	}

	input, err := decodeNativeToolArguments(call.Arguments)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	if descriptor.ID == "interaction.question" {
		result.Output, err = e.nativeAskQuestion(ctx, sessionID, input)
		if err != nil {
			result.Error = err.Error()
		}
		result.Duration = time.Since(start).Milliseconds()
		return result
	}
	if e.permissions != nil {
		if err := e.permissions.AuthorizeNativeTool(ctx, sessionID, project, descriptor, input); err != nil {
			result.Error = err.Error()
			return result
		}
	}

	switch descriptor.ID {
	case "files.read":
		result.Output, err = nativeReadFile(project, input)
	case "files.list":
		result.Output, err = nativeListFiles(project, input)
	case "files.write":
		result.Output, result.Changes, err = nativeWriteFile(project, input)
	case "files.edit":
		result.Output, result.Changes, err = nativeEditFile(project, input)
	case "files.delete":
		result.Output, result.Changes, err = nativeDeleteFile(project, input)
	case "search.content":
		result.Output, err = nativeSearchContent(ctx, project, input)
	case "terminal.command":
		result.Output, err = e.nativeRunCommand(ctx, project, input)
	default:
		err = errors.New("native tool handler is not registered")
	}
	if err != nil {
		result.Error = err.Error()
	}
	result.Duration = time.Since(start).Milliseconds()
	return result
}

func nativeReadFile(project string, input map[string]any) (any, error) {
	path, err := nativeRequiredString(input, "path", false)
	if err != nil {
		return nil, err
	}
	startLine, err := nativeOptionalInt(input, "startLine", 1)
	if err != nil {
		return nil, err
	}
	endLine, err := nativeOptionalInt(input, "endLine", 0)
	if err != nil {
		return nil, err
	}
	if startLine < 1 {
		return nil, errors.New("startLine must be at least 1")
	}
	if endLine > 0 && endLine < startLine {
		return nil, errors.New("endLine must be greater than or equal to startLine")
	}
	if endLine > 0 && endLine-startLine+1 > nativeAgentReadMaxLines {
		return nil, fmt.Errorf("files.read supports at most %d lines per call; request a smaller range", nativeAgentReadMaxLines)
	}

	preview, _, err := readLocalFilePreview(project, path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("project-relative file %q does not exist; inspect the project root with files.list path \"\" before guessing another path", filepath.ToSlash(path))
		}
		return nil, err
	}
	if preview.Binary {
		return nil, errors.New("binary files are not readable by the coding Agent")
	}

	lines := strings.Split(preview.Content, "\n")
	totalLines := len(lines)
	if preview.Content == "" {
		totalLines = 0
	}
	if totalLines == 0 {
		return map[string]any{
			"path": preview.Path, "content": "", "sha256": preview.SHA256, "size": preview.Size,
			"startLine": 0, "endLine": 0, "totalLines": 0, "truncated": false,
		}, nil
	}
	if startLine > totalLines {
		return nil, fmt.Errorf("startLine %d is beyond end of file (%d lines)", startLine, totalLines)
	}

	requestedEnd := endLine
	if requestedEnd == 0 {
		requestedEnd = startLine + nativeAgentReadDefaultLines - 1
	}
	if requestedEnd > totalLines {
		requestedEnd = totalLines
	}
	if requestedEnd-startLine+1 > nativeAgentReadMaxLines {
		requestedEnd = startLine + nativeAgentReadMaxLines - 1
	}
	content := strings.Join(lines[startLine-1:requestedEnd], "\n")
	truncated := startLine > 1 || requestedEnd < totalLines
	result := map[string]any{
		"path": preview.Path,
		"content": content,
		"sha256": preview.SHA256,
		"size": preview.Size,
		"startLine": startLine,
		"endLine": requestedEnd,
		"totalLines": totalLines,
		"truncated": truncated,
	}
	if requestedEnd < totalLines {
		result["nextStartLine"] = requestedEnd + 1
	}
	return result, nil
}

func nativeListFiles(project string, input map[string]any) (any, error) {
	path, err := nativeOptionalString(input, "path")
	if err != nil {
		return nil, err
	}
	target, rel, err := resolveProjectEntry(project, path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("project-relative directory %q does not exist; inspect the project root with files.list path \"\" before guessing another path", filepath.ToSlash(path))
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("requested path is not a directory")
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].IsDir() != entries[j].IsDir() {
			return entries[i].IsDir()
		}
		return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name())
	})
	items := make([]localFileEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Name() == ".git" && entry.IsDir() {
			continue
		}
		item, itemErr := localFileEntryFromDirEntry(rel, entry)
		if itemErr == nil {
			items = append(items, item)
		}
	}
	return map[string]any{"path": rel, "entries": items}, nil
}

func nativeWriteFile(project string, input map[string]any) (any, []sessionChangeView, error) {
	path, err := nativeRequiredString(input, "path", false)
	if err != nil {
		return nil, nil, err
	}
	content, err := nativeRequiredString(input, "content", true)
	if err != nil {
		return nil, nil, err
	}
	expected, err := nativeOptionalString(input, "expectedSha256")
	if err != nil {
		return nil, nil, err
	}
	if int64(len([]byte(content))) > maxLocalWriteBytes {
		return nil, nil, fmt.Errorf("file is too large to write (max %d MiB)", maxLocalWriteBytes/(1024*1024))
	}
	target, rel, err := resolveProjectMutationPath(project, path)
	if err != nil {
		return nil, nil, err
	}

	workspaceFileMutationMu.Lock()
	defer workspaceFileMutationMu.Unlock()

	var previous []byte
	mode := os.FileMode(0o600)
	info, statErr := os.Lstat(target)
	exists := statErr == nil
	switch {
	case exists:
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, nil, errors.New("requested path is not an editable regular file")
		}
		mode = info.Mode().Perm()
		previous, err = os.ReadFile(target)
		if err != nil {
			return nil, nil, err
		}
	case errors.Is(statErr, os.ErrNotExist):
	default:
		return nil, nil, statErr
	}
	if err := validateWorkspaceExpectedSHA(expected, previous, exists, true); err != nil {
		return nil, nil, err
	}
	if err := atomicWorkspaceFileWrite(target, []byte(content), mode, !exists, expected); err != nil {
		return nil, nil, err
	}
	change := nativeFileChange(rel, string(previous), content)
	return map[string]any{
		"path": rel,
		"sha256": fileSHA256([]byte(content)),
		"bytes": len([]byte(content)),
		"created": !exists,
	}, []sessionChangeView{change}, nil
}

func nativeEditFile(project string, input map[string]any) (any, []sessionChangeView, error) {
	path, err := nativeRequiredString(input, "path", false)
	if err != nil {
		return nil, nil, err
	}
	oldText, err := nativeRequiredString(input, "old", false)
	if err != nil {
		return nil, nil, err
	}
	newText, err := nativeRequiredString(input, "new", true)
	if err != nil {
		return nil, nil, err
	}
	expected, err := nativeRequiredString(input, "expectedSha256", false)
	if err != nil {
		return nil, nil, err
	}
	replaceAll, err := nativeOptionalBool(input, "all")
	if err != nil {
		return nil, nil, err
	}
	target, rel, err := resolveProjectMutationPath(project, path)
	if err != nil {
		return nil, nil, err
	}

	workspaceFileMutationMu.Lock()
	defer workspaceFileMutationMu.Unlock()

	info, err := os.Lstat(target)
	if err != nil {
		return nil, nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, nil, errors.New("requested path is not an editable regular file")
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return nil, nil, err
	}
	if err := validateWorkspaceExpectedSHA(expected, data, true, true); err != nil {
		return nil, nil, err
	}
	original := string(data)
	count := strings.Count(original, oldText)
	if count == 0 {
		return nil, nil, errors.New("edit target text was not found")
	}
	if !replaceAll && count != 1 {
		return nil, nil, fmt.Errorf("edit target occurs %d times; provide a unique target or set all=true", count)
	}
	limit := 1
	if replaceAll {
		limit = -1
	}
	updated := strings.Replace(original, oldText, newText, limit)
	if int64(len([]byte(updated))) > maxLocalWriteBytes {
		return nil, nil, fmt.Errorf("edited file is too large (max %d MiB)", maxLocalWriteBytes/(1024*1024))
	}
	if err := atomicWorkspaceFileWrite(target, []byte(updated), info.Mode().Perm(), false, expected); err != nil {
		return nil, nil, err
	}
	change := nativeFileChange(rel, original, updated)
	return map[string]any{
		"path": rel,
		"replacements": func() int { if replaceAll { return count }; return 1 }(),
		"sha256": fileSHA256([]byte(updated)),
	}, []sessionChangeView{change}, nil
}

// nativeDeleteFile deletes only a single regular file with a revision
// read by the agent, never directories/symlinks. This is deliberately not a
// general recursive delete or shell command bypass.
func nativeDeleteFile(project string, input map[string]any) (any, []sessionChangeView, error) {
	path, err := nativeRequiredString(input, "path", false)
	if err != nil { return nil, nil, err }
	expected, err := nativeRequiredString(input, "expectedSha256", false)
	if err != nil { return nil, nil, err }
	target, rel, err := resolveProjectMutationPath(project, path)
	if err != nil { return nil, nil, err }
	workspaceFileMutationMu.Lock()
	defer workspaceFileMutationMu.Unlock()
	info, err := os.Lstat(target)
	if err != nil { return nil, nil, err }
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, nil, errors.New("files.delete only supports regular files; directories and symlinks cannot be deleted by Agent")
	}
	previous, err := os.ReadFile(target)
	if err != nil { return nil, nil, err }
	if err := validateWorkspaceExpectedSHA(expected, previous, true, true); err != nil { return nil, nil, err }
	// Protect from stale edits or deletion of a different file between Lstat
	// and removal where possible; the path remains project-boundary checked.
	if err := os.Remove(target); err != nil { return nil, nil, err }
	return map[string]any{"path": rel, "deleted": true}, []sessionChangeView{nativeFileChange(rel, string(previous), "")}, nil
}

func nativeSearchContent(ctx context.Context, project string, input map[string]any) (any, error) {
	query, err := nativeRequiredString(input, "query", false)
	if err != nil {
		return nil, err
	}
	include, err := nativeOptionalString(input, "include")
	if err != nil {
		return nil, err
	}
	exclude, err := nativeOptionalString(input, "exclude")
	if err != nil {
		return nil, err
	}
	caseSensitive, err := nativeOptionalBool(input, "caseSensitive")
	if err != nil {
		return nil, err
	}
	limit, err := nativeOptionalInt(input, "limit", maxProjectSearchResults)
	if err != nil {
		return nil, err
	}
	if limit < 1 || limit > maxProjectSearchResults {
		return nil, fmt.Errorf("limit must be between 1 and %d", maxProjectSearchResults)
	}
	return searchProject(ctx, project, projectSearchOptions{
		Query: query,
		CaseSensitive: caseSensitive,
		Includes: splitSearchPatterns(include),
		Excludes: splitSearchPatterns(exclude),
		Limit: limit,
	})
}

func (e *nativeToolExecutor) nativeRunCommand(ctx context.Context, project string, input map[string]any) (any, error) {
	command, err := nativeRequiredString(input, "command", false)
	if err != nil {
		return nil, err
	}
	seconds, err := nativeOptionalInt(input, "timeoutSeconds", int(nativeToolDefaultTimeout/time.Second))
	if err != nil {
		return nil, err
	}
	if seconds < 1 || time.Duration(seconds)*time.Second > nativeToolMaxTimeout {
		return nil, fmt.Errorf("timeoutSeconds must be between 1 and %d", int(nativeToolMaxTimeout/time.Second))
	}
	commandCtx, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
	defer cancel()
	snapshot, runErr := e.processes.run(commandCtx, command, project)
	output := map[string]any{
		"command": snapshot.Command,
		"cwd": snapshot.CWD,
		"output": snapshot.Output,
		"running": snapshot.Running,
	}
	if snapshot.ExitCode != nil {
		output["exitCode"] = *snapshot.ExitCode
	}
	return output, runErr
}

func nativeLineCount(text string) int {
	if text == "" {
		return 0
	}
	return strings.Count(text, "\n") + 1
}

func nativeFileChange(path, before, after string) sessionChangeView {
	return sessionChangeView{
		File:      filepath.ToSlash(path),
		Additions: nativeLineCount(after),
		Deletions: nativeLineCount(before),
	}
}
