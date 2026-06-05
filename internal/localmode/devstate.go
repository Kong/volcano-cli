package localmode

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	configDirName    = ".volcano"
	devStateFileName = "dev-state.json"
	configDirMode    = 0o700
	devStateFileMode = 0o600
)

// DevState is the local development state file shape used by the old CLI.
type DevState struct {
	JWTSecret        string `json:"jwt_secret"`
	EncryptionKey    string `json:"encryption_key"`
	AnonKeySecret    string `json:"anon_key_secret"`
	ServiceKeySecret string `json:"service_key_secret"`
	UserID           string `json:"user_id"`
	AuthUserID       string `json:"auth_user_id,omitempty"`
	UserToken        string `json:"user_token,omitempty"`
	ProjectID        string `json:"project_id"`
	AnonKey          string `json:"anon_key"`
	ServiceKey       string `json:"service_key"`
	DatabaseURL      string `json:"database_url"`
	RedisURL         string `json:"redis_url"`
}

// DevStateFromInfo maps server-owned local metadata into the legacy state file.
func DevStateFromInfo(info Info) DevState {
	return DevState{
		JWTSecret:        info.JWTSecret,
		EncryptionKey:    info.EncryptionKey,
		AnonKeySecret:    info.AnonKeySecret,
		ServiceKeySecret: info.ServiceKeySecret,
		UserID:           info.UserID,
		AuthUserID:       info.AuthUserID,
		UserToken:        info.UserToken,
		ProjectID:        info.ProjectID,
		AnonKey:          info.AnonKey,
		ServiceKey:       info.ServiceKey,
		DatabaseURL:      info.DatabaseURL,
		RedisURL:         info.RedisURL,
	}
}

// DevStatePath returns the on-disk path for local development state.
func DevStatePath() (string, error) {
	return devStatePath(true)
}

func saveDevState(info Info) error {
	path, err := DevStatePath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(DevStateFromInfo(info), "", "  ") //nolint:gosec // Local dev state intentionally persists local-only secrets with owner-only file permissions.
	if err != nil {
		return fmt.Errorf("failed to marshal dev state: %w", err)
	}

	if err := os.WriteFile(path, data, devStateFileMode); err != nil {
		return fmt.Errorf("failed to write dev state file: %w", err)
	}
	if err := os.Chmod(path, devStateFileMode); err != nil {
		return fmt.Errorf("failed to set dev state permissions: %w", err)
	}
	return nil
}

func deleteDevState() error {
	path, err := devStatePath(false)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete dev state file: %w", err)
	}
	return nil
}

func devStatePath(createDir bool) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	configDir := filepath.Join(homeDir, configDirName)
	if createDir {
		if err := os.MkdirAll(configDir, configDirMode); err != nil {
			return "", fmt.Errorf("failed to create .volcano directory: %w", err)
		}
		if err := os.Chmod(configDir, configDirMode); err != nil {
			return "", fmt.Errorf("failed to set .volcano permissions: %w", err)
		}
	}

	return filepath.Join(configDir, devStateFileName), nil
}
