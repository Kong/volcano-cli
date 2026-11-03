package projectconfig

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSchedulersOnlyKeepsJustTheNamedFunctionsSchedulers(t *testing.T) {
	manifest, err := Parse([]byte(`version: 1
project:
  selected_regions: [us-east-1]
auth:
  email:
    enabled: true
functions:
  - name: hello
    visibility: public
    schedulers:
      - name: every-minute
        cron: "* * * * *"
  - name: order-pipeline
    kind: durable
    visibility: private
    schedulers:
      - name: nightly
        cron: "0 0 * * *"
  - name: other
    schedulers:
      - name: hourly
        cron: "0 * * * *"
`), noEnv)
	require.NoError(t, err)

	subset, ok := SchedulersOnly([]*Manifest{manifest}, map[string]bool{"hello": true, "order-pipeline": true})
	require.True(t, ok)
	body, err := subset.uploadBody()
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"version": 1,
		"functions": [
			{"name": "hello", "schedulers": [{"name": "every-minute", "cron": "* * * * *"}]},
			{"name": "order-pipeline", "kind": "durable", "schedulers": [{"name": "nightly", "cron": "0 0 * * *"}]}
		]
	}`, string(body))
	assert.Nil(t, subset.Project)
	assert.Nil(t, subset.Auth)
}

func TestSchedulersOnlyTakesTheLastManifestThatListsThem(t *testing.T) {
	base, err := Parse([]byte(`version: 1
functions:
  - name: hello
    schedulers:
      - name: every-minute
        cron: "* * * * *"
`), noEnv)
	require.NoError(t, err)
	overlay, err := Parse([]byte(`version: 1
functions:
  - name: hello
    schedulers:
      - name: hourly
        cron: "0 * * * *"
`), noEnv)
	require.NoError(t, err)
	visibility, err := Parse([]byte("version: 1\nfunctions:\n  - name: hello\n    visibility: private\n"), noEnv)
	require.NoError(t, err)

	subset, ok := SchedulersOnly([]*Manifest{base, overlay, visibility}, map[string]bool{"hello": true})
	require.True(t, ok)
	body, err := subset.uploadBody()
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"version": 1,
		"functions": [{"name": "hello", "schedulers": [{"name": "hourly", "cron": "0 * * * *"}]}]
	}`, string(body))
}

func TestSchedulersOnlyWithoutSchedulersForTheNamedFunctions(t *testing.T) {
	manifest, err := Parse([]byte("version: 1\nfunctions:\n  - name: hello\n    visibility: public\n"), noEnv)
	require.NoError(t, err)

	_, ok := SchedulersOnly([]*Manifest{manifest}, map[string]bool{"hello": true})
	assert.False(t, ok)
}
