package api

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

const (
	apiE2EEnabledEnv        = "VOLCANO_API_E2E"
	apiE2EDefaultRegion     = "aws-us-east-1"
	apiE2EDefaultPGVersion  = "16"
	apiE2ECompiledAPIURLVar = "github.com/Kong/volcano-cli/internal/config.compiledDefaultAPIURL"

	apiE2EFunctionDeploymentTimeout = 20 * time.Minute
	apiE2EFrontendDeploymentTimeout = 30 * time.Minute
	apiE2EResourceDeleteTimeout     = 10 * time.Minute
	apiE2EPollInterval              = 5 * time.Second
)

type apiE2E struct {
	apiURL     string
	mgmtURL    string
	binary     string
	homeDir    string
	projectDir string
	token      string
	projectID  string
	project    string
}

func setupAPIE2E(t *testing.T, prefix string) *apiE2E {
	t.Helper()

	requireAPIE2EEnabled(t)
	apiURL := strings.TrimRight(requireAPIE2EEnv(t, "VOLCANO_API_URL"), "/")
	mgmtURL := strings.TrimRight(requireAPIE2EEnv(t, "VOLCANO_MGMT_URL"), "/")
	binary := buildAPIE2EBinary(t, apiURL)
	suffix := apiE2ESuffix(t)
	userID := fmt.Sprintf("cli-e2e-%s-user-%s", prefix, suffix)
	projectName := fmt.Sprintf("cli-e2e-%s-%s", prefix, suffix)
	createAPIE2EUser(t, mgmtURL, userID)
	t.Cleanup(func() {
		if err := deleteAPIE2EUser(mgmtURL, userID); err != nil {
			t.Errorf("delete API E2E user: %v", err)
		}
	})
	token := createAPIE2EUserToken(t, mgmtURL, userID)
	projectID := createAPIE2EProject(t, apiURL, token, projectName)
	t.Cleanup(func() {
		if err := deleteAPIE2EProject(apiURL, token, projectID); err != nil {
			t.Errorf("delete API E2E project: %v", err)
		}
	})

	env := &apiE2E{
		apiURL:     apiURL,
		mgmtURL:    mgmtURL,
		binary:     binary,
		homeDir:    t.TempDir(),
		projectDir: t.TempDir(),
		token:      token,
		projectID:  projectID,
		project:    projectName,
	}
	return env
}

func requireAPIE2EEnabled(t *testing.T) {
	t.Helper()
	if os.Getenv(apiE2EEnabledEnv) != "1" {
		t.Skipf("set %s=1 and run the API E2E Make target to execute these tests", apiE2EEnabledEnv)
	}
}

func (e *apiE2E) loginAndUse(t *testing.T) {
	t.Helper()
	e.runCLI(t, "login", "--token", e.token).requireSuccess(t, "Logged in successfully")
	e.runCLI(t, "use", e.project).requireSuccess(t, "Now using project")
}

func requireAPIE2EEnv(t *testing.T, key string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		t.Fatalf("%s is required", key)
	}
	return value
}

func apiE2ESuffix(t *testing.T) string {
	t.Helper()
	var buf [5]byte
	if _, err := rand.Read(buf[:]); err != nil {
		t.Fatalf("failed to generate random suffix: %v", err)
	}
	return hex.EncodeToString(buf[:])
}
