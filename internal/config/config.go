// Package config loads and persists the CLI configuration in ~/.volcano/config.json.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const (
	envToken              = "VOLCANO_TOKEN"
	envProjectID          = "VOLCANO_PROJECT_ID"
	envAPIURL             = "VOLCANO_API_URL"
	envWebURL             = "VOLCANO_WEB_URL"
	envFirstPartyDeviceID = "VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID"
	defaultConfigDirName  = ".volcano"
	defaultConfigFileName = "config.json"
	defaultConfigDirMode  = 0o700
	defaultConfigFileMode = 0o600
	defaultCompiledAPIURL = "https://api.volcano.dev"
	defaultCompiledWebURL = "https://volcano.dev"
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
	compiledDefaultWebURL            = defaultCompiledWebURL
	compiledFirstPartyDeviceClientID = ""
)

// Config represents the CLI configuration stored in ~/.volcano/config.json.
type Config struct {
	// APIBaseURL overrides the compiled API URL for synthetic command configs.
	// It is intentionally not persisted to the user's cloud config file.
	APIBaseURL     string         `json:"-"`
	UserToken      string         `json:"user_token,omitempty"`
	UserID         string         `json:"user_id,omitempty"`
	AnonKey        string         `json:"-"`
	ServiceKey     string         `json:"-"`
	CurrentProject *ProjectConfig `json:"current_project,omitempty"`
	// FunctionAliases stores per-user function invoke aliases by API URL and
	// project ID scope. Scope keys are produced by FunctionAliasScope.
	FunctionAliases map[string]map[string]string `json:"function_aliases,omitempty"`
	// DocsSource persists a non-default documentation source override so that
	// `volcano docs` commands resolve the same repo/ref/path across invocations
	// without repeating --repo/--ref/--path flags. Nil means "use the compiled
	// default source".
	DocsSource *DocsSourceConfig `json:"docs_source,omitempty"`
	// IgnoreEnv disables environment overrides for synthetic command configs.
	IgnoreEnv bool `json:"-"`
}

// DocsSourceConfig persists a documentation source override. Empty fields fall
// back to the compiled defaults during resolution.
type DocsSourceConfig struct {
	Repo string `json:"repo,omitempty"`
	Ref  string `json:"ref,omitempty"`
	Path string `json:"path,omitempty"`
}

// ProjectConfig represents the currently selected Volcano project.
type ProjectConfig struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Default returns an empty config.
func Default() *Config {
	return &Config{}
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

	data, err := json.MarshalIndent(c, "", "  ")
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
	return c.UserToken
}

// FunctionInvokeToken returns the token used for runtime function invocation.
// Local mode supplies service and anon keys for invoke endpoints; cloud falls
// back to the normal configured token until a project invoke key is available.
func (c *Config) FunctionInvokeToken() string {
	if strings.TrimSpace(c.ServiceKey) != "" {
		return c.ServiceKey
	}
	if strings.TrimSpace(c.AnonKey) != "" {
		return c.AnonKey
	}
	return c.Token()
}

// ProjectID returns the current project ID, with VOLCANO_PROJECT_ID taking precedence unless env overrides are disabled.
func (c *Config) ProjectID() string {
	if projectID := os.Getenv(envProjectID); !c.IgnoreEnv && projectID != "" {
		return projectID
	}
	if c.CurrentProject != nil {
		return c.CurrentProject.ID
	}
	return ""
}

// APIURL returns the API URL with VOLCANO_API_URL taking precedence unless env overrides are disabled.
// Precedence: env > runtime override (APIBaseURL) > compiled default.
func (c *Config) APIURL() string {
	if apiURL := strings.TrimSpace(os.Getenv(envAPIURL)); !c.IgnoreEnv && apiURL != "" {
		return apiURL
	}
	if c.APIBaseURL != "" {
		return c.APIBaseURL
	}
	return compiledDefaultAPIURL
}

// WebURL returns the Volcano web URL for c.APIURL(). See WebURLForAPIURL for
// callers that already resolved the API URL through another path (e.g. a
// runtime override that doesn't flow through c.APIURL()).
func (c *Config) WebURL() string {
	return c.WebURLForAPIURL(c.APIURL())
}

// WebURLForAPIURL returns the Volcano web URL for a specific, already-resolved
// API URL: VOLCANO_WEB_URL takes precedence, then an explicitly compiled-in
// default (e.g. via `make local`'s DEFAULT_WEB_URL, which differs from the
// shipped defaultCompiledWebURL literal only when someone set it), then a URL
// derived from apiURL (see deriveWebURL), then the shipped compiled default.
// The explicit compiled default has to win over derivation: otherwise a
// loopback API URL baked in alongside a non-conventional compiled web URL
// (e.g. a frontend dev server not on port 3000) would have its own compiled
// default silently overridden by the :3000 convention.
func (c *Config) WebURLForAPIURL(apiURL string) string {
	if webURL := strings.TrimSpace(os.Getenv(envWebURL)); !c.IgnoreEnv && webURL != "" {
		return webURL
	}
	if compiledDefaultWebURL != defaultCompiledWebURL {
		return compiledDefaultWebURL
	}
	if derived := deriveWebURL(apiURL); derived != "" {
		return derived
	}
	return compiledDefaultWebURL
}

// IsLoopbackAPIURL reports whether apiURL points at a loopback address
// ("localhost" or a loopback IP). Shared by local-mode's device-client
// selection and by deriveWebURL below.
func IsLoopbackAPIURL(apiURL string) bool {
	u, err := url.Parse(strings.TrimSpace(apiURL))
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// deriveWebURL derives the Volcano Web origin from an API URL for the common
// naming conventions, so only VOLCANO_API_URL needs to be set to point the
// CLI at a non-default environment: a loopback API host (local-mode) maps to
// the conventional local Web port 3000, and an "api." API host maps to the
// same host with that prefix stripped (api.volcano.dev -> volcano.dev,
// api.staging.volcano.dev -> staging.volcano.dev). Returns "" when neither
// convention applies, so the caller falls back to the compiled default.
func deriveWebURL(apiURL string) string {
	if IsLoopbackAPIURL(apiURL) {
		return "http://localhost:3000"
	}
	u, err := url.Parse(strings.TrimSpace(apiURL))
	if err != nil || u.Host == "" {
		return ""
	}
	// DNS hostnames are case-insensitive, so match the "api." prefix that way too.
	host, ok := strings.CutPrefix(strings.ToLower(u.Hostname()), "api.")
	if !ok || host == "" {
		return ""
	}
	if port := u.Port(); port != "" {
		host += ":" + port
	}
	return u.Scheme + "://" + host
}

// WebURLOverride returns an explicit VOLCANO_WEB_URL value when one is set,
// reporting false otherwise. Used only by Signup, to fail fast on a
// misconfigured override before allocating a device code (see WebURLForAPIURL
// for the derived-origin precedence Signup otherwise falls back to).
// LoginWithBrowser does not use this: its browser flow opens the backend's
// device-authorization verification URL directly and never routes through
// Volcano Web.
func (c *Config) WebURLOverride() (string, bool) {
	if c.IgnoreEnv {
		return "", false
	}
	if webURL := strings.TrimSpace(os.Getenv(envWebURL)); webURL != "" {
		return webURL, true
	}
	return "", false
}

// FunctionAliasScope returns the config key for aliases bound to one API URL
// and project ID. The API URL is trimmed so trailing slashes do not split scopes.
func FunctionAliasScope(apiURL, projectID string) string {
	return strings.TrimRight(strings.TrimSpace(apiURL), "/") + "|" + strings.TrimSpace(projectID)
}

// FunctionAlias returns the function ID configured for alias in the given scope.
func (c *Config) FunctionAlias(scope, alias string) (string, bool) {
	if c == nil || c.FunctionAliases == nil {
		return "", false
	}
	aliases := c.FunctionAliases[strings.TrimSpace(scope)]
	if aliases == nil {
		return "", false
	}
	functionID, ok := aliases[strings.TrimSpace(alias)]
	return functionID, ok
}

// SetFunctionAlias stores alias in the given scope.
func (c *Config) SetFunctionAlias(scope, alias, functionID string) {
	scope = strings.TrimSpace(scope)
	alias = strings.TrimSpace(alias)
	functionID = strings.TrimSpace(functionID)
	if c.FunctionAliases == nil {
		c.FunctionAliases = map[string]map[string]string{}
	}
	if c.FunctionAliases[scope] == nil {
		c.FunctionAliases[scope] = map[string]string{}
	}
	c.FunctionAliases[scope][alias] = functionID
}

// DeleteFunctionAlias removes alias from the given scope and reports whether it existed.
func (c *Config) DeleteFunctionAlias(scope, alias string) bool {
	if c == nil || c.FunctionAliases == nil {
		return false
	}
	scope = strings.TrimSpace(scope)
	alias = strings.TrimSpace(alias)
	aliases := c.FunctionAliases[scope]
	if aliases == nil {
		return false
	}
	if _, ok := aliases[alias]; !ok {
		return false
	}
	delete(aliases, alias)
	if len(aliases) == 0 {
		delete(c.FunctionAliases, scope)
	}
	return true
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
