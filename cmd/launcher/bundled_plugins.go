package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
)

const bundledPluginStateVersion = 1

type bundledPluginManifest struct {
	ID          string
	Name        string
	Description string
	Version     string
	Executable  string
	Arguments   []string
	Environment []string
	Transport   string
	License     string
	Upstream    string
}

type bundledPluginState struct {
	Enabled bool `json:"enabled"`
}

type bundledPluginStateFile struct {
	Version int                           `json:"version"`
	Plugins map[string]bundledPluginState `json:"plugins"`
}

// Intentionally small. Shipping a new bundled plugin is a release-engineering
// decision: add a reviewed, version-pinned manifest here and place the matching
// executable under plugins/<id>/bin in each platform release package.
var bundledPluginManifests = []bundledPluginManifest{}

var bundledPluginStateMu sync.Mutex

func bundledPluginStatePath() string {
	return filepath.Join(tlStudioStateDirectory(), "bundled-plugins.json")
}

func normalizeBundledPluginManifest(input bundledPluginManifest) (bundledPluginManifest, error) {
	input.ID = normalizePluginID(input.ID)
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	input.Version = strings.TrimSpace(input.Version)
	input.Executable = strings.TrimSpace(input.Executable)
	input.Transport = strings.ToLower(strings.TrimSpace(input.Transport))
	input.License = strings.TrimSpace(input.License)
	input.Upstream = strings.TrimSpace(input.Upstream)
	if input.ID == "" || !validProviderID(input.ID) {
		return bundledPluginManifest{}, errors.New("bundled plugin ID is invalid")
	}
	if input.Name == "" {
		return bundledPluginManifest{}, errors.New("bundled plugin name is required")
	}
	if input.Version == "" {
		return bundledPluginManifest{}, errors.New("bundled plugin version is required")
	}
	if input.Executable == "" || strings.ContainsAny(input.Executable, `/\\`) {
		return bundledPluginManifest{}, errors.New("bundled plugin executable must be a file name")
	}
	if input.Transport == "" {
		input.Transport = pluginTransportStdio
	}
	if input.Transport != pluginTransportStdio {
		return bundledPluginManifest{}, errors.New("bundled plugin transport is unsupported")
	}
	input.Arguments = append([]string(nil), input.Arguments...)
	seenEnv := map[string]bool{}
	environment := make([]string, 0, len(input.Environment))
	for _, rawName := range input.Environment {
		name := normalizePluginEnvName(rawName)
		if name == "" {
			return bundledPluginManifest{}, fmt.Errorf("bundled plugin environment variable %q is invalid", rawName)
		}
		if !seenEnv[name] {
			seenEnv[name] = true
			environment = append(environment, name)
		}
	}
	sort.Strings(environment)
	input.Environment = environment
	return input, nil
}

func bundledPluginManifestByID(id string) (bundledPluginManifest, bool) {
	id = normalizePluginID(id)
	for _, raw := range bundledPluginManifests {
		manifest, err := normalizeBundledPluginManifest(raw)
		if err == nil && manifest.ID == id {
			return manifest, true
		}
	}
	return bundledPluginManifest{}, false
}

func bundledPluginPackageRoot() string {
	if override := strings.TrimSpace(os.Getenv("TL_STUDIO_BUNDLED_PLUGIN_ROOT")); override != "" {
		if abs, err := filepath.Abs(override); err == nil {
			return filepath.Clean(abs)
		}
	}
	executable, err := os.Executable()
	if err != nil {
		return "."
	}
	if resolved, resolveErr := filepath.EvalSymlinks(executable); resolveErr == nil {
		executable = resolved
	}
	return filepath.Dir(executable)
}

func bundledPluginExecutableName(manifest bundledPluginManifest, goos string) string {
	name := manifest.Executable
	if goos == "windows" && !strings.HasSuffix(strings.ToLower(name), ".exe") {
		name += ".exe"
	}
	return name
}

func bundledPluginExecutableAt(root string, manifest bundledPluginManifest, goos string) string {
	return filepath.Join(root, "plugins", manifest.ID, "bin", bundledPluginExecutableName(manifest, goos))
}

func bundledPluginExecutable(manifest bundledPluginManifest) string {
	return bundledPluginExecutableAt(bundledPluginPackageRoot(), manifest, runtime.GOOS)
}

func minimalBundledPluginEnvironment() []string {
	// Bundled executables are part of TL Studio's trusted release, but they
	// should not inherit unrelated user secrets (cloud keys, tokens, etc.) from
	// the launcher process. Keep only ordinary OS/runtime variables; explicit
	// plugin credentials are appended separately by the MCP client.
	names := []string{
		"PATH", "HOME", "USER", "USERPROFILE",
		"SYSTEMROOT", "WINDIR", "COMSPEC",
		"TEMP", "TMP", "TMPDIR",
		"LANG", "LC_ALL", "LC_CTYPE",
		"XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME",
		"SSL_CERT_FILE", "SSL_CERT_DIR",
	}
	result := make([]string, 0, len(names))
	seen := map[string]bool{}
	for _, name := range names {
		lookup := name
		if runtime.GOOS == "windows" {
			lookup = strings.ToUpper(name)
		}
		if seen[lookup] {
			continue
		}
		value, ok := os.LookupEnv(name)
		if !ok && runtime.GOOS == "windows" {
			for _, entry := range os.Environ() {
				key, valuePart, found := strings.Cut(entry, "=")
				if found && strings.EqualFold(key, name) {
					value = valuePart
					ok = true
					break
				}
			}
		}
		if ok {
			seen[lookup] = true
			result = append(result, name+"="+value)
		}
	}
	return result
}

func readBundledPluginStatesLocked() (bundledPluginStateFile, error) {
	stored := bundledPluginStateFile{Version: bundledPluginStateVersion, Plugins: map[string]bundledPluginState{}}
	data, err := os.ReadFile(bundledPluginStatePath())
	if errors.Is(err, os.ErrNotExist) {
		return stored, nil
	}
	if err != nil {
		return stored, err
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		return bundledPluginStateFile{}, fmt.Errorf("decode bundled plugin state: %w", err)
	}
	if stored.Version != 0 && stored.Version != bundledPluginStateVersion {
		return bundledPluginStateFile{}, fmt.Errorf("unsupported bundled plugin state version %d", stored.Version)
	}
	stored.Version = bundledPluginStateVersion
	if stored.Plugins == nil {
		stored.Plugins = map[string]bundledPluginState{}
	}
	return stored, nil
}

func writeBundledPluginStatesLocked(stored bundledPluginStateFile) error {
	path := bundledPluginStatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	stored.Version = bundledPluginStateVersion
	if stored.Plugins == nil {
		stored.Plugins = map[string]bundledPluginState{}
	}
	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), "bundled-plugins-*.tmp")
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
	if err := os.Rename(name, path); err != nil {
		return os.WriteFile(path, data, 0o600)
	}
	return nil
}

func bundledPluginEnabled(id string) (bool, error) {
	bundledPluginStateMu.Lock()
	defer bundledPluginStateMu.Unlock()
	stored, err := readBundledPluginStatesLocked()
	if err != nil {
		return false, err
	}
	return stored.Plugins[normalizePluginID(id)].Enabled, nil
}

func setBundledPluginEnabled(id string, enabled bool) error {
	id = normalizePluginID(id)
	if _, ok := bundledPluginManifestByID(id); !ok {
		return os.ErrNotExist
	}
	bundledPluginStateMu.Lock()
	defer bundledPluginStateMu.Unlock()
	stored, err := readBundledPluginStatesLocked()
	if err != nil {
		return err
	}
	stored.Plugins[id] = bundledPluginState{Enabled: enabled}
	return writeBundledPluginStatesLocked(stored)
}

func bundledPluginConfig(manifest bundledPluginManifest, enabled bool) pluginConfig {
	env := make([]pluginEnvironmentRef, 0, len(manifest.Environment))
	for _, name := range manifest.Environment {
		env = append(env, pluginEnvironmentRef{Name: name, Configured: true})
	}
	return pluginConfig{
		ID:          manifest.ID,
		Name:        manifest.Name,
		Description: manifest.Description,
		Type:        pluginTypeMCP,
		Enabled:     enabled,
		Scope:       "global",
		Transport:   manifest.Transport,
		Command:     bundledPluginExecutable(manifest),
		Arguments:   append([]string(nil), manifest.Arguments...),
		Environment: env,
		Metadata: map[string]string{
			"origin":         "bundled",
			"bundledVersion": manifest.Version,
			"license":        manifest.License,
			"upstream":       manifest.Upstream,
		},
	}
}

func bundledPluginConfigs() ([]pluginConfig, error) {
	configs := make([]pluginConfig, 0, len(bundledPluginManifests))
	for _, raw := range bundledPluginManifests {
		manifest, err := normalizeBundledPluginManifest(raw)
		if err != nil {
			return nil, err
		}
		enabled, err := bundledPluginEnabled(manifest.ID)
		if err != nil {
			return nil, err
		}
		configs = append(configs, bundledPluginConfig(manifest, enabled))
	}
	sort.Slice(configs, func(i, j int) bool {
		return strings.ToLower(configs[i].Name+"\x00"+configs[i].ID) < strings.ToLower(configs[j].Name+"\x00"+configs[j].ID)
	})
	return configs, nil
}

func bundledPluginConfigByID(id string) (pluginConfig, bool, error) {
	manifest, ok := bundledPluginManifestByID(id)
	if !ok {
		return pluginConfig{}, false, nil
	}
	enabled, err := bundledPluginEnabled(manifest.ID)
	if err != nil {
		return pluginConfig{}, false, err
	}
	return bundledPluginConfig(manifest, enabled), true, nil
}

func (m *pluginManager) configsForProject(project string) ([]pluginConfig, error) {
	userConfigs, err := m.store.list(project)
	if err != nil {
		return nil, err
	}
	bundledConfigs, err := bundledPluginConfigs()
	if err != nil {
		return nil, err
	}
	result := append(userConfigs, bundledConfigs...)
	sort.Slice(result, func(i, j int) bool {
		leftOrigin := result[i].Metadata["origin"]
		rightOrigin := result[j].Metadata["origin"]
		if leftOrigin != rightOrigin {
			return leftOrigin == "bundled"
		}
		return strings.ToLower(result[i].Name+"\x00"+result[i].ID) < strings.ToLower(result[j].Name+"\x00"+result[j].ID)
	})
	return result, nil
}

func pluginOrigin(config pluginConfig) string {
	if strings.EqualFold(config.Metadata["origin"], "bundled") {
		return "bundled"
	}
	return "user"
}

func pluginVersion(config pluginConfig) string {
	if pluginOrigin(config) == "bundled" {
		return strings.TrimSpace(config.Metadata["bundledVersion"])
	}
	return ""
}
