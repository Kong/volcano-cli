package projectconfig

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/sensitive"
)

func TestParseMarksValueQuotingErrorsSensitive(t *testing.T) {
	noEnv := func(string) (string, bool) { return "", false }
	for name, manifest := range map[string]string{
		"interpolation": "auth:\n  smtp:\n    password: 'hunter2${'\n",
		"yaml syntax":   "auth: [hunter2\n",
		"decode":        "functions: hunter2\n",
		"validation":    "functions:\n  - name: hello\n    kind: hunter2\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(manifest), noEnv)
			require.Error(t, err)
			assert.NotEmpty(t, sensitive.Text(err))
		})
	}
}
