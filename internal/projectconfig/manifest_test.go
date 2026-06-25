package projectconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidate(t *testing.T) {
	limit := int64(1024)
	zero := int64(0)
	pub := true
	priv := false

	tests := []struct {
		name        string
		manifest    Manifest
		errContains string
	}{
		{
			name: "valid buckets only",
			manifest: Manifest{
				Version: 1,
				Buckets: []BucketManifest{{Name: "uploads"}},
			},
		},
		{
			name: "valid functions only",
			manifest: Manifest{
				Version:   1,
				Functions: []FunctionManifest{{Name: "hello", Public: &pub}},
			},
		},
		{
			name: "version 0 rejected",
			manifest: Manifest{
				Buckets: []BucketManifest{{Name: "uploads"}},
			},
			errContains: "unsupported manifest version 0",
		},
		{
			name:        "version 2 rejected",
			manifest:    Manifest{Version: 2, Buckets: []BucketManifest{{Name: "uploads"}}},
			errContains: "unsupported manifest version 2",
		},
		{
			name:        "empty manifest rejected",
			manifest:    Manifest{Version: 1},
			errContains: "must include at least one bucket or function",
		},
		{
			name: "duplicate bucket name",
			manifest: Manifest{
				Version: 1,
				Buckets: []BucketManifest{{Name: "uploads"}, {Name: "uploads"}},
			},
			errContains: `duplicate bucket name "uploads"`,
		},
		{
			name: "blank bucket name",
			manifest: Manifest{
				Version: 1,
				Buckets: []BucketManifest{{Name: "   "}},
			},
			errContains: "bucket name is required",
		},
		{
			name: "non-positive file size limit",
			manifest: Manifest{
				Version: 1,
				Buckets: []BucketManifest{{Name: "uploads", FileSizeLimit: &zero}},
			},
			errContains: "file_size_limit must be greater than 0",
		},
		{
			name: "all-empty allowed mime types",
			manifest: Manifest{
				Version: 1,
				Buckets: []BucketManifest{
					{Name: "uploads", AllowedMimeTypes: &[]string{"", "  "}},
				},
			},
			errContains: "contains only empty values",
		},
		{
			name: "duplicate policy name within bucket",
			manifest: Manifest{
				Version: 1,
				Buckets: []BucketManifest{{
					Name: "uploads",
					Policies: []PolicyManifest{
						{Name: "owner", Operation: "select", Definition: "true"},
						{Name: "owner", Operation: "insert", Definition: "true"},
					},
				}},
			},
			errContains: `duplicate policy name "owner"`,
		},
		{
			name: "invalid policy operation",
			manifest: Manifest{
				Version: 1,
				Buckets: []BucketManifest{{
					Name: "uploads",
					Policies: []PolicyManifest{
						{Name: "owner", Operation: "READ", Definition: "true"},
					},
				}},
			},
			errContains: "operation must be SELECT, INSERT, UPDATE, or DELETE",
		},
		{
			name: "missing policy definition",
			manifest: Manifest{
				Version: 1,
				Buckets: []BucketManifest{{
					Name: "uploads",
					Policies: []PolicyManifest{
						{Name: "owner", Operation: "select"},
					},
				}},
			},
			errContains: "definition is required",
		},
		{
			name: "missing function public flag and no schedulers",
			manifest: Manifest{
				Version:   1,
				Functions: []FunctionManifest{{Name: "hello"}},
			},
			errContains: `function "hello": must set 'public' or declare at least one scheduler`,
		},
		{
			name: "function with schedulers only (no public)",
			manifest: Manifest{
				Version: 1,
				Functions: []FunctionManifest{{
					Name: "hello",
					Schedulers: []SchedulerManifest{{
						Name: "daily",
						Cron: "0 0 * * *",
					}},
				}},
			},
		},
		{
			name: "function with both public and schedulers",
			manifest: Manifest{
				Version: 1,
				Functions: []FunctionManifest{{
					Name:   "hello",
					Public: &pub,
					Schedulers: []SchedulerManifest{{
						Name: "hourly",
						Cron: "0 * * * *",
					}},
				}},
			},
		},
		{
			name: "scheduler missing name",
			manifest: Manifest{
				Version: 1,
				Functions: []FunctionManifest{{
					Name:   "hello",
					Public: &pub,
					Schedulers: []SchedulerManifest{{
						Cron: "0 0 * * *",
					}},
				}},
			},
			errContains: `function "hello": scheduler name is required`,
		},
		{
			name: "scheduler missing cron",
			manifest: Manifest{
				Version: 1,
				Functions: []FunctionManifest{{
					Name:   "hello",
					Public: &pub,
					Schedulers: []SchedulerManifest{{
						Name: "daily",
					}},
				}},
			},
			errContains: `function "hello" scheduler "daily": cron is required`,
		},
		{
			name: "duplicate scheduler name",
			manifest: Manifest{
				Version: 1,
				Functions: []FunctionManifest{{
					Name:   "hello",
					Public: &pub,
					Schedulers: []SchedulerManifest{
						{Name: "daily", Cron: "0 0 * * *"},
						{Name: "daily", Cron: "0 12 * * *"},
					},
				}},
			},
			errContains: `function "hello": duplicate scheduler name "daily"`,
		},
		{
			name: "duplicate function name",
			manifest: Manifest{
				Version: 1,
				Functions: []FunctionManifest{
					{Name: "hello", Public: &pub},
					{Name: "hello", Public: &priv},
				},
			},
			errContains: `duplicate function name "hello"`,
		},
		{
			name: "scheduler with full fields",
			manifest: Manifest{
				Version: 1,
				Functions: []FunctionManifest{{
					Name:   "hello",
					Public: &pub,
					Schedulers: []SchedulerManifest{{
						Name:    "daily",
						Cron:    "0 0 * * *",
						Enabled: &pub,
						Payload: map[string]any{"key": "value"},
						Regions: []string{"us-east-1"},
					}},
				}},
			},
		},
		{
			name: "normalization: operation lowercase, file size limit kept",
			manifest: Manifest{
				Version: 1,
				Buckets: []BucketManifest{{
					Name:          "uploads",
					FileSizeLimit: &limit,
					Policies: []PolicyManifest{
						{Name: "owner", Operation: "select", Definition: "true"},
					},
				}},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.manifest
			err := m.Validate()
			if tc.errContains != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errContains)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestValidateNormalizesPolicyOperationAndMIME(t *testing.T) {
	m := Manifest{
		Version: 1,
		Buckets: []BucketManifest{{
			Name:             "uploads",
			AllowedMimeTypes: &[]string{"image/png", " ", "image/jpeg"},
			Policies: []PolicyManifest{
				{Name: "owner", Operation: " select ", Definition: " true "},
			},
		}},
	}
	require.NoError(t, m.Validate())

	bucket := m.Buckets[0]
	require.NotNil(t, bucket.AllowedMimeTypes)
	assert.Equal(t, []string{"image/png", "image/jpeg"}, *bucket.AllowedMimeTypes)
	assert.Equal(t, "SELECT", bucket.Policies[0].Operation)
	assert.Equal(t, "true", bucket.Policies[0].Definition)
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`version: 1
buckets:
  - name: uploads
    file_size_limit: 2048
    allowed_mime_types:
      - image/png
    policies:
      - name: owner
        operation: select
        definition: "auth.uid() = owner_id"
functions:
  - name: hello
    public: true
`), 0o644))

	manifest, resolved, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, path, resolved)
	require.Len(t, manifest.Buckets, 1)
	assert.Equal(t, "uploads", manifest.Buckets[0].Name)
	require.NotNil(t, manifest.Buckets[0].FileSizeLimit)
	assert.EqualValues(t, 2048, *manifest.Buckets[0].FileSizeLimit)
	require.NotNil(t, manifest.Buckets[0].AllowedMimeTypes)
	assert.Equal(t, []string{"image/png"}, *manifest.Buckets[0].AllowedMimeTypes)
	require.Len(t, manifest.Buckets[0].Policies, 1)
	assert.Equal(t, "SELECT", manifest.Buckets[0].Policies[0].Operation)
	require.Len(t, manifest.Functions, 1)
	require.NotNil(t, manifest.Functions[0].Public)
	assert.True(t, *manifest.Functions[0].Public)
}

func TestLoadEmptyPath(t *testing.T) {
	_, _, err := Load("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "file path is required")
}

func TestLoadInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.yaml")
	require.NoError(t, os.WriteFile(path, []byte("not: valid: yaml: :"), 0o644))

	_, _, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse YAML")
}

func TestLoadMissingFile(t *testing.T) {
	dir := t.TempDir()
	_, _, err := Load(filepath.Join(dir, "absent.yaml"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read configuration file")
}

func TestLoadValidationFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.yaml")
	require.NoError(t, os.WriteFile(path, []byte("version: 1\n"), 0o644))

	_, _, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must include at least one bucket or function")
	assert.NotContains(t, err.Error(), "failed to parse")
}

// withTempWorkingDir chdirs into a fresh temp directory for the test body and
// restores the original cwd afterwards. ResolveManifestPath inspects the cwd,
// so tests must isolate themselves from each other and from the developer's
// repo layout.
func withTempWorkingDir(t *testing.T, fn func(tempDir string)) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)

	resolved, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	fn(resolved)
}

func TestResolveManifestPath_DefaultsToNestedFile(t *testing.T) {
	withTempWorkingDir(t, func(_ string) {
		require.NoError(t, os.MkdirAll("volcano", 0o755))
		require.NoError(t, os.WriteFile(filepath.Join("volcano", "volcano-config.yaml"), []byte("version: 1\n"), 0o644))

		got, err := ResolveManifestPath("")
		require.NoError(t, err)
		assert.Equal(t, filepath.Join("volcano", "volcano-config.yaml"), got)
	})
}

func TestResolveManifestPath_DefaultsToRootFile(t *testing.T) {
	withTempWorkingDir(t, func(_ string) {
		require.NoError(t, os.WriteFile("volcano-config.yaml", []byte("version: 1\n"), 0o644))

		got, err := ResolveManifestPath("")
		require.NoError(t, err)
		assert.Equal(t, "volcano-config.yaml", got)
	})
}

func TestResolveManifestPath_ErrorsWhenBothExist(t *testing.T) {
	withTempWorkingDir(t, func(_ string) {
		require.NoError(t, os.WriteFile("volcano-config.yaml", []byte("version: 1\n"), 0o644))
		require.NoError(t, os.MkdirAll("volcano", 0o755))
		require.NoError(t, os.WriteFile(filepath.Join("volcano", "volcano-config.yaml"), []byte("version: 1\n"), 0o644))

		_, err := ResolveManifestPath("")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "found multiple volcano-config.yaml files")
		assert.Contains(t, err.Error(), "keep only one volcano-config.yaml file")
	})
}

func TestResolveManifestPath_UsesExplicitFile(t *testing.T) {
	withTempWorkingDir(t, func(_ string) {
		require.NoError(t, os.MkdirAll("config", 0o755))
		path := filepath.Join("config", "custom-config.yaml")
		require.NoError(t, os.WriteFile(path, []byte("version: 1\n"), 0o644))

		got, err := ResolveManifestPath(path)
		require.NoError(t, err)
		assert.Equal(t, path, got)
	})
}

func TestResolveManifestPath_ExplicitMissing(t *testing.T) {
	withTempWorkingDir(t, func(_ string) {
		_, err := ResolveManifestPath("config/missing.yaml")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "specified file not found")
	})
}

func TestResolveManifestPath_ErrorsWhenMissing(t *testing.T) {
	withTempWorkingDir(t, func(_ string) {
		_, err := ResolveManifestPath("")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no volcano-config.yaml file found")
	})
}
