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
	envFirstPartyDeviceID = "VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID"
	defaultConfigDirName  = ".volcano"
	defaultConfigFileName = "config.json"
	defaultConfigDirMode  = 0o700
	defaultConfigFileMode = 0o600
	defaultCompiledAPIURL = "https://api.volcano.dev"
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
	APIBaseURL     string         `json:"-"`
	UserToken      string         `json:"user_token,omitempty"`
	UserID         string         `json:"user_id,omitempty"`
	CurrentProject *ProjectConfig `json:"current_project,omitempty"`
	// IgnoreEnv disables environment overrides for synthetic command configs.
	IgnoreEnv bool `json:"-"`
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
