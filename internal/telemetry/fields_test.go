package telemetry

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/sensitive"
)

// executed runs a one-command tree with use as the leaf's usage line and
// returns the leaf after parsing args.
func executed(t *testing.T, use string, args ...string) *cobra.Command {
	t.Helper()
	root := &cobra.Command{Use: "volcano"}
	leaf := &cobra.Command{Use: use, RunE: func(*cobra.Command, []string) error { return nil }}
	leaf.Flags().String("function", "", "")
	leaf.Flags().String("payload", "", "")
	leaf.Flags().StringSlice("database", nil, "")
	root.AddCommand(leaf)
	root.SetArgs(append([]string{strings.Fields(use)[0]}, args...))
	cmd, err := root.ExecuteC()
	require.NoError(t, err)
	return cmd
}

func TestResources(t *testing.T) {
	cases := []struct {
		name string
		use  string
		args []string
		want map[string]string
	}{
		{"placeholders", "get <database> <branch>", []string{"app", "preview-1"}, map[string]string{"database": "app", "branch": "preview-1"}},
		{"optional placeholder missing", "logs <name-or-id> [deployment-id]", []string{"hello"}, map[string]string{"name-or-id": "hello"}},
		{"paths are not resources", "upload <bucket> <local-path> <remote-path>", []string{"media", "/tmp/a.png", "a.png"}, map[string]string{"bucket": "media"}},
		{"queries are not resources", "search <query>", []string{"how to deploy"}, nil},
		{"stops at --", "exec [session-id] -- <command> [args...]", []string{"s1", "--", "env"}, map[string]string{"session-id": "s1"}},
		{"missing -- sends no positional", "exec [session-id] -- <command> [args...]", []string{"echo hunter2"}, nil},
		{"resource flag", "invoke [name]", []string{"--function", "hello", "--payload", "{}"}, map[string]string{"function": "hello"}},
		{"positional wins over flag", "get <function>", []string{"a", "--function", "b"}, map[string]string{"function": "a"}},
		{"slice flag value is dropped", "list", []string{"--database", "a,b"}, nil},
		{"secret-shaped value is dropped", "get <name>", []string{"pk-abcdefghijklmnopqrstuvwx"}, nil},
		{"odd characters are dropped", "get <name>", []string{"a;rm -rf"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, resources(executed(t, tc.use, tc.args...)))
		})
	}
}

func TestResources_Capped(t *testing.T) {
	fields := []string{"set"}
	var args []string
	for i := range maxResources + 2 {
		fields = append(fields, fmt.Sprintf("<k%02d>", i))
		args = append(args, "v")
	}
	use := strings.Join(fields, " ")
	for i := range maxResources + 2 {
		resourcePlaceholders[fmt.Sprintf("k%02d", i)] = true
	}
	t.Cleanup(func() {
		for i := range maxResources + 2 {
			delete(resourcePlaceholders, fmt.Sprintf("k%02d", i))
		}
	})

	got := resources(executed(t, use, args...))

	assert.Len(t, got, maxResources)
	assert.Contains(t, got, "k00")
	assert.NotContains(t, got, "k11")
}

func TestRedact(t *testing.T) {
	jwt := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.c2lnbmF0dXJl"
	cases := map[string]string{
		"Authorization: Bearer abc.def-ghi":                                            "Authorization: Bearer [redacted]",
		"token " + jwt + " expired":                                                    "token [redacted] expired",
		"key pk-0123456789abcdefABCD rejected":                                         "key [redacted] rejected",
		"used pt-0123456789abcdefABCD, sk-0123456789abcdefABCD":                        "used [redacted], [redacted]",
		"GET https://x.dev/o/a.png?X-Amz-Signature=abc&b=1 failed":                     "GET https://x.dev/o/a.png?[redacted] failed",
		"id " + strings.Repeat("a1B2", 10) + " leaked":                                 "id [redacted] leaked",
		"open /Users/someone/workspace/my-really-long-project-name/functions/index.ts": "open /Users/someone/workspace/my-really-long-project-name/functions/index.ts",
		"function hello not found (11111111-2222-3333-4444-555555555555)":              "function hello not found (11111111-2222-3333-4444-555555555555)",
		"pk-short": "pk-short",
		"aws " + "AKIA" + strings.Repeat("A1", 8) + " denied":     "aws [redacted] denied",
		"dial postgres://app:hunter2@db.internal:5432/app failed": "dial postgres://[redacted]@db.internal:5432/app failed",
		"run with password=hunter2 and API_KEY=abc":               "run with password=[redacted] and API_KEY=[redacted]",
		`unknown command "DB_PASSWORD=hunter2" for "volcano"`:     `unknown command "DB_PASSWORD=[redacted] for "volcano"`,
		"PGPASSWORD=a GITHUB_TOKEN=b MY_API_KEY=c":                "PGPASSWORD=[redacted] GITHUB_TOKEN=[redacted] MY_API_KEY=[redacted]",
		"tokens=5 MAX_TOKENS=100 TOKEN_TTL=60":                    "tokens=5 MAX_TOKENS=100 TOKEN_TTL=60",
		"invalid token: signature mismatch":                       "invalid token: signature mismatch",
	}
	for in, want := range cases {
		assert.Equal(t, want, redact(in), in)
		assert.Equal(t, want, redact(redact(in)), "idempotent: "+in)
	}
}

func TestErrorMessage(t *testing.T) {
	assert.Empty(t, errorMessage(nil, "/home/dev"))
	assert.Equal(t, "read ~/app/x.yaml: bad indent", errorMessage(errors.New("read /home/dev/app/x.yaml:\n  bad   indent"), "/home/dev"))
	assert.Equal(t, "a/b", errorMessage(errors.New("a/b"), "/"), "a home of / is not replaced")
	assert.Equal(t, "Bearer [redacted] rejected", errorMessage(errors.New("Bearer\u00a0abc123 rejected"), ""), "a Unicode space does not hide a token")
	assert.Equal(t, "failed: bad input", errorMessage(errors.New("\x1b[31mfailed:\x1b[0m bad\x00input"), ""))
	assert.Equal(t, "failed to read ~/app/volcano.env: [redacted]",
		errorMessage(fmt.Errorf("failed to read /home/dev/app/volcano.env: %w", sensitive.Wrap(errors.New(`unterminated quoted value "hunter2`))), "/home/dev"))
	long := errorMessage(errors.New(strings.Repeat("é ", 400)), "")
	assert.Len(t, []rune(long), maxErrorMessageRunes)
}
