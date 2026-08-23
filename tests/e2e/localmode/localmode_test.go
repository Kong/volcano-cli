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

	requireLocalModeOmitsProviderOnlyDatabaseCommands(t, volcanoBin, env, projectDir)

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
	assertVolcanoLocalModeE2EUserIsPro(t, env, info)
	functionID := waitForVolcanoLocalModeE2EFunctionID(t, info, "hello")
	waitForVolcanoLocalModeE2EInvokeContains(t, info, functionID, `"ok":true`)

	restartOutput := runVolcanoLocalModeE2E(t, volcanoBin, env, projectDir, "restart")
	requireContains(t, restartOutput, "Volcano is ready for local development.")
	waitForVolcanoLocalModeE2EContains(t, volcanoBin, env, projectDir, "hello", "functions", "list")
	waitForVolcanoLocalModeE2EInvokeContains(t, fetchVolcanoLocalModeE2EInfo(t, env), functionID, `"ok":true`)

	runLocalModeConfigSmoke(t, volcanoBin, env, projectDir)

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

func readLocalModeE2EFile(t *testing.T, projectDir, relativePath string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(projectDir, relativePath))
	if err != nil {
		t.Fatalf("failed to read %s: %v", relativePath, err)
	}
	return string(data)
}

// runLocalModeConfigSmoke exercises `config deploy` + `config pull` against the
// local-mode server. It is self-contained: it declares SMOKE_MESSAGE (already
// present, kept by the full sync) plus CONFIG_SMOKE, verifies both commands,
// then removes CONFIG_SMOKE so the shared cleanup only handles SMOKE_MESSAGE.
//
// Older local-mode images predate the /projects/{id}/config endpoints; against
// those the CLI returns an upgrade hint and this smoke is skipped rather than
// failed, so the CLI can ship ahead of the server image that carries the
// endpoints (cross-repo rollout).
func runLocalModeConfigSmoke(t *testing.T, binary string, env []string, projectDir string) {
	t.Helper()
	writeLocalModeE2EFile(t, projectDir, filepath.Join("volcano", "volcano-config.yaml"), `
version: 1
variables:
  - name: SMOKE_MESSAGE
    value: hello-from-volcano-cli
  - name: CONFIG_SMOKE
    value: from-config-deploy
functions:
  - name: hello
    public: true
`)

	deployOutput, err := runVolcanoLocalModeE2EAllowFailure(t, binary, env, projectDir, "config", "deploy")
	if err != nil {
		if strings.Contains(deployOutput, "does not support declarative config apply") {
			t.Logf("skipping config deploy/pull smoke: local-mode image predates the config endpoints\n%s", deployOutput)
			return
		}
		t.Fatalf("volcano config deploy failed: %v\n%s", err, deployOutput)
	}
	requireContains(t, deployOutput, "Configuration deployed from volcano-config.yaml")
	requireContains(t, deployOutput, "variables:")

	variablesAfterConfig := runVolcanoLocalModeE2E(t, binary, env, projectDir, "variables", "list")
	requireContains(t, variablesAfterConfig, "CONFIG_SMOKE")

	configPullOutput := runVolcanoLocalModeE2E(t, binary, env, projectDir, "config", "pull", "--force")
	requireContains(t, configPullOutput, "Configuration written to")
	pulledConfig := readLocalModeE2EFile(t, projectDir, filepath.Join("volcano", "volcano-config.yaml"))
	requireContains(t, pulledConfig, "CONFIG_SMOKE")
	requireContains(t, pulledConfig, "version: 1")

	configDeleteOutput := runVolcanoLocalModeE2E(t, binary, env, projectDir, "variables", "delete", "CONFIG_SMOKE", "--yes")
	requireContains(t, configDeleteOutput, "deleted")
}

// requireLocalModeOmitsProviderOnlyDatabaseCommands checks against the running
// stack what the unit tests check against the command tree: backups, restores,
// and branches need the storage provider, which local development does not run.
// A local project has to be turned away with the cloud path rather than after a
// request the local server could only fail.
func requireLocalModeOmitsProviderOnlyDatabaseCommands(t *testing.T, binary string, env []string, dir string) {
	t.Helper()
	help := runVolcanoLocalModeE2E(t, binary, env, dir, "databases", "--help")
	for _, command := range []string{"backups", "backup-schedule", "restore", "restores", "branches"} {
		requireNotContains(t, help, command)
	}

	for _, args := range [][]string{
		{"databases", "backups", "list", "app"},
		{"databases", "backup", "list", "app"},
		{"databases", "backup-schedule", "get", "app"},
		{"databases", "restore", "app", "--backup", "nightly"},
		{"databases", "restore", "app", "--to", "2026-01-15T09:30:00Z"},
		{"databases", "restores", "list", "app"},
		{"databases", "restore-history", "list", "app"},
		{"databases", "branches", "list", "app"},
		{"databases", "branches", "create", "app", "feature"},
		{"databases", "branch", "list", "app"},
	} {
		output, err := runVolcanoLocalModeE2EAllowFailure(t, binary, env, dir, args...)
		if err == nil {
			t.Fatalf("expected volcano %s to fail in local mode\n%s", strings.Join(args, " "), output)
		}
		requireContains(t, output, "is a cloud command")
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
	UserID     string `json:"user_id"`
	UserToken  string `json:"user_token"`
	ServiceKey string `json:"service_key"`
}

type localModeE2EUser struct {
	ID   string `json:"id"`
	Plan string `json:"plan"`
}

// localModeE2EManagementURL is the container-internal management API address. It
// is bound to localhost:8001 inside volcano-server and intentionally NOT
// published to the host, so the plan checks below reach it via `docker exec`
// (like `local info` above) rather than over info.APIURL, which is the public API
// on 8000 and does not serve management routes.
const localModeE2EManagementURL = "http://localhost:8001"

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
	if info.APIURL == "" || info.ProjectID == "" || info.UserID == "" || info.UserToken == "" || info.ServiceKey == "" {
		t.Fatalf("local-mode info missing required fields: %s", output)
	}
	return info
}

// requestVolcanoLocalModeE2EManagement calls the container's management API from
// inside volcano-server. That API is network-isolated (localhost:8001, not
// published, not token-protected), so requests run through `docker exec` and busybox
// wget, mirroring how the hosting CI exercises the same routes. It returns the
// response body and whether the server answered 2xx (wget exits non-zero on 4xx/5xx).
func requestVolcanoLocalModeE2EManagement(t *testing.T, env []string, method, path, jsonBody string) (string, bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	args := []string{"exec", "volcano-server", "wget", "-qO-"}
	if method == http.MethodPost {
		args = append(args, "--header", "Content-Type: application/json", "--post-data", jsonBody)
	}
	args = append(args, localModeE2EManagementURL+path)

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	return string(output), err == nil
}

func assertVolcanoLocalModeE2EUserIsPro(t *testing.T, env []string, info localModeE2EInfo) {
	t.Helper()

	user := fetchVolcanoLocalModeE2EUser(t, env, info)
	if user.Plan != "PRO" {
		t.Fatalf("local-mode default user plan = %q, want PRO", user.Plan)
	}

	// Local mode must not expose the management downgrade path. The server runs
	// every user as PRO locally; the route is unregistered in local mode, so this
	// must fail. If it ever starts accepting FREE, the re-read below catches it.
	if body, ok := requestVolcanoLocalModeE2EManagement(t, env, http.MethodPost, "/users/"+info.UserID+"/plan", `{"plan":"FREE"}`); ok {
		t.Fatalf("local-mode management API unexpectedly accepted a plan downgrade: %s", body)
	}

	user = fetchVolcanoLocalModeE2EUser(t, env, info)
	if user.Plan != "PRO" {
		t.Fatalf("local-mode default user plan after attempted downgrade = %q, want PRO", user.Plan)
	}
}

func fetchVolcanoLocalModeE2EUser(t *testing.T, env []string, info localModeE2EInfo) localModeE2EUser {
	t.Helper()
	body, ok := requestVolcanoLocalModeE2EManagement(t, env, http.MethodGet, "/users/"+info.UserID, "")
	if !ok {
		t.Fatalf("fetch local user %s failed: %s", info.UserID, body)
	}
	var user localModeE2EUser
	if err := json.Unmarshal([]byte(body), &user); err != nil {
		t.Fatalf("parse local user: %v\n%s", err, body)
	}
	if user.ID != info.UserID {
		t.Fatalf("local user id = %q, want %q", user.ID, info.UserID)
	}
	return user
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
