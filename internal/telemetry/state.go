package telemetry

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Kong/volcano-cli/internal/config"
)

// State is the telemetry settings file, ~/.volcano/telemetry.json. It is kept
// apart from config.json because `volcano logout` deletes config.json, and an
// opt-out must survive a logout.
type State struct {
	DeviceID    string `json:"device_id,omitempty"`
	Disabled    bool   `json:"disabled,omitempty"`
	NoticeShown bool   `json:"notice_shown,omitempty"`
}

// StateStore loads and saves State.
type StateStore interface {
	Load() (State, error)
	Save(State) error
}

// FileStateStore stores State next to config.json. Path overrides the location
// (tests).
type FileStateStore struct {
	Path string
}

func (s FileStateStore) path() (string, error) {
	if s.Path != "" {
		return s.Path, nil
	}
	configPath, err := config.Path()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(configPath), "telemetry.json"), nil
}

// Load reads the state file. A missing file is the zero State.
func (s FileStateStore) Load() (State, error) {
	path, err := s.path()
	if err != nil {
		return State{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return State{}, nil
	}
	if err != nil {
		return State{}, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return state, nil
}

// Save writes the state file atomically.
func (s FileStateStore) Save(state State) error {
	path, err := s.path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".telemetry-*.json")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
