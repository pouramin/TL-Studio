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
	chatGPTAccountProviderID = "chatgpt"
	chatGPTAccountBaseURL    = "https://chatgpt.com"
)

type chatGPTCodexConfig struct {
	Executable string `json:"executable,omitempty"`
	Email      string `json:"email,omitempty"`
	PlanType   string `json:"planType,omitempty"`
}

func chatGPTCodexConfigPath() string {
	return filepath.Join(tlStudioStateDirectory(), "chatgpt-codex.json")
}

func loadChatGPTCodexConfig() (chatGPTCodexConfig, error) {
	data, err := os.ReadFile(chatGPTCodexConfigPath())
	if errors.Is(err, os.ErrNotExist) {
		return chatGPTCodexConfig{}, nil
	}
	if err != nil {
		return chatGPTCodexConfig{}, err
	}
	var config chatGPTCodexConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return chatGPTCodexConfig{}, fmt.Errorf("decode ChatGPT/Codex setup: %w", err)
	}
	config.Executable = strings.TrimSpace(config.Executable)
	config.Email = strings.TrimSpace(config.Email)
	config.PlanType = strings.TrimSpace(config.PlanType)
	return config, nil
}

func saveChatGPTCodexConfig(config chatGPTCodexConfig) error {
	path := chatGPTCodexConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	config.Executable = strings.TrimSpace(config.Executable)
	config.Email = strings.TrimSpace(config.Email)
	config.PlanType = strings.TrimSpace(config.PlanType)
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), "chatgpt-codex-*.tmp")
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

func resolveExecutableCandidate(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	if strings.ContainsAny(value, `/\`) || filepath.IsAbs(value) {
		info, err := os.Stat(value)
		return value, err == nil && !info.IsDir()
	}
	path, err := exec.LookPath(value)
	return path, err == nil && strings.TrimSpace(path) != ""
}

type chatGPTLoginTransaction struct {
	LoginID      string
	CodexLoginID string
	AuthURL      string
	Server       *codexAppServer
	ExpiresAt    time.Time
	Completed    bool
	Synced       bool
	Err          string
}

type chatGPTAccountAdapter struct {
	state   *appState
	manager *providerManager

	mu     sync.Mutex
	logins map[string]*chatGPTLoginTransaction

	bridgeMu      sync.Mutex
	bridgeServer  *codexAppServer
	bridgeCommand codexCommand
}

func newChatGPTAccountAdapter(state *appState, manager *providerManager) *chatGPTAccountAdapter {
	return &chatGPTAccountAdapter{
		state:   state,
		manager: manager,
		logins:  map[string]*chatGPTLoginTransaction{},
	}
}

func (a *chatGPTAccountAdapter) ID() string       { return chatGPTAccountProviderID }
func (a *chatGPTAccountAdapter) Protocol() string { return codexChatGPTProviderProtocol }
func (a *chatGPTAccountAdapter) CompleteModelTurn(ctx context.Context, request nativeModelRequest, onTextDelta func(string)) (nativeModelResponse, error) {
	return a.completeModelTurn(ctx, request, onTextDelta)
}

func sameCodexCommand(left, right codexCommand) bool {
	if strings.TrimSpace(left.Executable) != strings.TrimSpace(right.Executable) ||
		strings.TrimSpace(left.Source) != strings.TrimSpace(right.Source) ||
		len(left.PrefixArgs) != len(right.PrefixArgs) {
		return false
	}
	for index := range left.PrefixArgs {
		if left.PrefixArgs[index] != right.PrefixArgs[index] {
			return false
		}
	}
	return true
}

func (a *chatGPTAccountAdapter) resetBridgeServerLocked() {
	if a == nil {
		return
	}
	if a.bridgeServer != nil {
		a.bridgeServer.Close()
	}
	a.bridgeServer = nil
	a.bridgeCommand = codexCommand{}
}

func (a *chatGPTAccountAdapter) resetBridgeServer() {
	if a == nil {
		return
	}
	a.bridgeMu.Lock()
	defer a.bridgeMu.Unlock()
	a.resetBridgeServerLocked()
}

func (a *chatGPTAccountAdapter) bridgeServerLocked(command codexCommand) (*codexAppServer, error) {
	if a == nil {
		return nil, errors.New("ChatGPT account transport is unavailable")
	}
	if a.bridgeServer != nil {
		if a.bridgeServer.isClosed() || !sameCodexCommand(a.bridgeCommand, command) {
			a.resetBridgeServerLocked()
		}
	}
	if a.bridgeServer == nil {
		server, err := startCodexAppServer(command)
		if err != nil {
			return nil, err
		}
		a.bridgeServer = server
		a.bridgeCommand = command
	}
	return a.bridgeServer, nil
}

func windowsNodeBackedCommand(path, source string) (codexCommand, bool) {
	if runtime.GOOS != "windows" {
		return codexCommand{}, false
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".cmd" && ext != ".bat" {
		return codexCommand{}, false
	}
	node, ok := resolveExecutableCandidate("node")
	if !ok {
		return codexCommand{}, false
	}
	root := filepath.Dir(path)
	switch strings.ToLower(filepath.Base(path)) {
	case "npx.cmd", "npx.bat":
		script := filepath.Join(root, "node_modules", "npm", "bin", "npx-cli.js")
		if info, err := os.Stat(script); err == nil && !info.IsDir() {
			return codexCommand{
				Executable: node,
				PrefixArgs: []string{script, "--yes", "@openai/codex@0.162.1"},
				Source:     source + "-node",
			}, true
		}
	case "codex.cmd", "codex.bat":
		script := filepath.Join(root, "node_modules", "@openai", "codex", "bin", "codex.js")
		if info, err := os.Stat(script); err == nil && !info.IsDir() {
			return codexCommand{
				Executable: node,
				PrefixArgs: []string{script},
				Source:     source + "-node",
			}, true
		}
	}
	return codexCommand{}, false
}

func codexCommandForExecutable(path, source string) codexCommand {
	if command, ok := windowsNodeBackedCommand(path, source); ok {
		return command
	}
	return codexCommand{Executable: path, Source: source}
}

func (a *chatGPTAccountAdapter) resolveCommand() (codexCommand, error) {
	if override := strings.TrimSpace(os.Getenv("TL_STUDIO_CODEX_EXECUTABLE")); override != "" {
		if path, ok := resolveExecutableCandidate(override); ok {
			return codexCommandForExecutable(path, "environment"), nil
		}
		return codexCommand{}, errors.New("TL_STUDIO_CODEX_EXECUTABLE does not point to an executable Codex CLI")
	}
	config, err := loadChatGPTCodexConfig()
	if err != nil {
		return codexCommand{}, err
	}
	if config.Executable != "" {
		if path, ok := resolveExecutableCandidate(config.Executable); ok {
			return codexCommandForExecutable(path, "configured"), nil
		}
		return codexCommand{}, errors.New("configured Codex executable was not found")
	}
	if path, ok := resolveExecutableCandidate("codex"); ok {
		return codexCommandForExecutable(path, "path"), nil
	}
	if path, ok := resolveExecutableCandidate("npx"); ok {
		if command, resolved := windowsNodeBackedCommand(path, "npx"); resolved {
			return command, nil
		}
		return codexCommand{
			Executable: path,
			PrefixArgs: []string{"--yes", "@openai/codex@0.162.1"},
			Source:     "npx",
		}, nil
	}
	return codexCommand{}, errors.New("official Codex CLI is not installed; install @openai/codex or configure its executable path")
}

func (a *chatGPTAccountAdapter) setupSummary() *providerAccountSetupSummary {
	_, commandErr := a.resolveCommand()
	return &providerAccountSetupSummary{
		Configurable: true,
		Configured:   commandErr == nil,
		Label:        "Codex setup",
	}
}

func (a *chatGPTAccountAdapter) baseStatus() providerAccountStatus {
	_, commandErr := a.resolveCommand()
	status := providerAccountStatus{
		ID:           chatGPTAccountProviderID,
		Name:         "ChatGPT / Codex",
		Description:  "Connect a ChatGPT account through OpenAI's official Codex login.",
		Available:    commandErr == nil,
		State:        providerAccountDisconnected,
		AuthModes:    []string{"chatgpt"},
		Capabilities: []string{"models", "inference"},
		BillingNote:  "Usage follows the connected ChatGPT/Codex plan rather than normal OpenAI API-key billing.",
		Setup:        a.setupSummary(),
	}
	if commandErr != nil {
		status.Error = commandErr.Error()
	}
	return status
}

func (a *chatGPTAccountAdapter) Status(context.Context, string) (providerAccountStatus, error) {
	status := a.baseStatus()
	if !status.Available || a.manager == nil || a.manager.store == nil {
		return status, nil
	}
	provider, found, err := a.manager.store.get(chatGPTAccountProviderID)
	if err != nil {
		return providerAccountStatus{}, err
	}
	if !found || provider.ManagedBy != "account" || provider.Protocol != codexChatGPTProviderProtocol {
		return status, nil
	}
	config, err := loadChatGPTCodexConfig()
	if err != nil {
		return providerAccountStatus{}, err
	}
	status.State = providerAccountConnected
	status.Connected = true
	status.AccountLabel = config.Email
	status.AccountType = "ChatGPT"
	if config.PlanType != "" {
		status.AccountType = "ChatGPT " + config.PlanType
	}
	status.Models = make([]string, 0, len(provider.Models))
	for _, model := range provider.Models {
		status.Models = append(status.Models, model.ID)
	}
	return status, nil
}

func (a *chatGPTAccountAdapter) Setup(context.Context, string) (providerAccountSetup, error) {
	config, err := loadChatGPTCodexConfig()
	if err != nil {
		return providerAccountSetup{}, err
	}
	override := strings.TrimSpace(os.Getenv("TL_STUDIO_CODEX_EXECUTABLE"))
	readOnly := override != ""
	value := config.Executable
	if readOnly {
		value = override
	}
	return providerAccountSetup{
		Title:       "ChatGPT / Codex setup",
		Description: "TL Studio uses OpenAI's official Codex CLI for ChatGPT account authorization and plan-backed model access. If Codex is already on PATH, or npx is available, no custom path is required.",
		Fields: []providerAccountSetupField{{
			ID:          "codexExecutable",
			Label:       "Codex executable",
			Description: "Optional path or command for the official Codex CLI. Leave empty to auto-detect codex or use npx @openai/codex.",
			Placeholder: "codex",
			Value:       value,
			ReadOnly:    readOnly,
		}},
	}, nil
}

func (a *chatGPTAccountAdapter) Configure(ctx context.Context, directory string, values map[string]string) (providerAccountStatus, error) {
	if strings.TrimSpace(os.Getenv("TL_STUDIO_CODEX_EXECUTABLE")) != "" {
		return providerAccountStatus{}, errors.New("Codex executable is controlled by TL_STUDIO_CODEX_EXECUTABLE")
	}
	config, err := loadChatGPTCodexConfig()
	if err != nil {
		return providerAccountStatus{}, err
	}
	config.Executable = strings.TrimSpace(values["codexExecutable"])
	if config.Executable != "" {
		if _, ok := resolveExecutableCandidate(config.Executable); !ok {
			return providerAccountStatus{}, errors.New("Codex executable was not found")
		}
	}
	if err := saveChatGPTCodexConfig(config); err != nil {
		return providerAccountStatus{}, err
	}
	a.resetBridgeServer()
	status, err := a.Status(ctx, directory)
	if err != nil {
		return providerAccountStatus{}, err
	}
	if !status.Available {
		return status, errors.New(status.Error)
	}
	return status, nil
}

func (a *chatGPTAccountAdapter) BeginLogin(ctx context.Context, _ string) (providerAccountLogin, error) {
	a.resetBridgeServer()
	command, err := a.resolveCommand()
	if err != nil {
		return providerAccountLogin{}, err
	}
	server, err := startCodexAppServer(command)
	if err != nil {
		return providerAccountLogin{}, err
	}
	var response struct {
		Type    string `json:"type"`
		LoginID string `json:"loginId"`
		AuthURL string `json:"authUrl"`
	}
	loginCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := server.request(loginCtx, "account/login/start", map[string]any{"type": "chatgpt"}, &response); err != nil {
		server.Close()
		return providerAccountLogin{}, err
	}
	response.LoginID = strings.TrimSpace(response.LoginID)
	response.AuthURL = strings.TrimSpace(response.AuthURL)
	if response.Type != "chatgpt" || response.LoginID == "" || response.AuthURL == "" {
		server.Close()
		return providerAccountLogin{}, errors.New("official Codex login returned an invalid ChatGPT challenge")
	}
	localLoginID, err := randomBase64URL(18)
	if err != nil {
		server.Close()
		return providerAccountLogin{}, err
	}
	expiresAt := time.Now().UTC().Add(10 * time.Minute)
	transaction := &chatGPTLoginTransaction{
		LoginID:      localLoginID,
		CodexLoginID: response.LoginID,
		AuthURL:      response.AuthURL,
		Server:       server,
		ExpiresAt:    expiresAt,
	}
	a.mu.Lock()
	a.logins[localLoginID] = transaction
	a.mu.Unlock()
	go a.watchLogin(transaction)

	return providerAccountLogin{
		LoginID:             localLoginID,
		Flow:                "chatgpt",
		AuthorizationURL:    response.AuthURL,
		Instructions:        "Open ChatGPT, sign in, and approve Codex access for TL Studio.",
		ExpiresAt:           expiresAt.Format(time.RFC3339),
		PollIntervalSeconds: 2,
	}, nil
}

func (a *chatGPTAccountAdapter) watchLogin(transaction *chatGPTLoginTransaction) {
	if transaction == nil || transaction.Server == nil {
		return
	}
	for {
		select {
		case notification := <-transaction.Server.notify:
			if notification.Method != "account/login/completed" {
				continue
			}
			var completed struct {
				LoginID string `json:"loginId"`
				Success bool   `json:"success"`
				Error   string `json:"error"`
			}
			if err := json.Unmarshal(notification.Params, &completed); err != nil {
				continue
			}
			if strings.TrimSpace(completed.LoginID) != transaction.CodexLoginID {
				continue
			}
			a.mu.Lock()
			if current := a.logins[transaction.LoginID]; current == transaction {
				current.Completed = completed.Success
				if !completed.Success {
					current.Err = strings.TrimSpace(completed.Error)
					if current.Err == "" {
						current.Err = "ChatGPT authorization failed"
					}
				}
			}
			a.mu.Unlock()
			return
		case <-transaction.Server.done:
			a.mu.Lock()
			if current := a.logins[transaction.LoginID]; current == transaction && !current.Completed && current.Err == "" {
				current.Err = "official Codex login process stopped unexpectedly"
			}
			a.mu.Unlock()
			return
		case <-time.After(time.Until(transaction.ExpiresAt)):
			a.mu.Lock()
			if current := a.logins[transaction.LoginID]; current == transaction && !current.Completed {
				current.Err = "ChatGPT authorization expired"
			}
			a.mu.Unlock()
			transaction.Server.Close()
			return
		}
	}
}

func (a *chatGPTAccountAdapter) CompleteLogin(context.Context, string, providerAccountCallback) error {
	return errors.New("ChatGPT callback is handled by OpenAI's official Codex app-server")
}

func preferredCodexReasoningEffort(item codexModelListItem) string {
	supported := map[string]bool{}
	for _, option := range item.SupportedReasoningEfforts {
		effort := strings.ToLower(strings.TrimSpace(option.ReasoningEffort))
		if effort != "" {
			supported[effort] = true
		}
	}
	for _, effort := range []string{"low", "minimal", "none"} {
		if supported[effort] {
			return effort
		}
	}
	return strings.ToLower(strings.TrimSpace(item.DefaultReasoningEffort))
}

func (a *chatGPTAccountAdapter) syncProvider(ctx context.Context, server *codexAppServer) ([]string, error) {
	if a.manager == nil || a.manager.store == nil {
		return nil, errors.New("TL Studio Provider Registry is unavailable")
	}
	var accountResponse codexAccountReadResponse
	if err := server.request(ctx, "account/read", map[string]any{"refreshToken": false}, &accountResponse); err != nil {
		return nil, err
	}
	if accountResponse.Account == nil || accountResponse.Account.Type != "chatgpt" {
		return nil, errors.New("official Codex is not signed in with a ChatGPT account")
	}
	items, err := codexListModelsWithServer(ctx, server)
	if err != nil {
		return nil, err
	}
	models := make([]tlProviderModel, 0, len(items))
	ids := make([]string, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		id := strings.TrimSpace(item.Model)
		if id == "" {
			id = strings.TrimSpace(item.ID)
		}
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		name := strings.TrimSpace(item.DisplayName)
		if name == "" {
			name = id
		}
		models = append(models, tlProviderModel{
			ID:              id,
			Name:            name,
			ToolCall:        true,
			Reasoning:       len(item.SupportedReasoningEfforts) > 0,
			ReasoningEffort: preferredCodexReasoningEffort(item),
		})
		ids = append(ids, id)
	}
	if len(models) == 0 {
		return nil, errors.New("official Codex returned no selectable ChatGPT models")
	}
	if existing, found, getErr := a.manager.store.get(chatGPTAccountProviderID); getErr != nil {
		return nil, getErr
	} else if found && existing.ManagedBy != "account" {
		return nil, errors.New("provider ID chatgpt is already used by a different provider")
	}
	definition := tlProviderDefinition{
		ID:        chatGPTAccountProviderID,
		Name:      "ChatGPT / Codex",
		Protocol:  codexChatGPTProviderProtocol,
		BaseURL:   chatGPTAccountBaseURL,
		ManagedBy: "account",
		Models:    models,
	}
	if err := a.manager.store.put(definition); err != nil {
		return nil, err
	}
	config, err := loadChatGPTCodexConfig()
	if err != nil {
		return nil, err
	}
	config.Email = strings.TrimSpace(accountResponse.Account.Email)
	config.PlanType = strings.TrimSpace(accountResponse.Account.PlanType)
	if err := saveChatGPTCodexConfig(config); err != nil {
		return nil, err
	}
	return ids, nil
}

func (a *chatGPTAccountAdapter) PollLogin(ctx context.Context, directory, loginID string) (providerAccountStatus, error) {
	loginID = strings.TrimSpace(loginID)
	a.mu.Lock()
	transaction := a.logins[loginID]
	if transaction == nil {
		a.mu.Unlock()
		return providerAccountStatus{}, errors.New("provider login transaction not found")
	}
	completed, synced, loginErr, expiresAt, server := transaction.Completed, transaction.Synced, transaction.Err, transaction.ExpiresAt, transaction.Server
	a.mu.Unlock()

	if loginErr != "" {
		status := a.baseStatus()
		status.State = providerAccountErrorState
		status.Error = loginErr
		return status, nil
	}
	if time.Now().UTC().After(expiresAt) && !completed {
		status := a.baseStatus()
		status.State = providerAccountExpired
		status.Connected = false
		if server != nil {
			server.Close()
		}
		return status, nil
	}
	if completed && !synced {
		syncCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		_, err := a.syncProvider(syncCtx, server)
		cancel()
		if err != nil {
			a.mu.Lock()
			if current := a.logins[loginID]; current != nil {
				current.Err = "ChatGPT model discovery failed"
			}
			a.mu.Unlock()
			if server != nil {
				server.Close()
			}
			return providerAccountStatus{}, err
		}
		a.mu.Lock()
		if current := a.logins[loginID]; current != nil {
			current.Synced = true
		}
		a.mu.Unlock()
		if server != nil {
			a.bridgeMu.Lock()
			if a.bridgeServer == nil || a.bridgeServer.isClosed() {
				a.bridgeServer = server
				a.bridgeCommand = server.command
				server = nil
			}
			a.bridgeMu.Unlock()
			if server != nil {
				server.Close()
			}
		}
	}
	if completed {
		return a.Status(ctx, directory)
	}
	status := a.baseStatus()
	status.State = providerAccountConnecting
	status.Connected = false
	return status, nil
}

func (a *chatGPTAccountAdapter) CancelLogin(ctx context.Context, _ string, loginID string) error {
	loginID = strings.TrimSpace(loginID)
	a.mu.Lock()
	transaction := a.logins[loginID]
	delete(a.logins, loginID)
	a.mu.Unlock()
	if transaction == nil {
		return nil
	}
	if transaction.Server != nil && transaction.CodexLoginID != "" {
		cancelCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_ = transaction.Server.request(cancelCtx, "account/login/cancel", map[string]any{
			"loginId": transaction.CodexLoginID,
		}, nil)
		cancel()
		transaction.Server.Close()
	}
	return nil
}

func (a *chatGPTAccountAdapter) Refresh(ctx context.Context, directory string) (providerAccountStatus, error) {
	a.resetBridgeServer()
	command, err := a.resolveCommand()
	if err != nil {
		return providerAccountStatus{}, err
	}
	server, err := startCodexAppServer(command)
	if err != nil {
		return providerAccountStatus{}, err
	}
	defer server.Close()
	var account codexAccountReadResponse
	if err := server.request(ctx, "account/read", map[string]any{"refreshToken": true}, &account); err != nil {
		return providerAccountStatus{}, err
	}
	if account.Account == nil || account.Account.Type != "chatgpt" {
		return providerAccountStatus{}, errors.New("ChatGPT account needs reauthentication")
	}
	if _, err := a.syncProvider(ctx, server); err != nil {
		return providerAccountStatus{}, err
	}
	return a.Status(ctx, directory)
}

func (a *chatGPTAccountAdapter) ResolveCredential(context.Context, string) (string, error) {
	if a == nil || a.manager == nil || a.manager.store == nil {
		return "", errCredentialNotFound
	}
	provider, found, err := a.manager.store.get(chatGPTAccountProviderID)
	if err != nil {
		return "", err
	}
	if !found || provider.ManagedBy != "account" || provider.Protocol != codexChatGPTProviderProtocol {
		return "", errCredentialNotFound
	}
	if _, err := a.resolveCommand(); err != nil {
		return "", errCredentialNotFound
	}
	// The official Codex client owns and refreshes ChatGPT OAuth state. Avoid an
	// account/read subprocess on the hot path; codex exec returns an explicit
	// authentication error if the session has actually expired.
	return "official-codex-chatgpt-account", nil
}

func (a *chatGPTAccountAdapter) DiscoverModels(ctx context.Context, _ string) ([]string, error) {
	command, err := a.resolveCommand()
	if err != nil {
		return nil, err
	}
	server, err := startCodexAppServer(command)
	if err != nil {
		return nil, err
	}
	defer server.Close()
	return a.syncProvider(ctx, server)
}

func (a *chatGPTAccountAdapter) Disconnect(ctx context.Context, _ string) error {
	a.resetBridgeServer()
	command, err := a.resolveCommand()
	if err == nil {
		if server, startErr := startCodexAppServer(command); startErr == nil {
			logoutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			_ = server.request(logoutCtx, "account/logout", map[string]any{}, nil)
			cancel()
			server.Close()
		}
	}
	if a.manager != nil && a.manager.store != nil {
		if provider, found, getErr := a.manager.store.get(chatGPTAccountProviderID); getErr == nil && found && provider.ManagedBy == "account" {
			_ = a.manager.store.remove(chatGPTAccountProviderID)
		}
	}
	config, configErr := loadChatGPTCodexConfig()
	if configErr == nil {
		config.Email = ""
		config.PlanType = ""
		configErr = saveChatGPTCodexConfig(config)
	}
	return configErr
}
