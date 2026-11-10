package localmode

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// functionCandidatesCapability is the /capabilities entry a server that has
// function previews always lists, available or not.
const functionCandidatesCapability = "functions.candidates"

// requireLocalModeRefusesFunctionPreviews creates a preview of the deployed
// hello function against the local stack and checks the refusal says previews
// are unavailable: local functions run without the workflow a preview's own
// Lambda and its promotion are built on. Reading a function's previews still
// works and finds none.
//
// A server image built before function previews has no such routes and does
// not list the capability; the step is left out there, so this smoke does not
// depend on when the published local image picks up the API.
func requireLocalModeRefusesFunctionPreviews(t *testing.T, binary string, env []string, dir string) {
	t.Helper()
	status, listed := localModeE2ECapabilityStatus(t, env, functionCandidatesCapability)
	if !listed {
		t.Logf("the local server does not list %s; it predates function previews, so their refusal is not checked", functionCandidatesCapability)
		return
	}
	if status == "available" {
		t.Fatalf("the local server reports %s available; local functions cannot run previews", functionCandidatesCapability)
	}

	args := []string{"functions", "preview", "create", "hello", "--name", "next"}
	output, err := runVolcanoLocalModeE2EAllowFailure(t, binary, env, dir, args...)
	if err == nil {
		t.Fatalf("expected volcano %s to fail against the local stack\n%s", strings.Join(args, " "), output)
	}
	requireContains(t, output, "function previews aren't available")
	requireNotContains(t, output, "Preview 'next' deployment started")

	requireContains(t, runVolcanoLocalModeE2E(t, binary, env, dir, "functions", "preview", "list", "hello"), "No previews.")
}

// localModeE2ECapabilityStatus reads one entry of the local server's public
// /capabilities list and reports its status and whether it is listed at all.
func localModeE2ECapabilityStatus(t *testing.T, env []string, id string) (string, bool) {
	t.Helper()
	info := fetchVolcanoLocalModeE2EInfo(t, env)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, strings.TrimRight(info.APIURL, "/")+"/capabilities", http.NoBody)
	if err != nil {
		t.Fatalf("failed to build the capabilities request: %v", err)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to read the local server's capabilities: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /capabilities answered %d", resp.StatusCode)
	}
	var listed struct {
		Data []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil {
		t.Fatalf("failed to parse the local server's capabilities: %v", err)
	}
	for _, capability := range listed.Data {
		if capability.ID == id {
			return capability.Status, true
		}
	}
	return "", false
}
