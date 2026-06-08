// Package config loads and persists the CLI configuration in ~/.volcano/config.json.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	envToken              = "VOLCANO_TOKEN"
	envProjectID          = "VOLCANO_PROJECT_ID"
	envAPIURL             = "VOLCANO_API_URL"
	envContext            = "VOLCANO_CONTEXT"
	envFirstPartyDeviceID = "VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID"
	defaultConfigDirName  = ".volcano"
	defaultConfigFileName = "config.json"
	defaultConfigDirMode  = 0o700
	defaultConfigFileMode = 0o600
	defaultCompiledAPIURL = "https://api.volcano.dev"

	// ContextDev is the built-in context for localhost development APIs.
	ContextDev = "dev"
	// ContextStage is the built-in context for staging APIs.
	ContextStage = "stage"
	// ContextProd is the built-in context for production APIs.
	ContextProd = "prod"

	devAPIURL   = "http://localhost:8000"
	stageAPIURL = "https://api.staging.volcano.dev"
	prodAPIURL  = "https://api.volcano.dev"

	devDeviceClientID  = "devcli_dcc913b9786f9ef2825b861c"
	prodDeviceClientID = "devcli_94e247237984b85cfd58d37e"
)

var (
	// ErrNotAuthenticated indicates no token is configured for cloud API calls.
	ErrNotAuthenticated = errors.New("not authenticated. Run 'volcano login' first")
	// ErrNoProjectSelected indicates no active project is configured for project-scoped cloud API calls.
	ErrNoProjectSelected = errors.New("no project selected. Run 'volcano use <project-name>' or set VOLCANO_PROJECT_ID")
)

// These variables are intentionally settable with -ldflags -X.
var (
	compiledDefaultAPIURL            = defaultCompiledAPIURL
	compiledFirstPartyDeviceClientID = ""
)

// Config represents the CLI configuration stored in ~/.volcano/config.json.
type Config struct {
	// APIBaseURL overrides the compiled API URL for synthetic command configs.
	// It is intentionally not persisted to the user's cloud config file.
	APIBaseURL string `json:"-"`
	// ContextOverride is set by --context and intentionally not persisted.
	ContextOverride string `json:"-"`
	// Legacy flat fields are still read and mirrored for compatibility with
	// older callers/tests. New writes persist them under Contexts instead.
	UserToken      string                    `json:"user_token,omitempty"`
	UserID         string                    `json:"user_id,omitempty"`
	CurrentProject *ProjectConfig            `json:"current_project,omitempty"`
	DefaultContext string                    `json:"default_context,omitempty"`
	Contexts       map[string]*ContextConfig `json:"contexts,omitempty"`
	// IgnoreEnv disables environment overrides for synthetic command configs.
	IgnoreEnv bool `json:"-"`
}

// ContextConfig represents one named Volcano API target and its credentials.
type ContextConfig struct {
	APIBaseURL     string         `json:"api_url,omitempty"`
	DeviceClientID string         `json:"device_client_id,omitempty"`
	UserToken      string         `json:"user_token,omitempty"`
	UserID         string         `json:"user_id,omitempty"`
	CurrentProject *ProjectConfig `json:"current_project,omitempty"`
}

// ProjectConfig represents the currently selected Volcano project.
type ProjectConfig struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Default returns an empty config.
func Default() *Config {
	return &Config{DefaultContext: ContextProd}
}

// Path returns the default on-disk config path.
func Path() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(homeDir, defaultConfigDirName, defaultConfigFileName), nil
}

// Load reads ~/.volcano/config.json. Missing config is not an error.
func Load() (*Config, error) {
	configPath, err := Path()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		return Default(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}
	cfg.applyLoadedDefaults()
	return &cfg, nil
}

// Save writes the config to ~/.volcano/config.json with owner-only permissions.
func (c *Config) Save() error {
	configPath, err := Path()
	if err != nil {
		return err
	}

	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, defaultConfigDirMode); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	if err := os.Chmod(configDir, defaultConfigDirMode); err != nil {
		return fmt.Errorf("failed to set config directory permissions: %w", err)
	}

	persisted := c.persisted()
	data, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if info, err := os.Stat(configPath); err == nil {
		if info.IsDir() {
			return errors.New("config path is a directory")
		}
		if err := os.Chmod(configPath, defaultConfigFileMode); err != nil {
			return fmt.Errorf("failed to set config file permissions: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to stat config file: %w", err)
	}

	if err := os.WriteFile(configPath, data, defaultConfigFileMode); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	if err := os.Chmod(configPath, defaultConfigFileMode); err != nil {
		return fmt.Errorf("failed to set config file permissions: %w", err)
	}
	return nil
}

// Delete removes ~/.volcano/config.json. A missing file is considered success.
func Delete() error {
	configPath, err := Path()
	if err != nil {
		return err
	}
	if err := os.Remove(configPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete config file: %w", err)
	}
	return nil
}

// Token returns the configured token, with VOLCANO_TOKEN taking precedence unless env overrides are disabled.
func (c *Config) Token() string {
	if token := os.Getenv(envToken); !c.IgnoreEnv && token != "" {
		return token
	}
	if token := c.ResolvedActiveContext().UserToken; token != "" {
		return token
	}
	return c.UserToken
}

// ProjectID returns the current project ID, with VOLCANO_PROJECT_ID taking precedence unless env overrides are disabled.
func (c *Config) ProjectID() string {
	if projectID := os.Getenv(envProjectID); !c.IgnoreEnv && projectID != "" {
		return projectID
	}
	if project := c.ResolvedActiveContext().CurrentProject; project != nil {
		return project.ID
	}
	if c.CurrentProject != nil {
		return c.CurrentProject.ID
	}
	return ""
}

// APIURL returns the API URL with VOLCANO_API_URL taking precedence unless env overrides are disabled.
// Precedence: env > runtime override (APIBaseURL) > active context > compiled default.
func (c *Config) APIURL() string {
	if apiURL := strings.TrimSpace(os.Getenv(envAPIURL)); !c.IgnoreEnv && apiURL != "" {
		return apiURL
	}
	if c.APIBaseURL != "" {
		return c.APIBaseURL
	}
	if apiURL := strings.TrimSpace(c.ResolvedActiveContext().APIBaseURL); apiURL != "" {
		return apiURL
	}
	return compiledDefaultAPIURL
}

// RequireAuth returns an old-CLI-compatible error when no token is available.
func (c *Config) RequireAuth() error {
	if c.Token() == "" {
		return ErrNotAuthenticated
	}
	return nil
}

// RequireProject returns an old-CLI-compatible error when no project is selected.
func (c *Config) RequireProject() error {
	if strings.TrimSpace(c.ProjectID()) == "" {
		return ErrNoProjectSelected
	}
	return nil
}

// FirstPartyDeviceClientID resolves the first-party OAuth device client ID.
func FirstPartyDeviceClientID() (string, error) {
	if clientID := strings.TrimSpace(os.Getenv(envFirstPartyDeviceID)); clientID != "" {
		return clientID, nil
	}
	if clientID := strings.TrimSpace(compiledFirstPartyDeviceClientID); clientID != "" {
		return clientID, nil
	}
	return "", fmt.Errorf("%s is required", envFirstPartyDeviceID)
}

// DeviceClientID resolves the OAuth device client ID for the active context.
func (c *Config) DeviceClientID() (string, error) {
	if clientID := strings.TrimSpace(os.Getenv(envFirstPartyDeviceID)); !c.IgnoreEnv && clientID != "" {
		return clientID, nil
	}
	if clientID := strings.TrimSpace(c.ResolvedActiveContext().DeviceClientID); clientID != "" {
		return clientID, nil
	}
	return "", fmt.Errorf("device_client_id is required for context %q. Run 'volcano context set %s --device-client-id <id>'", c.ActiveContextName(), c.ActiveContextName())
}

// ActiveContextName returns the selected context name.
func (c *Config) ActiveContextName() string {
	if name := NormalizeContextName(c.ContextOverride); name != "" {
		return name
	}
	if name := strings.TrimSpace(os.Getenv(envContext)); !c.IgnoreEnv && name != "" {
		return NormalizeContextName(name)
	}
	if name := NormalizeContextName(c.DefaultContext); name != "" {
		return name
	}
	return ContextProd
}

// SetContextOverride selects a context for this in-memory config only.
func (c *Config) SetContextOverride(name string) {
	c.ContextOverride = NormalizeContextName(name)
	c.refreshLegacyMirror()
}

// ResolvedActiveContext returns active context values with built-in presets applied.
func (c *Config) ResolvedActiveContext() ContextConfig {
	return c.ResolvedContext(c.ActiveContextName())
}

// ResolvedContext returns context values with built-in presets applied.
func (c *Config) ResolvedContext(name string) ContextConfig {
	name = NormalizeContextName(name)
	if name == "" {
		name = ContextProd
	}
	resolved := PresetContext(name)
	if c.Contexts == nil {
		return resolved
	}
	stored := c.Contexts[name]
	if stored == nil {
		return resolved
	}
	if strings.TrimSpace(stored.APIBaseURL) != "" {
		resolved.APIBaseURL = stored.APIBaseURL
	}
	if strings.TrimSpace(stored.DeviceClientID) != "" {
		resolved.DeviceClientID = stored.DeviceClientID
	}
	if stored.UserToken != "" {
		resolved.UserToken = stored.UserToken
	}
	if stored.UserID != "" {
		resolved.UserID = stored.UserID
	}
	if stored.CurrentProject != nil {
		resolved.CurrentProject = cloneProject(stored.CurrentProject)
	}
	return resolved
}

// EnsureContext returns a mutable context, creating and presetting it when needed.
func (c *Config) EnsureContext(name string) *ContextConfig {
	name = NormalizeContextName(name)
	if name == "" {
		name = ContextProd
	}
	if c.Contexts == nil {
		c.Contexts = make(map[string]*ContextConfig)
	}
	ctx := c.Contexts[name]
	if ctx == nil {
		preset := PresetContext(name)
		ctx = &preset
		c.Contexts[name] = ctx
		return ctx
	}
	applyPresetDefaults(name, ctx)
	return ctx
}

// SetDefaultContext selects the persisted default context.
func (c *Config) SetDefaultContext(name string) {
	name = NormalizeContextName(name)
	if name == "" {
		name = ContextProd
	}
	c.EnsureContext(name)
	c.DefaultContext = name
	c.refreshLegacyMirror()
}

// SetCredentials stores credentials in the active context.
func (c *Config) SetCredentials(token, userID string) {
	ctx := c.EnsureContext(c.ActiveContextName())
	ctx.UserToken = token
	ctx.UserID = userID
	c.refreshLegacyMirror()
}

// SetCurrentProject stores the selected project in the active context.
func (c *Config) SetCurrentProject(project *ProjectConfig) {
	ctx := c.EnsureContext(c.ActiveContextName())
	ctx.CurrentProject = cloneProject(project)
	c.refreshLegacyMirror()
}

// DeleteContext removes persisted values for a context. Built-in presets still resolve.
func (c *Config) DeleteContext(name string) {
	name = NormalizeContextName(name)
	if c.Contexts != nil {
		delete(c.Contexts, name)
	}
	if c.ActiveContextName() == name {
		c.DefaultContext = ContextProd
	}
	c.refreshLegacyMirror()
}

// NormalizeContextName normalizes context aliases.
func NormalizeContextName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "production" {
		return ContextProd
	}
	return name
}

// BuiltInContextNames returns canonical built-in context names.
func BuiltInContextNames() []string {
	return []string{ContextDev, ContextStage, ContextProd}
}

// IsBuiltInContext reports whether name is a built-in preset context.
func IsBuiltInContext(name string) bool {
	switch NormalizeContextName(name) {
	case ContextDev, ContextStage, ContextProd:
		return true
	default:
		return false
	}
}

// PresetContext returns the built-in preset for name, or an empty custom context.
func PresetContext(name string) ContextConfig {
	switch NormalizeContextName(name) {
	case ContextDev:
		return ContextConfig{APIBaseURL: devAPIURL, DeviceClientID: devDeviceClientID}
	case ContextStage:
		return ContextConfig{APIBaseURL: stageAPIURL, DeviceClientID: devDeviceClientID}
	case ContextProd:
		return ContextConfig{APIBaseURL: prodAPIURL, DeviceClientID: prodDeviceClientID}
	default:
		return ContextConfig{}
	}
}

func (c *Config) applyLoadedDefaults() {
	if c.DefaultContext == "" {
		c.DefaultContext = ContextProd
	} else {
		c.DefaultContext = NormalizeContextName(c.DefaultContext)
	}
	if len(c.Contexts) == 0 && hasLegacyConfig(c) {
		legacyContext := c.legacyMigrationContextName()
		ctx := PresetContext(legacyContext)
		ctx.UserToken = c.UserToken
		ctx.UserID = c.UserID
		ctx.CurrentProject = cloneProject(c.CurrentProject)
		c.Contexts = map[string]*ContextConfig{legacyContext: &ctx}
		c.DefaultContext = legacyContext
	}
	c.refreshLegacyMirror()
}

func (c *Config) legacyMigrationContextName() string {
	if name := NormalizeContextName(os.Getenv(envContext)); !c.IgnoreEnv && name != "" {
		return name
	}
	if name := contextNameForAPIURL(os.Getenv(envAPIURL)); !c.IgnoreEnv && name != "" {
		return name
	}
	if name := NormalizeContextName(c.DefaultContext); name != "" {
		return name
	}
	return ContextProd
}

func contextNameForAPIURL(apiURL string) string {
	switch strings.TrimRight(strings.TrimSpace(apiURL), "/") {
	case strings.TrimRight(devAPIURL, "/"):
		return ContextDev
	case strings.TrimRight(stageAPIURL, "/"):
		return ContextStage
	case strings.TrimRight(prodAPIURL, "/"):
		return ContextProd
	default:
		return ""
	}
}

func (c *Config) persisted() Config {
	persisted := Config{
		DefaultContext: NormalizeContextName(c.DefaultContext),
		Contexts:       cloneContexts(c.Contexts),
	}
	if persisted.DefaultContext == "" {
		persisted.DefaultContext = ContextProd
	}
	if hasLegacyConfig(c) {
		ctx := ensureContextInMap(persisted.Contexts, c.ActiveContextName())
		applyPresetDefaults(c.ActiveContextName(), ctx)
		if c.UserToken != "" {
			ctx.UserToken = c.UserToken
		}
		if c.UserID != "" {
			ctx.UserID = c.UserID
		}
		if c.CurrentProject != nil {
			ctx.CurrentProject = cloneProject(c.CurrentProject)
		}
	}
	ensureContextInMap(persisted.Contexts, persisted.DefaultContext)
	applyPresetDefaults(persisted.DefaultContext, persisted.Contexts[persisted.DefaultContext])
	return persisted
}

func (c *Config) refreshLegacyMirror() {
	ctx := c.ResolvedActiveContext()
	c.UserToken = ctx.UserToken
	c.UserID = ctx.UserID
	c.CurrentProject = cloneProject(ctx.CurrentProject)
}

func hasLegacyConfig(c *Config) bool {
	return c.UserToken != "" || c.UserID != "" || c.CurrentProject != nil
}

func cloneContexts(contexts map[string]*ContextConfig) map[string]*ContextConfig {
	cloned := make(map[string]*ContextConfig, len(contexts))
	for name, ctx := range contexts {
		if ctx == nil {
			continue
		}
		ctxCopy := *ctx
		ctxCopy.CurrentProject = cloneProject(ctx.CurrentProject)
		cloned[NormalizeContextName(name)] = &ctxCopy
	}
	return cloned
}

func ensureContextInMap(contexts map[string]*ContextConfig, name string) *ContextConfig {
	name = NormalizeContextName(name)
	if name == "" {
		name = ContextProd
	}
	if contexts == nil {
		// All current callers pass a non-nil map from cloneContexts.
		return nil
	}
	ctx := contexts[name]
	if ctx == nil {
		preset := PresetContext(name)
		ctx = &preset
		contexts[name] = ctx
	}
	return ctx
}

func applyPresetDefaults(name string, ctx *ContextConfig) {
	if ctx == nil {
		return
	}
	preset := PresetContext(name)
	if ctx.APIBaseURL == "" {
		ctx.APIBaseURL = preset.APIBaseURL
	}
	if ctx.DeviceClientID == "" {
		ctx.DeviceClientID = preset.DeviceClientID
	}
}

func cloneProject(project *ProjectConfig) *ProjectConfig {
	if project == nil {
		return nil
	}
	cloned := *project
	return &cloned
}
