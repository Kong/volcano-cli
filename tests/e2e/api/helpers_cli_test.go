package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"
)

// credentialPattern matches the platform and project access token secrets these
// tests hold. Both are shown once and stay live until revoked, so neither may
// reach a CI log — which is what a failing assertion that dumps a command's
// output or its argv does.
//
// Only what gets printed is redacted. The assertions that a read never returns
// a secret have to compare against the real one.
var credentialPattern = regexp.MustCompile(`p([kt])-[A-Za-z0-9_-]{4,}`)

func redactCredentials(text string) string {
	return credentialPattern.ReplaceAllString(text, "p$1-[redacted]")
}

type cliResult struct {
	output string
	// stdout alone, for --json output: notices and errors go to stderr, which
	// output merges in and a decoder would choke on.
	stdout string
	code   int
	err    error
}

// The helpers below print a command's output and its argv on failure, and the
// argv of a `login --token <secret>` carries the secret just as the output of
// `create --json` does. This runs without the E2E gate because it tests the
// redaction, not the platform.
func TestAPIE2ERedactsMintedCredentials(t *testing.T) {
	const secret = "pt-Wq9l2m4XcR7tFv1sN8bK3hJ0"

	for name, text := range map[string]string{
		"create --json output": `{"name":"ci-deploy","token":"` + secret + `"}`,
		"login argv":           "login --token " + secret,
		"platform token":       "login --token pk-4bN7sK1pZx8c5Vt2",
	} {
		t.Run(name, func(t *testing.T) {
			redacted := redactCredentials(text)
			if strings.Contains(redacted, secret) || strings.Contains(redacted, "pk-4bN7sK1pZx8c5Vt2") {
				t.Fatalf("redaction left a credential in %q", redacted)
			}
			if !strings.Contains(redacted, "-[redacted]") {
				t.Fatalf("redaction did not mark what it removed: %q", redacted)
			}
		})
	}

	// A message that merely names the prefixes is not a credential.
	if got := redactCredentials("needs an account token (pk-) but this is a project access token (pt-)"); strings.Contains(got, "redacted") {
		t.Fatalf("redaction swallowed prose: %q", got)
	}
}

func (e *apiE2E) runCLI(t *testing.T, args ...string) cliResult {
	t.Helper()
	return e.runCLIWithEnv(t, nil, args...)
}

func (e *apiE2E) runCloudCLI(t *testing.T, args ...string) cliResult {
	t.Helper()
	return e.runCloudCLIWithEnv(t, nil, args...)
}

func (e *apiE2E) runCLIWithEnv(t *testing.T, extraEnv []string, args ...string) cliResult {
	t.Helper()
	return e.runCLIWithin(t, apiE2ECommandTimeout, extraEnv, args...)
}

func (e *apiE2E) runCLIWithin(t *testing.T, timeout time.Duration, extraEnv []string, args ...string) cliResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, e.binary, args...)
	cmd.Dir = e.projectDir
	cmd.Env = append(e.commandEnv(), extraEnv...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("failed to run volcano %s: %v", redactCredentials(strings.Join(args, " ")), err)
		}
	}
	return cliResult{output: stdout.String() + stderr.String(), stdout: stdout.String(), code: code, err: err}
}

func (e *apiE2E) runCloudCLIWithEnv(t *testing.T, extraEnv []string, args ...string) cliResult {
	t.Helper()
	cloudArgs := append([]string{"cloud"}, args...)
	return e.runCLIWithEnv(t, extraEnv, cloudArgs...)
}

func (e *apiE2E) runCloudCLIWithin(t *testing.T, timeout time.Duration, args ...string) cliResult {
	t.Helper()
	cloudArgs := append([]string{"cloud"}, args...)
	return e.runCLIWithin(t, timeout, nil, cloudArgs...)
}

// attemptTimeout keeps a polled command inside the budget its caller is waiting
// against, so one hung command cannot outlive the whole wait. The floor leaves a
// final attempt enough room to answer: killing it for a sliver of remaining time
// would report a cancelled command instead of the state the wait gave up on.
func attemptTimeout(deadline time.Time) time.Duration {
	remaining := time.Until(deadline)
	switch {
	case remaining > apiE2ECommandTimeout:
		return apiE2ECommandTimeout
	case remaining < apiE2EPollInterval:
		return apiE2EPollInterval
	default:
		return remaining
	}
}

func (e *apiE2E) commandEnv() []string {
	env := make([]string, 0, len(os.Environ())+3)
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "HOME=") ||
			strings.HasPrefix(entry, "VOLCANO_TOKEN=") ||
			strings.HasPrefix(entry, "VOLCANO_PROJECT_ID=") ||
			strings.HasPrefix(entry, "VOLCANO_API_URL=") {
			continue
		}
		env = append(env, entry)
	}
	env = append(env, "HOME="+e.homeDir, "VOLCANO_API_URL="+e.apiURL)
	return env
}

func (r cliResult) requireSuccess(t *testing.T, needles ...string) {
	t.Helper()
	if r.code != 0 {
		t.Fatalf("command failed with exit code %d:\n%s", r.code, redactCredentials(r.output))
	}
	for _, needle := range needles {
		if !strings.Contains(r.output, needle) {
			t.Fatalf("command output missing %q:\n%s", redactCredentials(needle), redactCredentials(r.output))
		}
	}
}

func (r cliResult) requireFailure(t *testing.T, needles ...string) {
	t.Helper()
	if r.code == 0 {
		t.Fatalf("command unexpectedly succeeded:\n%s", redactCredentials(r.output))
	}
	for _, needle := range needles {
		if !strings.Contains(r.output, needle) {
			t.Fatalf("command output missing %q:\n%s", redactCredentials(needle), redactCredentials(r.output))
		}
	}
}

func (r cliResult) requireNotContains(t *testing.T, needles ...string) {
	t.Helper()
	for _, needle := range needles {
		if strings.Contains(r.output, needle) {
			t.Fatalf("command output unexpectedly contained %q:\n%s", redactCredentials(needle), redactCredentials(r.output))
		}
	}
}

func (e *apiE2E) waitForDatabaseActive(t *testing.T, database string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastStatus string
	for time.Now().Before(deadline) {
		resp := apiE2EJSONRequest(t, http.MethodGet, e.apiURL+"/projects/"+e.projectID+"/databases/"+database, e.token, nil, http.StatusOK)
		lastStatus = strings.TrimSpace(fmt.Sprint(resp["status"]))
		connectionString := strings.TrimSpace(fmt.Sprint(resp["connection_string"]))
		if lastStatus == "active" && connectionString != "" && connectionString != "<nil>" {
			return
		}
		time.Sleep(10 * time.Second)
	}
	t.Fatalf("database %q did not become active before %s (last status: %s)", database, timeout, lastStatus)
}

func (e *apiE2E) waitForCLIContains(t *testing.T, timeout time.Duration, needle string, args ...string) cliResult {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last cliResult
	for time.Now().Before(deadline) {
		last = e.runCLIWithin(t, attemptTimeout(deadline), nil, args...)
		pendingDeployment := needle == "Status: active" && strings.Contains(last.output, "Pending Deployment:")
		if last.code == 0 && strings.Contains(last.output, needle) && !pendingDeployment {
			return last
		}
		if needle == "Status: active" && last.code == 0 {
			output := strings.ToLower(last.output)
			if strings.Contains(output, "status: failed") || strings.Contains(output, "status: error") {
				t.Fatalf("volcano %s reached a failed status while waiting for active:\n%s",
					redactCredentials(strings.Join(args, " ")), redactCredentials(last.output))
			}
		}
		time.Sleep(apiE2EPollInterval)
	}
	t.Fatalf("volcano %s did not return output containing %q before %s:\n%s",
		redactCredentials(strings.Join(args, " ")), redactCredentials(needle), timeout, redactCredentials(last.output))
	return last
}

func (e *apiE2E) waitForCloudCLIContains(t *testing.T, timeout time.Duration, needle string, args ...string) cliResult {
	t.Helper()
	cloudArgs := append([]string{"cloud"}, args...)
	return e.waitForCLIContains(t, timeout, needle, cloudArgs...)
}
