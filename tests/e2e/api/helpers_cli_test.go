package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type cliResult struct {
	output string
	code   int
	err    error
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
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
			t.Fatalf("failed to run volcano %s: %v", strings.Join(args, " "), err)
		}
	}
	return cliResult{output: stdout.String() + stderr.String(), code: code, err: err}
}

func (e *apiE2E) runCloudCLIWithEnv(t *testing.T, extraEnv []string, args ...string) cliResult {
	t.Helper()
	cloudArgs := append([]string{"cloud"}, args...)
	return e.runCLIWithEnv(t, extraEnv, cloudArgs...)
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
		t.Fatalf("command failed with exit code %d:\n%s", r.code, r.output)
	}
	for _, needle := range needles {
		if !strings.Contains(r.output, needle) {
			t.Fatalf("command output missing %q:\n%s", needle, r.output)
		}
	}
}

func (r cliResult) requireFailure(t *testing.T, needles ...string) {
	t.Helper()
	if r.code == 0 {
		t.Fatalf("command unexpectedly succeeded:\n%s", r.output)
	}
	for _, needle := range needles {
		if !strings.Contains(r.output, needle) {
			t.Fatalf("command output missing %q:\n%s", needle, r.output)
		}
	}
}

func (r cliResult) requireNotContains(t *testing.T, needles ...string) {
	t.Helper()
	for _, needle := range needles {
		if strings.Contains(r.output, needle) {
			t.Fatalf("command output unexpectedly contained %q:\n%s", needle, r.output)
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
		last = e.runCLI(t, args...)
		if last.code == 0 && strings.Contains(last.output, needle) {
			return last
		}
		time.Sleep(10 * time.Second)
	}
	t.Fatalf("volcano %s did not return output containing %q before %s:\n%s", strings.Join(args, " "), needle, timeout, last.output)
	return last
}

func (e *apiE2E) waitForCloudCLIContains(t *testing.T, timeout time.Duration, needle string, args ...string) cliResult {
	t.Helper()
	cloudArgs := append([]string{"cloud"}, args...)
	return e.waitForCLIContains(t, timeout, needle, cloudArgs...)
}
