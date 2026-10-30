package localmode

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// requireLocalModeListsCreateOptions lists what a database and a function can
// be created with. Database regions are left out: a local database runs on the
// host, so local mode lists none and accepts any.
func requireLocalModeListsCreateOptions(t *testing.T, binary string, env []string, dir string) {
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
}
