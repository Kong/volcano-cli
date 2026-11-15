package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSelectTests(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
		return path
	}
	cliSpec := write("cli.yaml", `paths:
  /common:
    get:
      operationId: common
  /new:
    post:
      operationId: newOperation
`)
	hostingSpec := write("hosting.yaml", `paths:
  /common:
    get:
      operationId: common
`)
	testDir := filepath.Join(dir, "tests")
	require.NoError(t, os.Mkdir(testDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(testDir, "suite_test.go"), []byte(`package api
func TestAPIE2ESmokeOld(t *testing.T) {}
func TestAPIE2ECloudNew(t *testing.T) {}
`), 0o600))

	manifest := write("capabilities.yaml", `common: [common]
tests:
  TestAPIE2ESmokeOld: [common]
  TestAPIE2ECloudNew: [newOperation]
`)
	selected, err := selectTests(cliSpec, hostingSpec, manifest, testDir, suitePattern)
	require.NoError(t, err)
	assert.Equal(t, []string{"TestAPIE2ESmokeOld"}, selected)

	unknown := write("unknown.yaml", `common: [common]
tests:
  TestAPIE2ESmokeOld: [common]
  TestAPIE2ECloudNew: [misspelled]
`)
	_, err = selectTests(cliSpec, hostingSpec, unknown, testDir, suitePattern)
	require.ErrorContains(t, err, "unknown OpenAPI operationId misspelled")

	missing := write("missing.yaml", `common: [common]
tests:
  TestAPIE2ESmokeOld: [common]
`)
	_, err = selectTests(cliSpec, hostingSpec, missing, testDir, suitePattern)
	require.ErrorContains(t, err, "TestAPIE2ECloudNew has no capability declaration")
}
