package localmode

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// requireLocalModeListsDeployments reads the deployment of hello that
// `functions deploy --all` just made through the project-wide deployment
// commands.
func requireLocalModeListsDeployments(t *testing.T, binary string, env []string, dir string) {
	t.Helper()

	var listed []apiclient.ProjectDeployment
	require.NoError(t, json.Unmarshal([]byte(runVolcanoLocalModeE2EStdout(t, binary, env, dir,
		"deployments", "list", "--type", "function", "--name", "hello", "--json")), &listed))
	require.NotEmpty(t, listed, "functions deploy --all must record a deployment of hello")
	newest := listed[0]
	require.Equal(t, "hello", newest.Resource.Name)

	shown := runVolcanoLocalModeE2E(t, binary, env, dir, "deployments", "get", newest.Id.String())
	requireContains(t, shown, "Resource: function hello")
	requireContains(t, shown, newest.Id.String())

	var summary apiclient.ProjectDeploymentSummary
	require.NoError(t, json.Unmarshal([]byte(runVolcanoLocalModeE2EStdout(t, binary, env, dir,
		"deployments", "summary", "--json")), &summary))
	require.Positive(t, summary.DeploymentCount)
}
