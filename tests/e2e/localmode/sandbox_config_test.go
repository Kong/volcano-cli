package localmode

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

type localSandboxSettings struct {
	Name     string `yaml:"name"`
	MemoryMB int    `yaml:"memory_mb"`
	Ports    []int  `yaml:"ports"`
	TTL      int    `yaml:"ttl_seconds"`
}

func sandboxSettingsFromManifest(data []byte, name string) (localSandboxSettings, error) {
	var manifest struct {
		Sandboxes []localSandboxSettings `yaml:"sandboxes"`
	}
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return localSandboxSettings{}, fmt.Errorf("invalid pulled manifest: %w", err)
	}
	for _, value := range manifest.Sandboxes {
		if value.Name == name {
			return value, nil
		}
	}
	return localSandboxSettings{}, fmt.Errorf("pulled manifest omitted sandbox %q", name)
}

func requireLocalSandboxConfigRoundTrip(t *testing.T, binary string, env []string, dir, template string) {
	t.Helper()
	// An explicit scratch manifest leaves the full-smoke fixture and undeclared sections untouched.
	manifest := filepath.Join(dir, "sandbox-settings.yaml")
	pulled := filepath.Join(dir, "sandbox-settings-pulled.yaml")
	pull := func() localSandboxSettings {
		t.Helper()
		runVolcanoLocalModeE2EStdout(t, binary, env, dir, "config", "pull", "--file", pulled, "--force")
		data, err := os.ReadFile(pulled)
		require.NoError(t, err)
		settings, err := sandboxSettingsFromManifest(data, "local-custom")
		require.NoError(t, err)
		return settings
	}
	before := pull()
	require.NoError(t, os.WriteFile(manifest, []byte(`version: 1
sandboxes:
  - name: local-custom
    memory_mb: 1024
    ports: [8080]
    ttl_seconds: 600
`), 0o600))
	plan := runVolcanoLocalModeE2EStdout(t, binary, env, dir, "config", "deploy", "--file", manifest, "--dry-run")
	require.Contains(t, plan, "Dry run:")
	require.Contains(t, plan, "sandboxes:")
	require.Equal(t, before, pull(), "dry run must not change template settings")
	applied := runVolcanoLocalModeE2EStdout(t, binary, env, dir, "config", "deploy", "--file", manifest)
	require.Contains(t, applied, "Configuration deployed")
	require.Equal(t, localSandboxSettings{Name: "local-custom", MemoryMB: 1024, Ports: []int{8080}, TTL: 600}, pull())
	var saved apiclient.SandboxTemplate
	require.NoError(t, json.Unmarshal([]byte(runVolcanoLocalModeE2EStdout(t, binary, env, dir,
		"sandboxes", "templates", "get", template, "--json")), &saved))
	require.Equal(t, template, saved.Id.String())
	require.Equal(t, "local-custom", saved.Name)
	require.NotNil(t, saved.MemoryMb)
	require.Equal(t, 1024, *saved.MemoryMb)

	checkLifetime := func(want time.Duration, extraArgs ...string) {
		t.Helper()
		args := append([]string{"sandboxes", "run", "--template", template, "--json"}, extraArgs...)
		var session apiclient.SandboxSession
		require.NoError(t, json.Unmarshal([]byte(runVolcanoLocalModeE2EStdout(t, binary, env, dir, args...)), &session))
		defer runVolcanoLocalModeE2EStdout(t, binary, env, dir, "sandboxes", "terminate", session.Id.String())
		if want == 0 {
			require.Nil(t, session.ExpiresAt, "unlimited local sessions must not expire")
		} else {
			require.NotNil(t, session.ExpiresAt)
			require.WithinDuration(t, session.CreatedAt.Add(want), *session.ExpiresAt, time.Second)
		}
	}
	checkLifetime(600 * time.Second)
	checkLifetime(0, "--duration", "0")
	require.NoError(t, os.WriteFile(manifest, []byte(`version: 1
sandboxes:
  - name: local-custom
    ttl_seconds: 0
`), 0o600))
	runVolcanoLocalModeE2EStdout(t, binary, env, dir, "config", "deploy", "--file", manifest)
	require.Zero(t, pull().TTL)
	checkLifetime(0)
}

func requireLocalSandboxDeploymentArtifacts(t *testing.T, run func(...string) string, template, deployment string) {
	t.Helper()
	source := run("deployments", "source", template, deployment)
	archive, err := gzip.NewReader(strings.NewReader(source))
	require.NoError(t, err, "source must be an uncontaminated gzip archive")
	defer archive.Close()
	tree := tar.NewReader(archive)
	found := false
	for {
		header, err := tree.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		if header.Name == "Dockerfile" {
			data, err := io.ReadAll(tree)
			require.NoError(t, err)
			require.Contains(t, string(data), "RUN printf custom-image > /image-version")
			found = true
		}
	}
	require.True(t, found, "exported source must contain the original Dockerfile")
	for _, region := range []string{"us-east-1", "aws-us-east-1"} {
		var logs apiclient.SandboxBuildLogPage
		require.NoError(t, json.Unmarshal([]byte(run("deployments", "logs", template, deployment,
			"--region", region, "--json")), &logs))
		require.NotEmpty(t, logs.Data, "build output must be visible in region %s", region)
		hasMessage := false
		for _, event := range logs.Data {
			require.False(t, event.Timestamp.IsZero())
			if strings.TrimSpace(event.Message) != "" {
				hasMessage = true
			}
		}
		require.True(t, hasMessage, "build output must include a nonblank message in region %s", region)
	}
}

func TestSandboxSettingsFromManifest(t *testing.T) {
	data := []byte("version: 1\nvariables:\n  - name: retained\nsandboxes:\n  - name: other\n  - name: local-custom\n    memory_mb: 1024\n    ports: [8080]\n    ttl_seconds: 600\n")
	settings, err := sandboxSettingsFromManifest(data, "local-custom")
	require.NoError(t, err)
	require.Equal(t, localSandboxSettings{Name: "local-custom", MemoryMB: 1024, Ports: []int{8080}, TTL: 600}, settings)
	_, err = sandboxSettingsFromManifest(data, "missing")
	require.ErrorContains(t, err, "omitted sandbox")
	_, err = sandboxSettingsFromManifest([]byte("invalid: ["), "local-custom")
	require.ErrorContains(t, err, "invalid pulled manifest")
}
