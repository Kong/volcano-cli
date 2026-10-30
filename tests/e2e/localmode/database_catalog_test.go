package localmode

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// requireLocalModeTestsDatabaseConnections lists what a database and a
// function can be created with, and tests the new database's connection
// through the database API with the local service key. Regions are left out:
// a local database runs on the host, so local mode lists none and accepts any.
func requireLocalModeTestsDatabaseConnections(t *testing.T, binary string, env []string, dir string) {
	t.Helper()

	for _, args := range [][]string{
		{"databases", "versions", "--json"},
		{"functions", "regions", "--json"},
	} {
		var listed []map[string]any
		require.NoError(t, json.Unmarshal([]byte(runVolcanoLocalModeE2EStdout(t, binary, env, dir, args...)), &listed),
			strings.Join(args, " "))
		require.NotEmpty(t, listed, strings.Join(args, " "))
	}

	pinged := runVolcanoLocalModeE2E(t, binary, env, dir, "databases", "ping", "cli_contract")
	requireContains(t, pinged, "Connected to database 'cli_contract'")

	output, err := runVolcanoLocalModeE2EAllowFailure(t, binary, env, dir, "databases", "ping", "cli_contract", "--branch", "preview")
	if err == nil {
		t.Fatalf("expected a branch connection test to fail in local mode\n%s", output)
	}
	requireContains(t, output, "local development has no database branches")
}
