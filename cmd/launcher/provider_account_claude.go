package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	claudeAccountProviderID        = "claude"
	claudeAccountRuntimeProviderID = "claude-account"
	claudeAccountProviderProtocol  = "claude-code-account"
	claudeAccountBaseURL           = "https://claude.ai"
)

type claudeCodeConfig struct {
	Executable       string `json:"executable,omitempty"`
	Email            string `json:"email,omitempty"`
	OrganizationName string `json:"organizationName,omitempty"`
	SubscriptionType string `json:"subscriptionType,omitempty"`
}

func claudeCodeConfigPath() string {
	return filepath.Join(tlStudioStateDirectory(), "claude-code.json")
}

func claudeConfigDirectory() string {
	return filepath.Join(tlStudioStateDirectory(), "claude-code")
}

func loadClaudeCodeConfig() (claudeCodeConfig, error) {
	data, err := os.ReadFile(claudeCodeConfigPath())
	if errors.Is(err, os.ErrNotExist) {
		return claudeCodeConfig{}, nil
	}
	if err != nil {
		return claudeCodeConfig{}, err
	}
	var config claudeCodeConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return claudeCodeConfig{}, fmt.Errorf("decode Claude Code setup: %w", err)
	}
	config.Executable = strings.TrimSpace(config.Executable)
	config.Email = strings.TrimSpace(config.Email)
	config.OrganizationName = strings.TrimSpace(config.OrganizationName)
	config.SubscriptionType = strings.TrimSpace(config.SubscriptionType)
	return config, nil
}

func saveClaudeCodeConfig(config claudeCodeConfig) error {
	path := claudeCodeConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	config.Executable = strings.TrimSpace(config.Executable)
	config.Email = strings.TrimSpace(config.Email)
	config.OrganizationName = strings.TrimSpace(config.OrganizationName)
	config.SubscriptionType = strings.TrimSpace(config.SubscriptionType)
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), "claude-code-*.tmp")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
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
	return os.Rename(tempName, path)
}

type claudeCommand struct {
	Executable string
	PrefixArgs []string
	Source     string
}

func (c claudeCommand) valid() bool {
	return strings.TrimSpace(c.Executable) != ""
}

func claudeWindowsNodeBackedCommand(path, source string) (claudeCommand, bool) {
	if runtime.GOOS != "windows" {
		return claudeCommand{}, false
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".cmd" && ext != ".bat" {
		return claudeCommand{}, false
	}
	if strings.ToLower(filepath.Base(path)) != "npx.cmd" && strings.ToLower(filepath.Base(path)) != "npx.bat" {
		return claudeCommand{}, false
	}
	node, ok := resolveExecutableCandidate("node")
	if !ok {
		return claudeCommand{}, false
	}
	script := filepath.Join(filepath.Dir(path), "node_modules", "npm", "bin", "npx-cli.js")
	if info, err := os.Stat(script); err == nil && !info.IsDir() {
		return claudeCommand{
			Executable: node,
			PrefixArgs: []string{script, "--yes", "@anthropic-ai/claude-code@2.1.300"},
			Source:     source + "-node",
		}, true
	}
	return claudeCommand{}, false
}

func claudeProcess(ctx context.Context, command claudeCommand, args ...string) *exec.Cmd {
	allArgs := append(append([]string(nil), command.PrefixArgs...), args...)
	executable := strings.TrimSpace(command.Executable)
	if runtime.GOOS == "windows" {
		ext := strings.ToLower(filepath.Ext(executable))
		if ext == ".cmd" || ext == ".bat" {
			return exec.CommandContext(ctx, "cmd.exe", "/d", "/s", "/c", windowsBatchCommandLine(executable, allArgs))
		}
	}
	return exec.CommandContext(ctx, executable, allArgs...)
}

func claudeCredentialSafeEnvironment() ([]string, error) {
	configDir := claudeConfigDirectory()
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return nil, fmt.Errorf("create isolated Claude config directory: %w", err)
	}
	blocked := map[string]bool{
		"ANTHROPIC_API_KEY":       true,
		"ANTHROPIC_AUTH_TOKEN":    true,
		"ANTHROPIC_BASE_URL":      true,
		"ANTHROPIC_PROFILE":       true,
		"CLAUDE_CODE_OAUTH_TOKEN": true,
		"CLAUDE_CODE_USE_BEDROCK": true,
		"CLAUDE_CODE_USE_VERTEX":  true,
		"CLAUDE_CODE_USE_FOUNDRY": true,
	}
	env := make([]string, 0, len(os.Environ())+2)
	for _, entry := range os.Environ() {
		key := entry
		if index := strings.IndexByte(entry, '='); index >= 0 {
			key = entry[:index]
		}
		if blocked[strings.ToUpper(strings.TrimSpace(key))] {
			continue
		}
		env = append(env, entry)
	}
	env = append(env,
		"CLAUDE_CONFIG_DIR="+configDir,
		"NO_COLOR=1",
	)
	return env, nil
}

func prepareClaudeCommand(cmd *exec.Cmd) error {
	if cmd == nil {
		return errors.New("Claude Code process is unavailable")
	}
	env, err := claudeCredentialSafeEnvironment()
	if err != nil {
		return err
	}
	cmd.Env = env
	return nil
}

type claudeAuthStatusPayload struct {
	LoggedIn         bool   `json:"loggedIn"`
	AuthMethod       string `json:"authMethod,omitempty"`
	APIProvider      string `json:"apiProvider,omitempty"`
	Email            string `json:"email,omitempty"`
	OrgID            string `json:"orgId,omitempty"`
	OrgName          string `json:"orgName,omitempty"`
	SubscriptionType string `json:"subscriptionType,omitempty"`
}

type claudeLoginTransaction struct {
	LoginID   string
	Command   *exec.Cmd
	Output    *boundedTextBuffer
	ExpiresAt time.Time
	Finished  bool
	Err       string
	Synced    bool
}

type claudeAccountAdapter struct {
	state   *appState
	manager *providerManager

	mu     sync.Mutex
	logins map[string]*claudeLoginTransaction
}

func newClaudeAccountAdapter(state *appState, manager *providerManager) *claudeAccountAdapter {
	return &claudeAccountAdapter{
		state:   state,
		manager: manager,
		logins:  map[string]*claudeLoginTransaction{},
	}
}

func (a *claudeAccountAdapter) ID() string                { return claudeAccountProviderID }
func (a *claudeAccountAdapter) RuntimeProviderID() string { return claudeAccountRuntimeProviderID }
func (a *claudeAccountAdapter) Protocol() string          { return claudeAccountProviderProtocol }

func (a *claudeAccountAdapter) resolveCommand() (claudeCommand, error) {
	if override := strings.TrimSpace(os.Getenv("TL_STUDIO_CLAUDE_EXECUTABLE")); override != "" {
		if path, ok := resolveExecutableCandidate(override); ok {
			return claudeCommand{Executable: path, Source: "environment"}, nil
		}
		return claudeCommand{}, errors.New("TL_STUDIO_CLAUDE_EXECUTABLE does not point to an executable Claude Code CLI")
	}
	config, err := loadClaudeCodeConfig()
	if err != nil {
		return claudeCommand{}, err
	}
	if config.Executable != "" {
		if path, ok := resolveExecutableCandidate(config.Executable); ok {
			return claudeCommand{Executable: path, Source: "configured"}, nil
		}
		return claudeCommand{}, errors.New("configured Claude Code executable was not found")
	}
	if path, ok := resolveExecutableCandidate("claude"); ok {
		return claudeCommand{Executable: path, Source: "path"}, nil
	}
	if path, ok := resolveExecutableCandidate("npx"); ok {
		if command, resolved := claudeWindowsNodeBackedCommand(path, "npx"); resolved {
			return command, nil
		}
		return claudeCommand{
			Executable: path,
			PrefixArgs: []string{"--yes", "@anthropic-ai/claude-code@2.1.300"},
			Source:     "npx",
		}, nil
	}
	return claudeCommand{}, errors.New("official Claude Code CLI is not installed; install Claude Code or configure its executable path")
}

func (a *claudeAccountAdapter) setupSummary() *providerAccountSetupSummary {
	_, commandErr := a.resolveCommand()
	return &providerAccountSetupSummary{
		Configurable: true,
		Configured:   commandErr == nil,
		Label:        "Claude Code setup",
	}
}

func (a *claudeAccountAdapter) baseStatus() providerAccountStatus {
	_, commandErr := a.resolveCommand()
	status := providerAccountStatus{
		ID:           claudeAccountProviderID,
		Name:         "Claude Code (Pro/Max)",
		Description:  "Connect a Claude Pro, Max, Team, or Enterprise account through Anthropic's official Claude Code browser login.",
		Available:    commandErr == nil,
		State:        providerAccountDisconnected,
		AuthModes:    []string{"claudeai"},
		Capabilities: []string{"models", "inference"},
		BillingNote:  "Account-backed usage follows the connected Claude subscription. Anthropic API-key billing remains a separate optional connection.",
		Setup:        a.setupSummary(),
	}
	if commandErr != nil {
		status.Error = commandErr.Error()
	}
	return status
}

func (a *claudeAccountAdapter) authStatus(ctx context.Context, command claudeCommand) (claudeAuthStatusPayload, bool, error) {
	if !command.valid() {
		return claudeAuthStatusPayload{}, false, errors.New("official Claude Code CLI is not configured")
	}
	cmd := claudeProcess(ctx, command, "auth", "status")
	if err := prepareClaudeCommand(cmd); err != nil {
		return claudeAuthStatusPayload{}, false, err
	}
	var stdout boundedTextBuffer
	stdout.max = 64 << 10
	var stderr boundedTextBuffer
	stderr.max = 8 << 10
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	var payload claudeAuthStatusPayload
	if data := strings.TrimSpace(stdout.String()); data != "" {
		_ = json.Unmarshal([]byte(data), &payload)
	}
	if err == nil {
		if !payload.LoggedIn {
			payload.LoggedIn = true
		}
		return payload, true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return payload, false, nil
	}
	detail := stderr.String()
	if detail == "" {
		detail = stdout.String()
	}
	if detail != "" {
		return payload, false, fmt.Errorf("Claude auth status failed: %w — %s", err, detail)
	}
	return payload, false, fmt.Errorf("Claude auth status failed: %w", err)
}

func (a *claudeAccountAdapter) removeManagedProvider() {
	if a == nil || a.manager == nil || a.manager.store == nil {
		return
	}
	if provider, found, err := a.manager.store.get(claudeAccountRuntimeProviderID); err == nil && found && provider.ManagedBy == "account" {
		_ = a.manager.store.remove(claudeAccountRuntimeProviderID)
	}
}

func claudeSubscriptionModels() []tlProviderModel {
	return []tlProviderModel{
		{ID: "haiku", Name: "Claude Haiku", ToolCall: true, Reasoning: true},
		{ID: "opus", Name: "Claude Opus", ToolCall: true, Reasoning: true},
		{ID: "sonnet", Name: "Claude Sonnet", ToolCall: true, Reasoning: true},
	}
}

func (a *claudeAccountAdapter) syncProvider(payload claudeAuthStatusPayload) ([]string, error) {
	if a == nil || a.manager == nil || a.manager.store == nil {
		return nil, errors.New("TL Studio Provider Registry is unavailable")
	}
	models := claudeSubscriptionModels()
	definition := tlProviderDefinition{
		ID:        claudeAccountRuntimeProviderID,
		Name:      "Claude / Account",
		Protocol:  claudeAccountProviderProtocol,
		BaseURL:   claudeAccountBaseURL,
		ManagedBy: "account",
		Models:    models,
	}
	if existing, found, err := a.manager.store.get(claudeAccountRuntimeProviderID); err != nil {
		return nil, err
	} else if found && existing.ManagedBy != "account" {
		return nil, errors.New("provider ID claude-account is already used by a different provider")
	}
	if err := a.manager.store.put(definition); err != nil {
		return nil, err
	}
	config, err := loadClaudeCodeConfig()
	if err != nil {
		return nil, err
	}
	config.Email = strings.TrimSpace(payload.Email)
	config.OrganizationName = strings.TrimSpace(payload.OrgName)
	config.SubscriptionType = strings.TrimSpace(payload.SubscriptionType)
	if err := saveClaudeCodeConfig(config); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	return ids, nil
}

func (a *claudeAccountAdapter) Status(ctx context.Context, _ string) (providerAccountStatus, error) {
	status := a.baseStatus()
	if !status.Available || a.manager == nil || a.manager.store == nil {
		return status, nil
	}
	provider, found, err := a.manager.store.get(claudeAccountRuntimeProviderID)
	if err != nil {
		return providerAccountStatus{}, err
	}
	if !found || provider.ManagedBy != "account" || provider.Protocol != claudeAccountProviderProtocol {
		return status, nil
	}
	command, err := a.resolveCommand()
	if err != nil {
		return status, nil
	}
	checkCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	payload, connected, err := a.authStatus(checkCtx, command)
	if err != nil {
		status.State = providerAccountErrorState
		status.Error = err.Error()
		return status, nil
	}
	if !connected {
		a.removeManagedProvider()
		return status, nil
	}
	ids, err := a.syncProvider(payload)
	if err != nil {
		return providerAccountStatus{}, err
	}
	config, _ := loadClaudeCodeConfig()
	status.State = providerAccountConnected
	status.Connected = true
	status.AccountLabel = strings.TrimSpace(payload.Email)
	if status.AccountLabel == "" {
		status.AccountLabel = config.Email
	}
	status.OrganizationID = strings.TrimSpace(payload.OrgID)
	status.AccountType = "Claude"
	subscription := strings.TrimSpace(payload.SubscriptionType)
	if subscription == "" {
		subscription = config.SubscriptionType
	}
	if subscription != "" {
		status.AccountType = "Claude " + strings.ToUpper(subscription[:1]) + subscription[1:]
	}
	status.Models = ids
	return status, nil
}

func (a *claudeAccountAdapter) Setup(context.Context, string) (providerAccountSetup, error) {
	config, err := loadClaudeCodeConfig()
	if err != nil {
		return providerAccountSetup{}, err
	}
	override := strings.TrimSpace(os.Getenv("TL_STUDIO_CLAUDE_EXECUTABLE"))
	readOnly := override != ""
	value := config.Executable
	if readOnly {
		value = override
	}
	return providerAccountSetup{
		Title:       "Claude Code setup",
		Description: "TL Studio uses Anthropic's official Claude Code CLI for browser sign-in and subscription-backed model access. If claude is already on PATH, or npx is available, no custom path is required.",
		Fields: []providerAccountSetupField{{
			ID:          "claudeExecutable",
			Label:       "Claude Code executable",
			Description: "Optional path or command for the official Claude Code CLI. Leave empty to auto-detect claude or use npx @anthropic-ai/claude-code.",
			Placeholder: "claude",
			Value:       value,
			ReadOnly:    readOnly,
		}},
	}, nil
}

func (a *claudeAccountAdapter) Configure(ctx context.Context, directory string, values map[string]string) (providerAccountStatus, error) {
	if strings.TrimSpace(os.Getenv("TL_STUDIO_CLAUDE_EXECUTABLE")) != "" {
		return providerAccountStatus{}, errors.New("Claude Code executable is controlled by TL_STUDIO_CLAUDE_EXECUTABLE")
	}
	config, err := loadClaudeCodeConfig()
	if err != nil {
		return providerAccountStatus{}, err
	}
	config.Executable = strings.TrimSpace(values["claudeExecutable"])
	if config.Executable != "" {
		if _, ok := resolveExecutableCandidate(config.Executable); !ok {
			return providerAccountStatus{}, errors.New("Claude Code executable was not found")
		}
	}
	if err := saveClaudeCodeConfig(config); err != nil {
		return providerAccountStatus{}, err
	}
	status, err := a.Status(ctx, directory)
	if err != nil {
		return providerAccountStatus{}, err
	}
	if !status.Available {
		return status, errors.New(status.Error)
	}
	return status, nil
}

func (a *claudeAccountAdapter) BeginLogin(context.Context, string) (providerAccountLogin, error) {
	command, err := a.resolveCommand()
	if err != nil {
		return providerAccountLogin{}, err
	}
	loginID, err := randomBase64URL(18)
	if err != nil {
		return providerAccountLogin{}, err
	}
	expiresAt := time.Now().UTC().Add(10 * time.Minute)
	output := newBoundedTextBuffer(16 << 10)
	cmd := claudeProcess(context.Background(), command, "auth", "login")
	if err := prepareClaudeCommand(cmd); err != nil {
		return providerAccountLogin{}, err
	}
	cmd.Stdout = output
	cmd.Stderr = output
	transaction := &claudeLoginTransaction{
		LoginID:   loginID,
		Command:   cmd,
		Output:    output,
		ExpiresAt: expiresAt,
	}
	if err := cmd.Start(); err != nil {
		return providerAccountLogin{}, fmt.Errorf("start official Claude Code login: %w", err)
	}
	a.mu.Lock()
	a.logins[loginID] = transaction
	a.mu.Unlock()
	go func() {
		err := cmd.Wait()
		a.mu.Lock()
		if current := a.logins[loginID]; current == transaction {
			current.Finished = true
			if err != nil {
				current.Err = err.Error()
			}
		}
		a.mu.Unlock()
	}()
	return providerAccountLogin{
		LoginID:             loginID,
		Flow:                "claude_browser",
		Instructions:        "Claude Code opened the Anthropic sign-in page in your browser. Complete the Claude.ai authorization there; TL Studio will detect the connected account automatically.",
		ExpiresAt:           expiresAt.Format(time.RFC3339),
		PollIntervalSeconds: 2,
	}, nil
}

func (a *claudeAccountAdapter) CompleteLogin(context.Context, string, providerAccountCallback) error {
	return errors.New("Claude callback is handled by Anthropic's official Claude Code CLI")
}

func (a *claudeAccountAdapter) PollLogin(ctx context.Context, directory, loginID string) (providerAccountStatus, error) {
	loginID = strings.TrimSpace(loginID)
	a.mu.Lock()
	transaction := a.logins[loginID]
	if transaction == nil {
		a.mu.Unlock()
		return providerAccountStatus{}, errors.New("provider login transaction not found")
	}
	finished, loginErr, expiresAt, output, cmd := transaction.Finished, transaction.Err, transaction.ExpiresAt, transaction.Output, transaction.Command
	a.mu.Unlock()

	if time.Now().UTC().After(expiresAt) {
		if cmd != nil && cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		status := a.baseStatus()
		status.State = providerAccountExpired
		return status, nil
	}
	command, err := a.resolveCommand()
	if err != nil {
		return providerAccountStatus{}, err
	}
	checkCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	payload, connected, statusErr := a.authStatus(checkCtx, command)
	cancel()
	if statusErr != nil {
		return providerAccountStatus{}, statusErr
	}
	if connected {
		if _, err := a.syncProvider(payload); err != nil {
			return providerAccountStatus{}, err
		}
		if cmd != nil && cmd.Process != nil && !finished {
			_ = cmd.Process.Kill()
		}
		a.mu.Lock()
		delete(a.logins, loginID)
		a.mu.Unlock()
		return a.Status(ctx, directory)
	}
	if finished {
		status := a.baseStatus()
		status.State = providerAccountErrorState
		detail := strings.TrimSpace(output.String())
		if detail == "" {
			detail = strings.TrimSpace(loginErr)
		}
		if detail == "" {
			detail = "Claude authorization did not complete"
		}
		status.Error = detail
		return status, nil
	}
	status := a.baseStatus()
	status.State = providerAccountConnecting
	return status, nil
}

func (a *claudeAccountAdapter) CancelLogin(_ context.Context, _ string, loginID string) error {
	loginID = strings.TrimSpace(loginID)
	a.mu.Lock()
	transaction := a.logins[loginID]
	delete(a.logins, loginID)
	a.mu.Unlock()
	if transaction != nil && transaction.Command != nil && transaction.Command.Process != nil {
		_ = transaction.Command.Process.Kill()
	}
	return nil
}

func (a *claudeAccountAdapter) Refresh(ctx context.Context, directory string) (providerAccountStatus, error) {
	return a.Status(ctx, directory)
}

func (a *claudeAccountAdapter) ResolveCredential(context.Context, string) (string, error) {
	if a == nil || a.manager == nil || a.manager.store == nil {
		return "", errCredentialNotFound
	}
	provider, found, err := a.manager.store.get(claudeAccountRuntimeProviderID)
	if err != nil {
		return "", err
	}
	if !found || provider.ManagedBy != "account" || provider.Protocol != claudeAccountProviderProtocol {
		return "", errCredentialNotFound
	}
	if _, err := a.resolveCommand(); err != nil {
		return "", errCredentialNotFound
	}
	return "official-claude-account", nil
}

func (a *claudeAccountAdapter) DiscoverModels(ctx context.Context, directory string) ([]string, error) {
	status, err := a.Status(ctx, directory)
	if err != nil {
		return nil, err
	}
	if !status.Connected {
		return nil, errors.New("Claude account is not signed in")
	}
	return append([]string(nil), status.Models...), nil
}

func (a *claudeAccountAdapter) Disconnect(ctx context.Context, _ string) error {
	command, err := a.resolveCommand()
	if err == nil {
		logoutCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		cmd := claudeProcess(logoutCtx, command, "auth", "logout")
		if prepareErr := prepareClaudeCommand(cmd); prepareErr == nil {
			var output boundedTextBuffer
			output.max = 8 << 10
			cmd.Stdout = &output
			cmd.Stderr = &output
			_ = cmd.Run()
		}
		cancel()
	}
	a.mu.Lock()
	for id, transaction := range a.logins {
		if transaction != nil && transaction.Command != nil && transaction.Command.Process != nil {
			_ = transaction.Command.Process.Kill()
		}
		delete(a.logins, id)
	}
	a.mu.Unlock()
	a.removeManagedProvider()
	config, configErr := loadClaudeCodeConfig()
	if configErr == nil {
		config.Email = ""
		config.OrganizationName = ""
		config.SubscriptionType = ""
		configErr = saveClaudeCodeConfig(config)
	}
	return configErr
}
