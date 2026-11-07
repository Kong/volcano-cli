package cmdutil

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
	"github.com/Kong/volcano-cli/internal/deploycommit"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	sourceProjectID = "22222222-2222-4222-8222-222222222222"
	sourceSHA       = "4f1c2b3a9d8e7f6a5b4c3d2e1f0a9b8c7d6e5f4a"
)

var shop = deploycommit.Target{Repository: "acme/shop", Branch: "main"}

func TestUnreadableSourceMessage(t *testing.T) {
	advice := "\nDeploy from a clean checkout of a commit pushed to acme/shop's main branch, or deploy with a Git push."
	for name, tc := range map[string]struct {
		unreadable deploycommit.Unreadable
		want       string
	}{
		"one deployment outside a checkout": {
			unreadable: deploycommit.Unreadable{
				Resources: []string{"frontend web"}, Uploads: 1,
				Reasons: []deploycommit.Reason{{Kind: deploycommit.ReasonNotCheckout}},
			},
			want: "LavaFlow can't read this deployment's code to fix errors it serves: " +
				"the deploy didn't run in a Git checkout, so it sent no commit." + advice,
		},
		"every deployment with uncommitted changes": {
			unreadable: deploycommit.Unreadable{
				Resources: []string{"function hello", "function orders"}, Uploads: 2,
				Reasons: []deploycommit.Reason{{
					Kind:  deploycommit.ReasonUncommitted,
					Paths: []string{"package.json", "volcano/functions/hello.js", "volcano/functions/orders/", "README.md", "dist/"},
					SHA:   sourceSHA,
				}},
			},
			want: "LavaFlow can't read these deployments' code to fix errors they serve: the checkout has uncommitted " +
				"changes (package.json, volcano/functions/hello.js, volcano/functions/orders/ and 2 more)." + advice,
		},
		"one of several, from another repository": {
			unreadable: deploycommit.Unreadable{
				Resources: []string{"frontend web"}, Uploads: 3,
				Reasons: []deploycommit.Reason{{Kind: deploycommit.ReasonOtherRepository, SHA: sourceSHA, Repository: "acme/web"}},
			},
			want: "LavaFlow can't read the code deployed for frontend web to fix errors it serves: " +
				"commit 4f1c2b3 is from acme/web, not the project's repository acme/shop." + advice,
		},
		"some of several, for two reasons": {
			unreadable: deploycommit.Unreadable{
				Resources: []string{"function hello", "durable function orders"}, Uploads: 3,
				Reasons: []deploycommit.Reason{
					{Kind: deploycommit.ReasonUncommitted, Paths: []string{"a.js", "b.js"}, SHA: sourceSHA},
					{Kind: deploycommit.ReasonOffBranch, SHA: sourceSHA},
				},
			},
			want: "LavaFlow can't read the code deployed for function hello and durable function orders to fix errors " +
				"they serve: the checkout has uncommitted changes (a.js and b.js); commit 4f1c2b3 isn't on origin/main." + advice,
		},
		"files that differ from the commit": {
			unreadable: deploycommit.Unreadable{
				Resources: []string{"frontend web"}, Uploads: 1,
				Reasons: []deploycommit.Reason{{Kind: deploycommit.ReasonDiffers, SHA: sourceSHA}},
			},
			want: "LavaFlow can't read this deployment's code to fix errors it serves: " +
				"the uploaded files differ from commit 4f1c2b3." + advice,
		},
		"reporting turned off": {
			unreadable: deploycommit.Unreadable{
				Resources: []string{"frontend web"}, Uploads: 1,
				Reasons: []deploycommit.Reason{{Kind: deploycommit.ReasonOptedOut}},
			},
			want: "LavaFlow can't read this deployment's code to fix errors it serves: " +
				"VOLCANO_DEPLOY_COMMIT=off kept the deploy from sending its commit.\n" +
				"Unset VOLCANO_DEPLOY_COMMIT, then deploy from a clean checkout of a commit pushed to acme/shop's " +
				"main branch, or deploy with a Git push.",
		},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, unreadableSourceMessage(&tc.unreadable, shop))
		})
	}
}

func TestReasonTextNamesEveryReason(t *testing.T) {
	for kind, want := range map[deploycommit.ReasonKind]string{
		deploycommit.ReasonNoGit:     "git isn't installed, so the deploy sent no commit",
		deploycommit.ReasonNoCommit:  "the checkout has no commit yet",
		deploycommit.ReasonNotGitHub: "the checkout's origin remote isn't a GitHub repository, so the deploy sent no commit",
		deploycommit.ReasonUnread:    "the CLI couldn't read the checkout's commit",
	} {
		assert.Equal(t, want, reasonText(deploycommit.Reason{Kind: kind}, shop))
	}
}

// settingsServer answers the project's LavaFlow settings read with status
// and body, and counts the reads.
func settingsServer(t *testing.T, status int, body any) (*httptest.Server, *int) {
	t.Helper()
	reads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/lavaflow/settings" || r.URL.Query().Get("project_id") != sourceProjectID {
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			http.NotFound(w, r)
			return
		}
		reads++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		assert.NoError(t, json.NewEncoder(w).Encode(body))
	}))
	t.Cleanup(server.Close)
	return server, &reads
}

func lavaflowSettings(enabled bool, repository any) map[string]any {
	return map[string]any{
		"project_id": sourceProjectID, "enabled": enabled, "autonomy": "open_pr", "repository": repository,
	}
}

var shopRepository = map[string]any{
	"repo_full_name": "acme/shop", "production_branch": "main", "repo_id": 1, "repo_installation_id": 2,
	"root_directory": "", "updated_at": "2026-11-01T00:00:00Z",
}

// outsideCheckout records a function deployed from a directory outside any
// Git checkout, which reports no commit.
func outsideCheckout(t *testing.T) *deploycommit.Uploads {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "")
	t.Setenv("VOLCANO_PROJECT_ID", "")
	t.Setenv("VOLCANO_API_URL", "")
	t.Setenv(deploycommit.OptOutEnv, "")
	require.NoError(t, (&cliconfig.Config{
		UserToken:      "token",
		CurrentProject: &cliconfig.ProjectConfig{ID: sourceProjectID, Name: "Shop"},
	}).Save())
	uploads := &deploycommit.Uploads{}
	uploads.AddFunction("function", "hello", t.TempDir(), nil, nil)
	return uploads
}

func warn(server *httptest.Server, deps cliruntime.Deps, uploads *deploycommit.Uploads) string {
	deps.HTTPClient, deps.APIBaseURL = server.Client(), server.URL
	var errOut bytes.Buffer
	WarnUnreadableSource(context.Background(), deps, &errOut, uploads)
	return errOut.String()
}

func TestWarnUnreadableSourceWhenLavaFlowIsOn(t *testing.T) {
	uploads := outsideCheckout(t)
	server, reads := settingsServer(t, http.StatusOK, lavaflowSettings(true, shopRepository))

	assert.Equal(t, "Warning: LavaFlow can't read this deployment's code to fix errors it serves: "+
		"the deploy didn't run in a Git checkout, so it sent no commit.\n"+
		"Deploy from a clean checkout of a commit pushed to acme/shop's main branch, or deploy with a Git push.\n",
		warn(server, cliruntime.Deps{}, uploads))
	assert.Equal(t, 1, *reads)
}

// The warning is a hint: whenever LavaFlow wouldn't read the code anyway, or
// the settings can't be read, the deploy prints nothing more.
func TestWarnUnreadableSourceStaysQuiet(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		body   any
	}{
		"LavaFlow off":            {http.StatusOK, lavaflowSettings(false, shopRepository)},
		"no connected repository": {http.StatusOK, lavaflowSettings(true, nil)},
		"LavaFlow unavailable":    {http.StatusNotFound, map[string]any{"error": "not found", "code": "feature_unavailable"}},
		"settings read failing":   {http.StatusInternalServerError, map[string]any{"error": "internal error"}},
	} {
		t.Run(name, func(t *testing.T) {
			uploads := outsideCheckout(t)
			server, reads := settingsServer(t, tc.status, tc.body)

			assert.Empty(t, warn(server, cliruntime.Deps{}, uploads))
			assert.Equal(t, 1, *reads)
		})
	}
}

// Local mode and a deploy that uploaded nothing never ask.
func TestWarnUnreadableSourceSkipsLocalModeAndEmptyDeploys(t *testing.T) {
	uploads := outsideCheckout(t)
	server, reads := settingsServer(t, http.StatusOK, lavaflowSettings(true, shopRepository))

	assert.Empty(t, warn(server, cliruntime.Deps{LocalMode: true}, uploads))
	assert.Empty(t, warn(server, cliruntime.Deps{}, &deploycommit.Uploads{}))
	assert.Empty(t, warn(server, cliruntime.Deps{}, nil))
	assert.Zero(t, *reads)
}
