package database

import (
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// MigrationState is where one migration file stands against a database's
// migration history.
type MigrationState string

const (
	// MigrationApplied is a file a run applied.
	MigrationApplied MigrationState = "applied"
	// MigrationBaselined is a file recorded as applied without running.
	MigrationBaselined MigrationState = "baselined"
	// MigrationPending is a file the history does not record, which the next
	// run applies.
	MigrationPending MigrationState = "pending"
	// MigrationChanged is a recorded file whose content differs from what the
	// history recorded. A run refuses it with checksum_mismatch.
	MigrationChanged MigrationState = "changed"
	// MigrationMissing is a recorded file that is not among the project's
	// files. A run reports it with a missing_file warning.
	MigrationMissing MigrationState = "missing"
)

// MigrationFileStatus is one file and where it stands.
type MigrationFileStatus struct {
	Name      string         `json:"name"`
	State     MigrationState `json:"state"`
	AppliedAt *time.Time     `json:"applied_at,omitempty"`
	RunID     *uuid.UUID     `json:"run_id,omitempty"`
}

// MigrationStatus compares a database's migration history with the project's
// files for it.
type MigrationStatus struct {
	Database    string                `json:"database"`
	ActiveRunID *uuid.UUID            `json:"active_run_id,omitempty"`
	Files       []MigrationFileStatus `json:"files"`
	// Compared is false when the project had no files for the database, so
	// Files lists the history alone.
	Compared bool `json:"-"`
}

// Count returns how many files are in state.
func (s MigrationStatus) Count(state MigrationState) int {
	count := 0
	for _, file := range s.Files {
		if file.State == state {
			count++
		}
	}
	return count
}

// CompareMigrations sets each of files, and each file the history records,
// against the history. With no files, it lists the history as recorded.
func CompareMigrations(databaseName string, history *apiclient.DatabaseMigrations, files []MigrationFile) MigrationStatus {
	status := MigrationStatus{Database: databaseName, ActiveRunID: history.ActiveRunId, Files: []MigrationFileStatus{}, Compared: len(files) > 0}
	recorded := make(map[string]apiclient.DatabaseMigrationHistoryEntry, len(history.History))
	for _, entry := range history.History {
		recorded[entry.Name] = entry
	}
	local := make(map[string]bool, len(files))
	for _, file := range files {
		name := file.Name()
		local[name] = true
		entry, ok := recorded[name]
		if !ok {
			status.Files = append(status.Files, MigrationFileStatus{Name: name, State: MigrationPending})
			continue
		}
		fileStatus := recordedStatus(entry)
		if entry.Checksum != file.Checksum() {
			fileStatus.State = MigrationChanged
		}
		status.Files = append(status.Files, fileStatus)
	}
	for _, entry := range history.History {
		if local[entry.Name] {
			continue
		}
		fileStatus := recordedStatus(entry)
		if status.Compared {
			fileStatus.State = MigrationMissing
		}
		status.Files = append(status.Files, fileStatus)
	}
	slices.SortStableFunc(status.Files, func(a, b MigrationFileStatus) int { return strings.Compare(a.Name, b.Name) })
	return status
}

func recordedStatus(entry apiclient.DatabaseMigrationHistoryEntry) MigrationFileStatus {
	state := MigrationApplied
	if entry.Status == apiclient.DatabaseMigrationHistoryEntryStatusBaselined {
		state = MigrationBaselined
	}
	appliedAt := entry.AppliedAt
	return MigrationFileStatus{Name: entry.Name, State: state, AppliedAt: &appliedAt, RunID: entry.RunId}
}
