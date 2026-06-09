package localmode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestLocalModeE2ESmoke(t *testing.T) {
	if os.Getenv("VOLCANO_LOCALMODE_E2E") != "1" {
		t.Skip("set VOLCANO_LOCALMODE_E2E=1 and run the local-mode E2E Make target to execute this destructive smoke test")
	}
	requireDocker(t)

	volcanoBin := buildLocalModeE2EBinary(t)
	env := localModeE2EEnv(t)
	projectDir := localModeE2EProjectDir(t)
	writeLocalModeE2EFixtures(t, projectDir)
	cleanup := func() {
		_, _ = runVolcanoLocalModeE2EAllowFailure(t, volcanoBin, env, projectDir, "stop", "--clean")
	}
	cleanup()
	t.Cleanup(cleanup)

	startOutput := runVolcanoLocalModeE2E(t, volcanoBin, env, projectDir, "start")
	requireContains(t, startOutput, "Volcano is ready for local development.")

	statusOutput := runVolcanoLocalModeE2E(t, volcanoBin, env, projectDir, "status")
	requireContains(t, statusOutput, "Volcano Local Development Status")
	requireContains(t, statusOutput, "API URL")

	databasesOutput := runVolcanoLocalModeE2E(t, volcanoBin, env, projectDir, "databases", "list")
	requireContains(t, databasesOutput, "app")

	createDatabaseOutput := runVolcanoLocalModeE2E(t, volcanoBin, env, projectDir, "databases", "create", "cli_contract")
	requireContains(t, createDatabaseOutput, "Database 'cli_contract' created")

	databasesAfterCreate := waitForVolcanoLocalModeE2EContains(t, volcanoBin, env, projectDir, "cli_contract", "databases", "list")
	requireContains(t, databasesAfterCreate, "app")

	migrationOutput := runVolcanoLocalModeE2E(t, volcanoBin, env, projectDir, "migrations", "deploy", "--all", "-d", "app")
	requireContains(t, migrationOutput, "Applying 001_create_cli_contract.sql... ok")
	requireContains(t, migrationOutput, "Migrations deployed!")

	deleteDatabaseOutput := runVolcanoLocalModeE2E(t, volcanoBin, env, projectDir, "databases", "delete", "cli_contract", "--yes")
	requireContains(t, deleteDatabaseOutput, "Database 'cli_contract' deleted")

	variablesDeployOutput := runVolcanoLocalModeE2E(t, volcanoBin, env, projectDir, "variables", "deploy")
	requireContains(t, variablesDeployOutput, "SMOKE_MESSAGE (saved)")
	requireContains(t, variablesDeployOutput, "1 variable(s) saved")

	variablesOutput := runVolcanoLocalModeE2E(t, volcanoBin, env, projectDir, "variables", "list")
	requireContains(t, variablesOutput, "SMOKE_MESSAGE")

	variableGetOutput := runVolcanoLocalModeE2E(t, volcanoBin, env, projectDir, "variables", "get", "SMOKE_MESSAGE")
	requireContains(t, variableGetOutput, "Name: SMOKE_MESSAGE")

	functionDeployOutput := runVolcanoLocalModeE2E(t, volcanoBin, env, projectDir, "functions", "deploy", "--all")
	requireContains(t, functionDeployOutput, "Packaging hello")
	requireContains(t, functionDeployOutput, "functions deployment started")

	waitForVolcanoLocalModeE2EContains(t, volcanoBin, env, projectDir, "hello", "functions", "list")
	functionGetOutput := runVolcanoLocalModeE2E(t, volcanoBin, env, projectDir, "functions", "get", "hello")
	requireContains(t, functionGetOutput, "Name: hello")

	info := fetchVolcanoLocalModeE2EInfo(t, env)
	functionID := waitForVolcanoLocalModeE2EFunctionID(t, info, "hello")
	waitForVolcanoLocalModeE2EInvokeContains(t, info, functionID, `"ok":true`)

	variableDeleteOutput := runVolcanoLocalModeE2E(t, volcanoBin, env, projectDir, "variables", "delete", "SMOKE_MESSAGE", "--yes")
	requireContains(t, variableDeleteOutput, "deleted")
	variablesAfterDelete := runVolcanoLocalModeE2E(t, volcanoBin, env, projectDir, "variables", "list")
	requireNotContains(t, variablesAfterDelete, "SMOKE_MESSAGE")

	writeLocalModeE2EFile(t, projectDir, "hello.txt", "hello from local storage\n")
	bucketOutput := runVolcanoLocalModeE2E(t, volcanoBin, env, projectDir, "storage", "bucket", "create", "cli-contract", "--allowed-mime-type", "text/plain", "--file-size-limit", "4096")
	requireContains(t, bucketOutput, "cli-contract")

	bucketGetOutput := runVolcanoLocalModeE2E(t, volcanoBin, env, projectDir, "storage", "bucket", "get", "cli-contract")
	requireContains(t, bucketGetOutput, "File size limit: 4.0 KiB")

	bucketUpdateOutput := runVolcanoLocalModeE2E(t, volcanoBin, env, projectDir, "storage", "bucket", "update", "cli-contract", "--allowed-mime-type", "text/plain", "--file-size-limit", "8192")
	requireContains(t, bucketUpdateOutput, "File size limit: 8.0 KiB")

	uploadOutput := runVolcanoLocalModeE2E(t, volcanoBin, env, projectDir, "storage", "object", "upload", "cli-contract", "hello.txt", "greetings/hello.txt", "--content-type", "text/plain")
	requireContains(t, uploadOutput, "greetings/hello.txt")

	objectsOutput := runVolcanoLocalModeE2E(t, volcanoBin, env, projectDir, "storage", "object", "list", "cli-contract")
	requireContains(t, objectsOutput, "greetings/hello.txt")

	downloadOutput := runVolcanoLocalModeE2E(t, volcanoBin, env, projectDir, "storage", "object", "download", "cli-contract", "greetings/hello.txt", "-")
	requireContains(t, downloadOutput, "hello from local storage")

	deleteObjectOutput := runVolcanoLocalModeE2E(t, volcanoBin, env, projectDir, "storage", "object", "delete", "cli-contract", "greetings/hello.txt", "--yes")
	requireContains(t, deleteObjectOutput, "deleted")

	deleteBucketOutput := runVolcanoLocalModeE2E(t, volcanoBin, env, projectDir, "storage", "bucket", "delete", "cli-contract", "--yes")
	requireContains(t, deleteBucketOutput, "deleted")

	deleteFunctionOutput := runVolcanoLocalModeE2E(t, volcanoBin, env, projectDir, "functions", "delete", "hello", "--yes")
	requireContains(t, deleteFunctionOutput, "deletion started")

	resetOutput := runVolcanoLocalModeE2E(t, volcanoBin, env, projectDir, "reset", "--yes")
	requireContains(t, resetOutput, "Local reset complete.")
	requireContains(t, resetOutput, "volcano migrations deploy --all -d app")

	statusAfterReset := runVolcanoLocalModeE2E(t, volcanoBin, env, projectDir, "status")
	requireContains(t, statusAfterReset, "Volcano Local Development Status")
}

func requireDocker(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "docker", "version").CombinedOutput()
	if err != nil {
		t.Skipf("Docker is unavailable: %v\n%s", err, strings.TrimSpace(string(output)))
	}
}

func buildLocalModeE2EBinary(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to resolve current test file path")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", "..", ".."))
	binary := filepath.Join(t.TempDir(), "volcano")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-o", binary, "./cmd/volcano")
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to build volcano binary: %v\n%s", err, strings.TrimSpace(string(output)))
	}
	return binary
}

func localModeE2EEnv(t *testing.T) []string {
	t.Helper()
	env := append([]string{}, os.Environ()...)
	if os.Getenv("DOCKER_CONFIG") == "" {
		home, err := os.UserHomeDir()
		if err == nil && home != "" {
			env = append(env, "DOCKER_CONFIG="+filepath.Join(home, ".docker"))
		}
	}
	env = append(env,
		"HOME="+t.TempDir(),
		"VOLCANO_TOKEN=",
		"VOLCANO_PROJECT_ID=",
		"VOLCANO_API_URL=",
		"VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID=",
	)
	if os.Getenv("VOLCANO_IMAGE") == "" {
		env = append(env, "VOLCANO_IMAGE=kong/volcano:local-nightly")
	}
	return env
}

func localModeE2EProjectDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

func writeLocalModeE2EFixtures(t *testing.T, projectDir string) {
	t.Helper()
	writeLocalModeE2EFile(t, projectDir, filepath.Join("volcano", "migrations", "001_create_cli_contract.sql"), `
CREATE TABLE IF NOT EXISTS cli_contract_smoke (
	id BIGSERIAL PRIMARY KEY,
	message TEXT NOT NULL
)
`)
	writeLocalModeE2EFile(t, projectDir, filepath.Join("volcano", "volcano.env"), "SMOKE_MESSAGE=hello-from-volcano-cli\n")
	writeLocalModeE2EFile(t, projectDir, filepath.Join("volcano", "functions", "hello.js"), `
exports.handler = async () => ({
	statusCode: 200,
	headers: { "content-type": "application/json" },
	body: JSON.stringify({ ok: true })
});
`)
}

func writeLocalModeE2EFile(t *testing.T, projectDir, relativePath, content string) {
	t.Helper()
	path := filepath.Join(projectDir, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("failed to create fixture directory for %s: %v", relativePath, err)
	}
	if err := os.WriteFile(path, []byte(strings.TrimLeft(content, "\n")), 0o644); err != nil {
		t.Fatalf("failed to write fixture %s: %v", relativePath, err)
	}
}

func runVolcanoLocalModeE2E(t *testing.T, binary string, env []string, dir string, args ...string) string {
	t.Helper()
	output, err := runVolcanoLocalModeE2EAllowFailure(t, binary, env, dir, args...)
	if err != nil {
		t.Fatalf("volcano %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
	return output
}

func runVolcanoLocalModeE2EAllowFailure(t *testing.T, binary string, env []string, dir string, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = env
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func waitForVolcanoLocalModeE2EContains(t *testing.T, binary string, env []string, dir, needle string, args ...string) string {
	t.Helper()
	var lastOutput string
	var lastErr error
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		lastOutput, lastErr = runVolcanoLocalModeE2EAllowFailure(t, binary, env, dir, args...)
		if lastErr == nil && strings.Contains(lastOutput, needle) {
			return lastOutput
		}
		time.Sleep(2 * time.Second)
	}
	if lastErr != nil {
		t.Fatalf("volcano %s did not return output containing %q: %v\n%s", strings.Join(args, " "), needle, lastErr, lastOutput)
	}
	t.Fatalf("expected output from volcano %s to contain %q\n%s", strings.Join(args, " "), needle, lastOutput)
	return ""
}

type localModeE2EInfo struct {
	APIURL     string `json:"api_url"`
	ProjectID  string `json:"project_id"`
	UserToken  string `json:"user_token"`
	ServiceKey string `json:"service_key"`
}

func fetchVolcanoLocalModeE2EInfo(t *testing.T, env []string) localModeE2EInfo {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "exec", "volcano-server", "/app/volcano-hosting", "local", "info", "--format", "json")
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to fetch local-mode info: %v\n%s", err, output)
	}

	var info localModeE2EInfo
	if err := json.Unmarshal(output, &info); err != nil {
		t.Fatalf("failed to parse local-mode info: %v\n%s", err, output)
	}
	if info.APIURL == "" || info.ProjectID == "" || info.UserToken == "" || info.ServiceKey == "" {
		t.Fatalf("local-mode info missing required fields: %s", output)
	}
	return info
}

func waitForVolcanoLocalModeE2EFunctionID(t *testing.T, info localModeE2EInfo, name string) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	var lastBody string
	for time.Now().Before(deadline) {
		id, body, err := volcanoLocalModeE2EFunctionID(t, info, name)
		lastBody = body
		if err == nil && id != "" {
			return id
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("function %q did not appear in local-mode API:\n%s", name, lastBody)
	return ""
}

func volcanoLocalModeE2EFunctionID(t *testing.T, info localModeE2EInfo, name string) (string, string, error) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/projects/%s/functions?limit=100", strings.TrimRight(info.APIURL, "/"), info.ProjectID), http.NoBody)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+info.UserToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", string(data), fmt.Errorf("list functions returned %d", resp.StatusCode)
	}

	var page struct {
		Data []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &page); err != nil {
		return "", string(data), err
	}
	for _, fn := range page.Data {
		if fn.Name == name {
			return fn.ID, string(data), nil
		}
	}
	return "", string(data), fmt.Errorf("function %q not found", name)
}

func waitForVolcanoLocalModeE2EInvokeContains(t *testing.T, info localModeE2EInfo, functionID, needle string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	var lastBody string
	var lastStatus int
	for time.Now().Before(deadline) {
		body, status := invokeVolcanoLocalModeE2EFunction(t, info, functionID)
		lastBody = body
		lastStatus = status
		if status == http.StatusOK && strings.Contains(body, needle) {
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("function %s did not return %q before timeout (last status %d):\n%s", functionID, needle, lastStatus, lastBody)
}

func invokeVolcanoLocalModeE2EFunction(t *testing.T, info localModeE2EInfo, functionID string) (string, int) {
	t.Helper()
	payload := bytes.NewBufferString(`{"payload":{}}`)
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/functions/%s/invoke", strings.TrimRight(info.APIURL, "/"), functionID), payload)
	if err != nil {
		t.Fatalf("failed to build function invoke request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+info.ServiceKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err.Error(), 0
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return string(data), resp.StatusCode
}

func requireContains(t *testing.T, output, needle string) {
	t.Helper()
	if !strings.Contains(output, needle) {
		t.Fatalf("expected output to contain %q\n%s", needle, output)
	}
}

func requireNotContains(t *testing.T, output, needle string) {
	t.Helper()
	if strings.Contains(output, needle) {
		t.Fatalf("expected output not to contain %q\n%s", needle, output)
	}
}
