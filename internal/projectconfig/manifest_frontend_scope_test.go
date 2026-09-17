package projectconfig

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseFrontendVariableScope(t *testing.T) {
	for _, source := range []string{
		"version: 1\nfrontends:\n  - name: web\n    variable_scope: scoped\n    variables: [NEXT_PUBLIC_URL]\n",
		"version: 1\nfrontends:\n  - name: web\n    variable_scope: scoped\n    variables: []\n",
	} {
		manifest, err := Parse([]byte(source), noEnv)
		require.NoError(t, err)
		body, err := manifest.uploadBody()
		require.NoError(t, err)
		var decoded struct {
			Frontends []struct {
				VariableScope string   `json:"variable_scope"`
				Variables     []string `json:"variables"`
			} `json:"frontends"`
		}
		require.NoError(t, json.Unmarshal(body, &decoded))
		require.Equal(t, "scoped", decoded.Frontends[0].VariableScope)
		require.NotNil(t, decoded.Frontends[0].Variables)
	}
}
