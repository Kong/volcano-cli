package projectconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func noEnv(string) (string, bool) { return "", false }

func envMap(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}

func TestParseFullManifest(t *testing.T) {
	manifest, err := Parse([]byte(`version: 1
project:
  name: my-app
  all_regions: false
  selected_regions: [us-east-1, us-west-2]
databases:
  - name: appdb
    region: aws-us-east-1
    pg_version: "16"
    database_type: volcano-db-xs
variables:
  - name: STRIPE_SECRET_KEY
    value: sk_test_123
buckets:
  - name: avatars
    file_size_limit: 5242880
    allowed_mime_types: [image/png]
    policies:
      - name: public-read
        operation: SELECT
        definition: "true"
realtime:
  enabled: true
  broadcast_enabled: false
auth:
  tokens:
    access_token_lifetime: 3600
  signup:
    enable_signup: true
  providers:
    email_password:
      enabled: true
    oauth:
      - provider: google
        enabled: true
        client_id: cid
        client_secret: secret
        redirect_url: https://api.myapp.com/callback
        scopes: [openid, email]
      - provider: device
        enabled: true
  email:
    enabled: true
    from:
      address: no-reply@myapp.com
      name: My App
    smtp:
      host: smtp.example.com
      port: 587
      username: mailer
      password: hunter2
      use_tls: true
    templates:
      confirmation:
        subject: "Confirm"
        html_body: "<p>{{.Token}}</p>"
        text_body: "{{.Token}}"
      welcome:
        subject: "Welcome"
  managed_pages:
    enabled: true
    redirects:
      allowed: [https://myapp.com/welcome]
      post_auth: https://myapp.com/welcome
    pages:
      login:
        html: "<html></html>"
        css: "body{}"
functions:
  - name: hello
    public: true
    schedulers:
      - name: nightly
        cron: "0 3 * * *"
        enabled: true
        payload: { source: cron }
frontends:
  - name: web
    custom_domain:
      domain: app.myapp.com
      tls:
        mode: byoc
        certificate_pem: CERT
        private_key_pem: KEY
        certificate_chain_pem: CHAIN
`), noEnv)
	require.NoError(t, err)

	assert.Equal(t, 1, manifest.Version)
	require.NotNil(t, manifest.Project)
	assert.Equal(t, "my-app", *manifest.Project.Name)
	require.NotNil(t, manifest.Project.AllRegions)
	assert.False(t, *manifest.Project.AllRegions)
	assert.Equal(t, []string{"us-east-1", "us-west-2"}, *manifest.Project.SelectedRegions)

	require.NotNil(t, manifest.Databases)
	require.Len(t, *manifest.Databases, 1)
	assert.Equal(t, "16", (*manifest.Databases)[0].PgVersion)
	assert.Equal(t, "volcano-db-xs", *(*manifest.Databases)[0].DatabaseType)

	require.NotNil(t, manifest.Variables)
	assert.Equal(t, "sk_test_123", (*manifest.Variables)[0].Value)

	require.NotNil(t, manifest.Buckets)
	bucket := (*manifest.Buckets)[0]
	assert.EqualValues(t, 5242880, *bucket.FileSizeLimit)
	require.NotNil(t, bucket.Policies)
	assert.Equal(t, "SELECT", (*bucket.Policies)[0].Operation)

	require.NotNil(t, manifest.Realtime)
	assert.True(t, *manifest.Realtime.Enabled)
	assert.False(t, *manifest.Realtime.BroadcastEnabled)
	assert.Nil(t, manifest.Realtime.PresenceEnabled)

	require.NotNil(t, manifest.Auth)
	assert.Equal(t, 3600, *manifest.Auth.Tokens.AccessTokenLifetime)
	require.NotNil(t, manifest.Auth.Providers.Oauth)
	oauth := *manifest.Auth.Providers.Oauth
	require.Len(t, oauth, 2)
	assert.Equal(t, "google", oauth[0].Provider)
	assert.Equal(t, "secret", *oauth[0].ClientSecret)
	assert.Equal(t, "device", oauth[1].Provider)
	assert.Equal(t, "hunter2", *manifest.Auth.Email.SMTP.Password)
	assert.Equal(t, "Confirm", *manifest.Auth.Email.Templates.Confirmation.Subject)
	assert.Nil(t, manifest.Auth.Email.Templates.PasswordReset)
	assert.Equal(t, "<html></html>", manifest.Auth.ManagedPages.Pages.Login.HTML)

	require.NotNil(t, manifest.Functions)
	function := (*manifest.Functions)[0]
	assert.True(t, *function.Public)
	require.NotNil(t, function.Schedulers)
	scheduler := (*function.Schedulers)[0]
	assert.Equal(t, "0 3 * * *", scheduler.Cron)
	require.NotNil(t, scheduler.Payload)
	assert.Equal(t, map[string]any{"source": "cron"}, *scheduler.Payload)

	require.NotNil(t, manifest.Frontends)
	frontend := (*manifest.Frontends)[0]
	require.NotNil(t, frontend.CustomDomain)
	assert.Equal(t, "app.myapp.com", frontend.CustomDomain.Domain)
	require.NotNil(t, frontend.CustomDomain.TLS)
	assert.Equal(t, "byoc", frontend.CustomDomain.TLS.Mode)
	assert.Equal(t, "CHAIN", *frontend.CustomDomain.TLS.CertificateChainPEM)
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name        string
		yaml        string
		errContains string
	}{
		{
			name:        "version 0 rejected",
			yaml:        "buckets:\n  - name: uploads\n",
			errContains: "unsupported manifest version 0",
		},
		{
			name:        "version 2 rejected",
			yaml:        "version: 2\n",
			errContains: "unsupported manifest version 2",
		},
		{
			name:        "empty manifest",
			yaml:        "",
			errContains: "manifest is empty",
		},
		{
			name:        "invalid yaml",
			yaml:        "not: valid: yaml: :",
			errContains: "failed to parse YAML",
		},
		{
			name:        "unknown field rejected",
			yaml:        "version: 1\nbuckets:\n  - name: uploads\n    file_size: 10\n",
			errContains: "field file_size not found",
		},
		{
			name: "scheduler regions rejected",
			yaml: `version: 1
functions:
  - name: hello
    schedulers:
      - name: nightly
        cron: "0 3 * * *"
        regions: [aws-us-east-1]
`,
			errContains: `scheduler placement is managed by the server`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.yaml), noEnv)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.errContains)
		})
	}
}

func TestParseInterpolation(t *testing.T) {
	lookup := envMap(map[string]string{
		"SECRET":   "s3cr3t",
		"EMPTY":    "",
		"REGION_1": "us-east-1",
	})

	t.Run("values interpolated", func(t *testing.T) {
		manifest, err := Parse([]byte(`version: 1
variables:
  - name: API_KEY
    value: ${SECRET}
  - name: COMPOSED
    value: pre-${SECRET}-post
  - name: SET_BUT_EMPTY
    value: ${EMPTY}
`), lookup)
		require.NoError(t, err)
		variables := *manifest.Variables
		assert.Equal(t, "s3cr3t", variables[0].Value)
		assert.Equal(t, "pre-s3cr3t-post", variables[1].Value)
		assert.Empty(t, variables[2].Value)
	})

	t.Run("dollar escape", func(t *testing.T) {
		manifest, err := Parse([]byte(`version: 1
variables:
  - name: LITERAL
    value: cost is $$5 for ${SECRET}
  - name: LONE_DOLLAR
    value: 5$ and $techno
`), lookup)
		require.NoError(t, err)
		variables := *manifest.Variables
		assert.Equal(t, "cost is $5 for s3cr3t", variables[0].Value)
		assert.Equal(t, "5$ and $techno", variables[1].Value)
	})

	t.Run("interpolates nested strings", func(t *testing.T) {
		manifest, err := Parse([]byte(`version: 1
auth:
  email:
    smtp:
      password: ${SECRET}
functions:
  - name: hello
    schedulers:
      - name: nightly
        cron: "0 3 * * *"
        payload: { key: "${SECRET}" }
project:
  selected_regions: ["${REGION_1}"]
`), lookup)
		require.NoError(t, err)
		assert.Equal(t, "s3cr3t", *manifest.Auth.Email.SMTP.Password)
		payload := *(*(*manifest.Functions)[0].Schedulers)[0].Payload
		assert.Equal(t, "s3cr3t", payload["key"])
		assert.Equal(t, []string{"us-east-1"}, *manifest.Project.SelectedRegions)
	})

	t.Run("keys are not interpolated", func(t *testing.T) {
		manifest, err := Parse([]byte(`version: 1
functions:
  - name: hello
    schedulers:
      - name: nightly
        cron: "0 3 * * *"
        payload:
          "${SECRET}": raw-key
`), lookup)
		require.NoError(t, err)
		payload := *(*(*manifest.Functions)[0].Schedulers)[0].Payload
		assert.Equal(t, "raw-key", payload["${SECRET}"])
	})

	t.Run("missing variable is an error", func(t *testing.T) {
		_, err := Parse([]byte("version: 1\nvariables:\n  - name: A\n    value: ${MISSING_VAR}\n"), lookup)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `environment variable "MISSING_VAR" is not set`)
	})

	t.Run("unterminated reference is an error", func(t *testing.T) {
		_, err := Parse([]byte("version: 1\nvariables:\n  - name: A\n    value: ${SECRET\n"), lookup)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unterminated ${...} reference")
	})

	t.Run("empty reference is an error", func(t *testing.T) {
		_, err := Parse([]byte("version: 1\nvariables:\n  - name: A\n    value: ${}\n"), lookup)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty ${} reference")
	})

	t.Run("non-string scalars untouched", func(t *testing.T) {
		manifest, err := Parse([]byte("version: 1\nrealtime:\n  enabled: true\n"), noEnv)
		require.NoError(t, err)
		assert.True(t, *manifest.Realtime.Enabled)
	})
}

// TestManifestJSONShape guards the JSON contract with the apply endpoint:
// omitted sections are absent, declared-empty lists serialize as [] (full
// sync deletes everything), and the rejected regions field never uploads.
func TestManifestJSONShape(t *testing.T) {
	manifest, err := Parse([]byte(`version: 1
variables: []
buckets:
  - name: avatars
    policies: []
functions:
  - name: hello
    schedulers:
      - name: nightly
        cron: "0 3 * * *"
`), noEnv)
	require.NoError(t, err)

	encoded, err := manifest.uploadBody()
	require.NoError(t, err)

	var body map[string]any
	require.NoError(t, json.Unmarshal(encoded, &body))

	assert.EqualValues(t, 1, body["version"])
	assert.Equal(t, []any{}, body["variables"])
	assert.NotContains(t, body, "auth")
	assert.NotContains(t, body, "realtime")
	assert.NotContains(t, body, "project")
	assert.NotContains(t, body, "databases")
	assert.NotContains(t, body, "frontends")

	buckets, ok := body["buckets"].([]any)
	require.True(t, ok)
	bucket, ok := buckets[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, []any{}, bucket["policies"])
	assert.NotContains(t, bucket, "file_size_limit")

	functions, ok := body["functions"].([]any)
	require.True(t, ok)
	function, ok := functions[0].(map[string]any)
	require.True(t, ok)
	schedulers, ok := function["schedulers"].([]any)
	require.True(t, ok)
	scheduler, ok := schedulers[0].(map[string]any)
	require.True(t, ok)
	assert.NotContains(t, scheduler, "regions")
	assert.NotContains(t, scheduler, "enabled")
}

// TestManifestUploadPreservesVariableValueAbsence guards that an omitted variable
// value uploads as an absent field (so the server's required-field validation
// rejects the typo) rather than an empty string that would silently clear the
// variable, while an explicit empty value is preserved as such.
func TestManifestUploadPreservesVariableValueAbsence(t *testing.T) {
	uploadedVariable := func(t *testing.T, yamlDoc string) map[string]any {
		t.Helper()
		manifest, err := Parse([]byte(yamlDoc), noEnv)
		require.NoError(t, err)
		body, err := manifest.uploadBody()
		require.NoError(t, err)
		var decoded map[string]any
		require.NoError(t, json.Unmarshal(body, &decoded))
		variables, ok := decoded["variables"].([]any)
		require.True(t, ok)
		variable, ok := variables[0].(map[string]any)
		require.True(t, ok)
		return variable
	}

	t.Run("omitted value stays absent", func(t *testing.T) {
		variable := uploadedVariable(t, "version: 1\nvariables:\n  - name: API_KEY\n")
		assert.Equal(t, "API_KEY", variable["name"])
		assert.NotContains(t, variable, "value")
	})

	t.Run("explicit empty value is preserved", func(t *testing.T) {
		variable := uploadedVariable(t, "version: 1\nvariables:\n  - name: API_KEY\n    value: \"\"\n")
		assert.Contains(t, variable, "value")
		assert.Empty(t, variable["value"])
	})
}

// TestManifestUploadFullShape verifies the apply request body emits the complete
// interpolated manifest with the server's snake_case keys and preserves declared
// values across every section, including explicit false booleans — which must
// reach the server rather than be dropped as "empty". This guards the
// generic-shape upload path against silently dropping, renaming, or retyping
// fields.
func TestManifestUploadFullShape(t *testing.T) {
	manifest, err := Parse([]byte(`version: 1
project:
  name: my-app
  all_regions: false
  selected_regions: [us-east-1, us-west-2]
databases:
  - name: appdb
    region: aws-us-east-1
    pg_version: "16"
    database_type: volcano-db-xs
variables:
  - name: STRIPE_SECRET_KEY
    value: sk_test_123
buckets:
  - name: avatars
    file_size_limit: 5242880
    allowed_mime_types: [image/png]
    policies:
      - name: public-read
        operation: SELECT
        definition: "true"
realtime:
  enabled: true
  broadcast_enabled: false
auth:
  tokens:
    access_token_lifetime: 3600
  providers:
    oauth:
      - provider: google
        enabled: true
        client_id: cid
        scopes: [openid, email]
  email:
    smtp:
      host: smtp.example.com
      port: 587
  managed_pages:
    pages:
      login:
        html: "<html></html>"
        css: "body{}"
functions:
  - name: hello
    public: true
    schedulers:
      - name: nightly
        cron: "0 3 * * *"
        enabled: true
        payload: { source: cron }
frontends:
  - name: web
    custom_domain:
      domain: app.myapp.com
      tls:
        mode: byoc
        certificate_pem: CERT
        private_key_pem: KEY
`), noEnv)
	require.NoError(t, err)

	body, err := manifest.uploadBody()
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(body, &m))

	asMap := func(v any) map[string]any {
		got, ok := v.(map[string]any)
		require.True(t, ok, "expected object, got %T", v)
		return got
	}
	first := func(v any) any {
		got, ok := v.([]any)
		require.True(t, ok, "expected array, got %T", v)
		require.NotEmpty(t, got)
		return got[0]
	}

	assert.EqualValues(t, 1, m["version"])

	// project: an explicitly declared false must be emitted, not dropped.
	project := asMap(m["project"])
	assert.Equal(t, "my-app", project["name"])
	assert.Equal(t, false, project["all_regions"])
	assert.Equal(t, []any{"us-east-1", "us-west-2"}, project["selected_regions"])

	db := asMap(first(m["databases"]))
	assert.Equal(t, "appdb", db["name"])
	assert.Equal(t, "16", db["pg_version"])
	assert.Equal(t, "volcano-db-xs", db["database_type"])

	variable := asMap(first(m["variables"]))
	assert.Equal(t, "STRIPE_SECRET_KEY", variable["name"])
	assert.Equal(t, "sk_test_123", variable["value"])

	bucket := asMap(first(m["buckets"]))
	assert.EqualValues(t, 5242880, bucket["file_size_limit"])
	assert.Equal(t, []any{"image/png"}, bucket["allowed_mime_types"])
	policy := asMap(first(bucket["policies"]))
	assert.Equal(t, "SELECT", policy["operation"])
	assert.Equal(t, "true", policy["definition"])

	realtime := asMap(m["realtime"])
	assert.Equal(t, true, realtime["enabled"])
	assert.Equal(t, false, realtime["broadcast_enabled"])
	assert.NotContains(t, realtime, "presence_enabled")

	auth := asMap(m["auth"])
	assert.EqualValues(t, 3600, asMap(auth["tokens"])["access_token_lifetime"])
	oauth := asMap(first(asMap(auth["providers"])["oauth"]))
	assert.Equal(t, "google", oauth["provider"])
	assert.Equal(t, true, oauth["enabled"])
	assert.Equal(t, "cid", oauth["client_id"])
	assert.Equal(t, []any{"openid", "email"}, oauth["scopes"])
	smtp := asMap(asMap(auth["email"])["smtp"])
	assert.Equal(t, "smtp.example.com", smtp["host"])
	assert.EqualValues(t, 587, smtp["port"])
	login := asMap(asMap(asMap(auth["managed_pages"])["pages"])["login"])
	assert.Equal(t, "<html></html>", login["html"])
	assert.Equal(t, "body{}", login["css"])

	function := asMap(first(m["functions"]))
	assert.Equal(t, "hello", function["name"])
	assert.Equal(t, true, function["public"])
	scheduler := asMap(first(function["schedulers"]))
	assert.Equal(t, "0 3 * * *", scheduler["cron"])
	assert.Equal(t, true, scheduler["enabled"])
	assert.Equal(t, map[string]any{"source": "cron"}, scheduler["payload"])

	customDomain := asMap(asMap(first(m["frontends"]))["custom_domain"])
	assert.Equal(t, "app.myapp.com", customDomain["domain"])
	tls := asMap(customDomain["tls"])
	assert.Equal(t, "byoc", tls["mode"])
	assert.Equal(t, "CERT", tls["certificate_pem"])
	assert.Equal(t, "KEY", tls["private_key_pem"])
	assert.NotContains(t, tls, "certificate_chain_pem")
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`version: 1
variables:
  - name: KEY
    value: literal
functions:
  - name: hello
    public: true
`), 0o644))

	manifest, resolved, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, path, resolved)
	require.NotNil(t, manifest.Variables)
	assert.Equal(t, "literal", (*manifest.Variables)[0].Value)
	require.NotNil(t, manifest.Functions)
	assert.True(t, *(*manifest.Functions)[0].Public)
}

func TestLoadInterpolatesFromProcessEnv(t *testing.T) {
	t.Setenv("VOLCANO_TEST_MANIFEST_SECRET", "from-env")
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.yaml")
	require.NoError(t, os.WriteFile(path, []byte("version: 1\nvariables:\n  - name: KEY\n    value: ${VOLCANO_TEST_MANIFEST_SECRET}\n"), 0o644))

	manifest, _, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "from-env", (*manifest.Variables)[0].Value)
}

func TestLoadEmptyPath(t *testing.T) {
	_, _, err := Load("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "file path is required")
}

func TestLoadMissingFile(t *testing.T) {
	dir := t.TempDir()
	_, _, err := Load(filepath.Join(dir, "absent.yaml"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read configuration file")
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

func TestDefaultPullPath(t *testing.T) {
	t.Run("existing nested manifest wins", func(t *testing.T) {
		withTempWorkingDir(t, func(_ string) {
			require.NoError(t, os.MkdirAll("volcano", 0o755))
			require.NoError(t, os.WriteFile(filepath.Join("volcano", "volcano-config.yaml"), []byte("version: 1\n"), 0o644))
			assert.Equal(t, filepath.Join("volcano", "volcano-config.yaml"), DefaultPullPath())
		})
	})

	t.Run("existing root manifest wins", func(t *testing.T) {
		withTempWorkingDir(t, func(_ string) {
			require.NoError(t, os.WriteFile("volcano-config.yaml", []byte("version: 1\n"), 0o644))
			assert.Equal(t, "volcano-config.yaml", DefaultPullPath())
		})
	})

	t.Run("volcano directory preferred", func(t *testing.T) {
		withTempWorkingDir(t, func(_ string) {
			require.NoError(t, os.MkdirAll("volcano", 0o755))
			assert.Equal(t, filepath.Join("volcano", "volcano-config.yaml"), DefaultPullPath())
		})
	})

	t.Run("falls back to root", func(t *testing.T) {
		withTempWorkingDir(t, func(_ string) {
			assert.Equal(t, "volcano-config.yaml", DefaultPullPath())
		})
	})
}
