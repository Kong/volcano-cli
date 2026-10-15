package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"

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

// StateStore loads and updates State.
type StateStore interface {
	Load() (State, error)
	// Update reloads the state under a lock, applies fn, and saves when fn
	// reports a change, so concurrent CLI processes never lose each other's
	// writes.
	Update(fn func(*State) (changed bool)) error
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

// lockTimeout bounds how long Update waits for another CLI process.
const lockTimeout = 2 * time.Second

// Update implements StateStore with an advisory lock on telemetry.json.lock.
func (s FileStateStore) Update(fn func(*State) bool) error {
	path, err := s.path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	lock := flock.New(path+".lock", flock.SetPermissions(0o600))
	ctx, cancel := context.WithTimeout(context.Background(), lockTimeout)
	defer cancel()
	locked, err := lock.TryLockContext(ctx, 25*time.Millisecond)
	if err != nil {
		return err
	}
	if !locked {
		return errors.New("telemetry state is locked by another process")
	}
	defer func() { _ = lock.Unlock() }()

	state, err := s.Load()
	if err != nil {
		return err
	}
	if !fn(&state) {
		return nil
	}
	return s.save(path, state)
}

// save writes the state file atomically.
func (s FileStateStore) save(path string, state State) error {
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
