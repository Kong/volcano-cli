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
	"sync/atomic"

	"github.com/gofrs/flock"
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
	defaultConfigLockMode = 0o600
	defaultCompiledAPIURL = "https://api.volcano.dev"
	defaultCompiledWebURL = "https://volcano.dev"

	// AccountTokenPrefix marks an account platform token, which reaches every
	// project the account owns.
	AccountTokenPrefix = "pk-"
	// ProjectTokenPrefix marks a project access token, which reaches only the
	// project it was minted in.
	ProjectTokenPrefix = "pt-"
)

var (
	// ErrNotAuthenticated indicates no token is configured for cloud API calls.
	ErrNotAuthenticated = errors.New("not authenticated. Run 'volcano login' first, or 'volcano signup' if you're new to Volcano")
	// ErrNoProjectSelected indicates no active project is configured for project-scoped cloud API calls.
	ErrNoProjectSelected = errors.New("no project selected. Run 'volcano use <project-name>'. " +
		"To create a project and link this directory to it, run 'volcano connect .'")
	// ErrNoProjectSelectedForProjectToken is ErrNoProjectSelected for a project
	// access token, which cannot look a project up by name.
	ErrNoProjectSelectedForProjectToken = errors.New("no project selected. A project access token (" + ProjectTokenPrefix +
		") is scoped to one project: run 'volcano use <project-id>' with that project's ID")
	// ErrAccountTokenRequired indicates a command needs account-wide access but
	// the configured credential only reaches a single project.
	ErrAccountTokenRequired = errors.New("this command needs an account token (" + AccountTokenPrefix +
		") but the current credential is a project access token (" + ProjectTokenPrefix +
		"), which only reaches the project it was minted in. Run 'volcano login', or set VOLCANO_TOKEN to an account token")
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
	// Link is the working directory's project link, found by Load. It is never
	// saved here: the link lives in the linked directory.
	Link *Link `json:"-"`
	// FunctionAliases stores per-user function invoke aliases by API URL and
	// project ID scope. Scope keys are produced by FunctionAliasScope.
	FunctionAliases map[string]map[string]string `json:"function_aliases,omitempty"`
	// DocsSource persists a non-default documentation source override so that
	// `volcano docs` commands resolve the same repo/ref/path across invocations
	// without repeating --repo/--ref/--path flags. Nil means "use the compiled
	// default source".
	DocsSource *DocsSourceConfig `json:"docs_source,omitempty"`
	// Telemetry holds the usage-data settings (see internal/telemetry). Logout
	// keeps it, so an opt-out survives logging out.
	Telemetry *TelemetryConfig `json:"telemetry,omitempty"`
	// Feedback holds the `volcano feedback` setting (see internal/feedback).
	// Logout keeps it, like Telemetry.
	Feedback *FeedbackConfig `json:"feedback,omitempty"`
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

// TelemetryConfig persists the usage-data settings.
type TelemetryConfig struct {
	// DeviceID is the random ID usage events are linked to when not logged in.
	DeviceID string `json:"device_id,omitempty"`
	// Disabled is set by `volcano telemetry disable`.
	Disabled bool `json:"disabled,omitempty"`
	// NoticeShown records that the first-run notice was printed.
	NoticeShown bool `json:"notice_shown,omitempty"`
}

// FeedbackConfig persists the `volcano feedback` setting, which is separate
// from telemetry.
type FeedbackConfig struct {
	// Disabled is set by `volcano feedback disable`.
	Disabled bool `json:"disabled,omitempty"`
	// NoticeShown records that the first-run notice was printed for
	// feedback, which is tracked apart from telemetry's.
	NoticeShown bool `json:"notice_shown,omitempty"`
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

// Load reads ~/.volcano/config.json and the working directory's project link.
// Missing config is not an error. A link that can't be used is a *LinkError.
func Load() (*Config, error) {
	cfg, linkErr, err := LoadIgnoringBadLink()
	if err != nil {
		return nil, err
	}
	if linkErr != nil {
		return nil, linkErr
	}
	return cfg, nil
}

// LoadIgnoringBadLink is Load for the commands that work without the link, or
// replace it: login, signup and connect. A link that can't be used is left out
// of the config and returned as linkErr instead of failing the load.
func LoadIgnoringBadLink() (cfg *Config, linkErr *LinkError, err error) {
	cfg, err = load()
	if err != nil {
		return nil, nil, err
	}
	wd, err := os.Getwd()
	if err != nil {
		// A working directory that no longer exists can't be linked.
		return cfg, nil, nil //nolint:nilerr // the link is optional; the config itself loaded
	}
	cfg.Link, err = FindLink(wd)
	if bad, ok := errors.AsType[*LinkError](err); ok {
		return cfg, bad, nil
	}
	if err != nil {
		return nil, nil, err
	}
	return cfg, nil, nil
}

// LoadWithoutLink reads ~/.volcano/config.json alone, for configs that never
// use a project link.
func LoadWithoutLink() (*Config, error) {
	return load()
}

func load() (*Config, error) {
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
	return withLock(c.save)
}

func (c *Config) save() error {
	configPath, err := Path()
	if err != nil {
		return err
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

// Update locks, reloads, and conditionally saves the config.
func Update(mutate func(*Config) (bool, error)) error {
	return withLock(func() error {
		cfg, err := load()
		if err != nil {
			return err
		}
		changed, err := mutate(cfg)
		if err != nil || !changed {
			return err
		}
		return cfg.save()
	})
}

// ClearLogin removes the login and everything tied to the account (the current
// project and function aliases) from ~/.volcano/config.json, and keeps the
// settings that belong to the machine (docs source, telemetry). The file is
// removed when nothing is left in it, or when it cannot be read or parsed, so
// logout still removes the token from a damaged or unreadable file.
func ClearLogin() error {
	return withLock(func() error {
		cfg, err := load()
		if err != nil {
			return remove()
		}
		cfg.UserToken = ""
		cfg.UserID = ""
		cfg.CurrentProject = nil
		cfg.FunctionAliases = nil
		if cfg.DocsSource == nil && cfg.Telemetry == nil && cfg.Feedback == nil {
			return remove()
		}
		return cfg.save()
	})
}

// Delete removes ~/.volcano/config.json. A missing file is considered success.
func Delete() error {
	return withLock(remove)
}

func remove() error {
	configPath, err := Path()
	if err != nil {
		return err
	}
	if err := os.Remove(configPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete config file: %w", err)
	}
	return nil
}

func withLock(run func() error) (err error) {
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

	lock := flock.New(configPath+".lock", flock.SetPermissions(defaultConfigLockMode))
	if err := lock.Lock(); err != nil {
		return fmt.Errorf("failed to lock config: %w", err)
	}
	defer func() {
		if unlockErr := lock.Unlock(); err == nil && unlockErr != nil {
			err = fmt.Errorf("failed to unlock config: %w", unlockErr)
		}
	}()
	return run()
}

// Token returns the configured token, with VOLCANO_TOKEN taking precedence unless env overrides are disabled.
func (c *Config) Token() string {
	// Trimmed because this is the documented CI path, where
	// `export VOLCANO_TOKEN=$(cat secret)` readily carries a trailing newline.
	// Untrimmed, that reaches the Authorization header and net/http refuses to
	// send the request at all, reporting an invalid header field rather than
	// anything the user can act on.
	if token := strings.TrimSpace(os.Getenv(envToken)); !c.IgnoreEnv && token != "" {
		return token
	}
	return c.UserToken
}

// FunctionInvokeToken returns the token used for runtime function invocation.
// Cloud uses the reserved data-plane service key (via the invoke token
// provider) or falls back to the configured token. In local mode the session
// layer sends no credential at all (see session.Factory.APIClient), so this
// value is unused there.
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
// Precedence: env > the working directory's link > the project `volcano use` saved.
func (c *Config) ProjectID() string {
	if projectID := c.ProjectIDFromEnv(); projectID != "" {
		return projectID
	}
	if project := c.Project(); project != nil {
		return project.ID
	}
	return ""
}

// SelectedProjectID returns what ProjectID returns without recording a
// VOLCANO_PROJECT_ID use, for reporting the selected project in metadata
// rather than running against it. A recorded use prints the deprecation
// warning after the command.
func (c *Config) SelectedProjectID() string {
	if projectID := c.envProjectID(); projectID != "" {
		return projectID
	}
	if project := c.Project(); project != nil {
		return project.ID
	}
	return ""
}

// Project returns the selected project without the VOLCANO_PROJECT_ID
// override: the working directory's link, else the project `volcano use`
// saved. Nil when neither is set.
func (c *Config) Project() *ProjectConfig {
	if c.Link != nil {
		return &c.Link.Project
	}
	return c.CurrentProject
}

// ProjectIDFromEnv returns VOLCANO_PROJECT_ID alone, without the project a
// previous 'volcano use' saved. Callers that have to tell the two apart — a
// failure that names the project it ran against — need the distinction.
//
// Trimmed for the same reason as Token: CI sets it the same way, where the
// value is as likely to arrive with a trailing newline, and untrimmed it
// failed UUID parsing as "invalid UUID length: 37".
//
// The variable is deprecated. A non-empty value is recorded so the CLI can
// warn once, after the command, that it was used; see ConsumeProjectEnvUse.
func (c *Config) ProjectIDFromEnv() string {
	projectID := c.envProjectID()
	if projectID != "" {
		projectEnvUsed.Store(true)
	}
	return projectID
}

// envProjectID is VOLCANO_PROJECT_ID, trimmed, or "" when env overrides are
// disabled.
func (c *Config) envProjectID() string {
	if c.IgnoreEnv {
		return ""
	}
	return strings.TrimSpace(os.Getenv(envProjectID))
}

// ProjectEnvDeprecation is the warning printed when VOLCANO_PROJECT_ID chose
// the project a command ran against.
const ProjectEnvDeprecation = "VOLCANO_PROJECT_ID is deprecated and will be removed in a future release. " +
	"Unset it and run 'volcano use <project-id>', or 'volcano login --token <token> --project <project-id>' in CI."

// projectEnvUsed records that VOLCANO_PROJECT_ID was read for a project in
// this process.
var projectEnvUsed atomic.Bool

// ConsumeProjectEnvUse reports whether VOLCANO_PROJECT_ID was read for a
// project since the last call, and clears the record.
func ConsumeProjectEnvUse() bool {
	return projectEnvUsed.Swap(false)
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
	if strings.TrimSpace(c.ProjectID()) != "" {
		return nil
	}
	if IsProjectToken(c.Token()) {
		return ErrNoProjectSelectedForProjectToken
	}
	return ErrNoProjectSelected
}

// RequireAccountToken rejects a project access token on a command that needs
// account-wide access. Any other credential is treated as an account token: the
// server is the authority on what a token may do, and tokens minted before the
// prefixes existed carry neither.
func (c *Config) RequireAccountToken() error {
	if IsProjectToken(c.Token()) {
		return ErrAccountTokenRequired
	}
	return nil
}

// IsProjectToken reports whether token is a project access token.
//
// Case-insensitive on purpose. The unknown-prefix default is deliberately
// fail-open, for credentials minted before the prefixes existed, but "PT-" is
// not one of those — it is a mangled pt- token, and matching case-sensitively
// let it past every guard here and fail as an opaque server error instead.
func IsProjectToken(token string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(token)), ProjectTokenPrefix)
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
