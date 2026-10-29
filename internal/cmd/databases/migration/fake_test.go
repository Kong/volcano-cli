package migration

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	migrationRunID  = "7f3a1c2e-5d4b-4a91-8c6f-2b1e9d0a4c73"
	migrationAppDir = "/projects/" + migrationProjectID + "/databases/app"
)

// fakeMigrationAPI serves the capability list and the migration routes of the
// database named app, and records the requests that change something.
type fakeMigrationAPI struct {
	t *testing.T
	// unavailable leaves databases.migrations out of the capability list.
	unavailable bool
	history     map[string]any
	// runs are the run bodies GET returns, one per read; the last one repeats.
	runs []map[string]any
	// startStatus and startBody answer POST migration-runs.
	startStatus int
	startBody   map[string]any
	// baselineStatus and baselineBody answer POST migrations/baseline.
	baselineStatus int
	baselineBody   map[string]any

	mu         sync.Mutex
	requests   []string
	sent       []sentFiles
	keys       []string
	runReads   int
	capReads   int
	listedRuns map[string]any
}

type sentFiles struct {
	Files []struct {
		Name string `json:"name"`
		SQL  string `json:"sql"`
	} `json:"files"`
}

func newFakeMigrationAPI(t *testing.T) *fakeMigrationAPI {
	t.Helper()
	return &fakeMigrationAPI{
		t:              t,
		history:        map[string]any{"history": []any{}},
		startStatus:    http.StatusAccepted,
		startBody:      migrationRun("queued", "plan", nil),
		baselineStatus: http.StatusOK,
		baselineBody:   map[string]any{"recorded": []string{}},
		listedRuns:     map[string]any{"data": []any{}},
	}
}

func (f *fakeMigrationAPI) start() *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(f.serve))
	f.t.Cleanup(server.Close)
	return server
}

func (f *fakeMigrationAPI) deps(server *httptest.Server) cliruntime.Deps {
	return cliruntime.Deps{
		HTTPClient: server.Client(),
		APIBaseURL: server.URL,
		NewTicker:  func(time.Duration) cliruntime.Ticker { return tickNow{} },
	}
}

func (f *fakeMigrationAPI) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...)
}

func (f *fakeMigrationAPI) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/capabilities":
		f.capReads++
		status := "available"
		if f.unavailable {
			status = "unavailable"
		}
		f.writeJSON(w, http.StatusOK, map[string]any{"data": []any{
			map[string]any{"id": "sandboxes", "status": "available"},
			map[string]any{"id": "databases.migrations", "status": status},
		}})
	case r.Method == http.MethodGet && r.URL.Path == migrationAppDir+"/migrations":
		f.writeJSON(w, http.StatusOK, f.history)
	case r.Method == http.MethodPost && r.URL.Path == migrationAppDir+"/migrations/baseline":
		f.requests = append(f.requests, "baseline")
		f.decodeFiles(r)
		f.writeJSON(w, f.baselineStatus, f.baselineBody)
	case r.Method == http.MethodPost && r.URL.Path == migrationAppDir+"/migration-runs":
		f.requests = append(f.requests, "start")
		f.keys = append(f.keys, r.Header.Get("Idempotency-Key"))
		f.decodeFiles(r)
		f.writeJSON(w, f.startStatus, f.startBody)
	case r.Method == http.MethodGet && r.URL.Path == migrationAppDir+"/migration-runs":
		f.writeJSON(w, http.StatusOK, f.listedRuns)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, migrationAppDir+"/migration-runs/"):
		if len(f.runs) == 0 {
			f.writeJSON(w, http.StatusNotFound, map[string]any{"error": "migration run not found"})
			return
		}
		run := f.runs[min(f.runReads, len(f.runs)-1)]
		f.runReads++
		f.writeJSON(w, http.StatusOK, run)
	default:
		f.t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	}
}

func (f *fakeMigrationAPI) decodeFiles(r *http.Request) {
	var body sentFiles
	require.NoError(f.t, json.NewDecoder(r.Body).Decode(&body))
	f.sent = append(f.sent, body)
}

func (f *fakeMigrationAPI) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	assert.NoError(f.t, json.NewEncoder(w).Encode(value))
}

// sentNames lists the file names of each request that carried files.
func (f *fakeMigrationAPI) sentNames() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out [][]string
	for _, body := range f.sent {
		var names []string
		for _, file := range body.Files {
			names = append(names, file.Name)
		}
		out = append(out, names)
	}
	return out
}

// migrationRun is a run body with files, each given as "name:outcome".
func migrationRun(status, phase string, files []string, extra ...map[string]any) map[string]any {
	run := map[string]any{
		"id":         migrationRunID,
		"database":   map[string]any{"type": "database", "id": "0b9c3f5e-2a41-4d7e-9f0a-6c8e1d2b3a45", "name": "app"},
		"status":     status,
		"phase":      phase,
		"source":     "api",
		"warnings":   []any{},
		"created_at": "2026-10-29T10:00:00Z",
		"updated_at": "2026-10-29T10:00:00Z",
	}
	if files != nil {
		var list []any
		for _, file := range files {
			name, outcome, _ := strings.Cut(file, ":")
			list = append(list, map[string]any{"name": name, "outcome": outcome})
		}
		run["files"] = list
	}
	for _, fields := range extra {
		maps.Copy(run, fields)
	}
	return run
}

// tickNow is a ticker that is always ready, so a wait polls again at once.
type tickNow struct{}

func (tickNow) C() <-chan time.Time {
	ch := make(chan time.Time, 1)
	ch <- time.Now()
	return ch
}

func (tickNow) Reset(time.Duration) {}

func (tickNow) Stop() {}
