package share

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	projectID = "22222222-2222-4222-8222-222222222222"
	linkID    = "k3Vn8TqPz0aX"
	releaseID = "0b6c1f1e-4a55-4d6f-9c3e-5d2f7a1b9e10"

	sharesPath   = "/shares"
	sharePath    = "/shares/" + linkID
	draftsPath   = "/share-release-drafts"
	releasesPath = "/share-releases"
	statsPath    = "/share-stats"
)

// fakeAPI answers the share routes and records what reached it, in order.
type fakeAPI struct {
	t     *testing.T
	mu    sync.Mutex
	links []map[string]any
	// draft answers each release-draft read; publish each publish.
	draft   func(call int) (int, any)
	publish func(call int, body map[string]any) (int, any)
	create  func(body map[string]any) (int, any)
	// share answers a read of the share; nil answers an active one.
	share func() map[string]any
	patch func(body map[string]any) (int, any)

	calls      []string
	queries    map[string][]string
	bodies     map[string][]map[string]any
	drafts     int
	publishes  int
	statsCalls int
}

func newFakeAPI(t *testing.T) (*fakeAPI, *httptest.Server) {
	t.Helper()
	fake := &fakeAPI{t: t, bodies: map[string][]map[string]any{}, queries: map[string][]string{}}
	fake.draft = func(int) (int, any) { return http.StatusOK, draftPayload("admin") }
	fake.publish = func(int, map[string]any) (int, any) { return http.StatusCreated, releasePayload() }
	fake.create = func(map[string]any) (int, any) { return http.StatusCreated, linkPayload("active") }
	fake.patch = func(body map[string]any) (int, any) {
		link := linkPayload("active")
		if body["status"] == "disabled" {
			link = linkPayload("disabled")
		}
		return http.StatusOK, link
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		call := r.Method + " " + r.URL.Path
		fake.calls = append(fake.calls, call)
		fake.queries[call] = append(fake.queries[call], r.URL.RawQuery)
		var body map[string]any
		if r.Body != nil {
			raw, _ := io.ReadAll(r.Body)
			if len(raw) > 0 {
				require.NoError(t, json.Unmarshal(raw, &body))
				fake.bodies[call] = append(fake.bodies[call], body)
			}
		}
		var status int
		var value any
		switch call {
		case "GET " + sharesPath:
			status, value = http.StatusOK, map[string]any{"shares": fake.links}
		case "POST " + sharesPath:
			status, value = fake.create(body)
		case "GET " + sharePath:
			status, value = http.StatusOK, linkPayload("active")
			if fake.share != nil {
				value = fake.share()
			}
		case "PATCH " + sharePath:
			status, value = fake.patch(body)
		case "GET " + draftsPath:
			fake.drafts++
			status, value = fake.draft(fake.drafts)
		case "POST " + releasesPath:
			fake.publishes++
			status, value = fake.publish(fake.publishes, body)
		case "GET " + releasesPath:
			status, value = http.StatusOK, map[string]any{"data": []any{releasePayload()}, "limit": 100, "has_more": false}
		case "GET " + statsPath:
			fake.statsCalls++
			status, value = http.StatusOK, map[string]any{"since": "2026-09-20T00:00:00Z", "totals": []any{}, "daily": []any{}}
		default:
			t.Errorf("unexpected request %s", call)
			status, value = http.StatusNotFound, map[string]any{"error": "not found"}
		}
		if retry, ok := value.(retryAfter); ok {
			w.Header().Set("Retry-After", retry.seconds)
			value = retry.body
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		require.NoError(t, json.NewEncoder(w).Encode(value))
	}))
	t.Cleanup(server.Close)
	return fake, server
}

// retryAfter is a fake answer that carries a Retry-After header.
type retryAfter struct {
	seconds string
	body    any
}

// steps records which of the put-on-cloud steps ran.
type steps struct {
	connected, deployed int
	connectName         string
	connectErr          error
	deployErr           error
}

func testOptions(t *testing.T, server *httptest.Server, stdin string, dir directory, fromGit bool, record *steps) (options, *bytes.Buffer) {
	t.Helper()
	var out bytes.Buffer
	return options{
		deps:   cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL},
		in:     iotest.OneByteReader(strings.NewReader(stdin)),
		out:    &out,
		stdout: &out,
		errOut: &out,
		connect: func(_ context.Context, opts options) error {
			record.connected++
			record.connectName = opts.newProjectName
			return record.connectErr
		},
		deploy: func(context.Context, options) error {
			record.deployed++
			return record.deployErr
		},
		directory:      func() directory { return dir },
		deploysFromGit: func(context.Context, options) bool { return fromGit },
	}, &out
}

func linkedDir() directory {
	return directory{path: "/work/shop", link: &cliconfig.Link{Dir: "/work/shop", Project: cliconfig.ProjectConfig{ID: projectID, Name: "Beta"}}}
}

// Outside a project directory, share publishes what the project runs: every
// draft variable and exactly the draft's service key functions, and prints the
// link and the button snippets.
func TestShareOutsideAProjectSharesWhatTheProjectRuns(t *testing.T) {
	setTestHome(t)
	fake, server := newFakeAPI(t)
	record := &steps{}
	opts, out := testOptions(t, server, "", directory{}, false, record)
	opts.yes = true
	description := "Ask a question, get a checked answer."
	opts.description = &description

	require.NoError(t, run(t.Context(), opts))

	assert.Zero(t, record.connected+record.deployed, "nothing is deployed outside a project directory")
	assert.Equal(t, []map[string]any{{"project_id": projectID, "description": description}}, fake.bodies["POST "+sharesPath],
		"a new share names the project and carries the description")
	assert.Equal(t, []string{"share_id=" + linkID + "&project_id=" + projectID}, fake.queries["GET "+draftsPath])
	assert.Equal(t, []map[string]any{{
		"share_id": linkID, "draft_id": "draft-1",
		"variables": []any{"NEXT_PUBLIC_TITLE", "OPENAI_API_KEY"}, "service_key_functions": []any{"admin"},
	}}, fake.bodies["POST "+releasesPath])
	for _, want := range []string{
		"This shares", "Variables: NEXT_PUBLIC_TITLE (starts empty), OPENAI_API_KEY (starts empty, secret)",
		"Each copy's service key, which bypasses row-level security, goes to: admin (confirmed by --yes).",
		"Shared. Anyone can fork it from: https://volcano.dev/deploy/" + linkID,
		"volcano fork https://volcano.dev/deploy/" + linkID,
		"[![Deploy with Volcano](https://volcano.dev/deploy-button.svg)](https://volcano.dev/deploy/" + linkID + ")",
	} {
		assert.Contains(t, out.String(), want)
	}
}

// A linked project directory is deployed to its cloud project first, so the
// share is what is in the directory; the owner confirms the deploy, the
// service key functions and the share.
func TestShareDeploysALinkedDirectoryBeforeSharing(t *testing.T) {
	setTestHome(t)
	interactive(t)
	fake, server := newFakeAPI(t)
	fake.links = []map[string]any{linkPayload("active")}
	record := &steps{}
	opts, out := testOptions(t, server, "y\ny\ny\n", linkedDir(), false, record)

	require.NoError(t, run(t.Context(), opts))

	assert.Equal(t, 1, record.deployed)
	assert.Zero(t, record.connected, "a linked directory keeps its project")
	assert.Contains(t, out.String(), "Deploys this directory to Beta, then shares it.")
	assert.Contains(t, out.String(), "Give them the service key?")
	assert.Contains(t, out.String(), "Share it?")
	assert.Equal(t, 1, fake.publishes)
	assert.NotContains(t, fake.calls, "POST "+sharesPath, "the project's share is reused")
}

func TestShareNoDeploySharesWhatTheProjectRuns(t *testing.T) {
	setTestHome(t)
	fake, server := newFakeAPI(t)
	record := &steps{}
	opts, _ := testOptions(t, server, "", linkedDir(), false, record)
	opts.yes, opts.noDeploy = true, true

	require.NoError(t, run(t.Context(), opts))
	assert.Zero(t, record.deployed)
	assert.Equal(t, 1, fake.publishes)
}

// --no-deploy in a directory not on Volcano Cloud yet creates no project and
// deploys nothing: it shares the selected cloud project as it runs.
func TestShareNoDeployInAnUnlinkedDirectoryCreatesNothing(t *testing.T) {
	setTestHome(t)
	fake, server := newFakeAPI(t)
	record := &steps{}
	opts, _ := testOptions(t, server, "", directory{path: "/work/shop"}, false, record)
	opts.yes, opts.noDeploy = true, true

	require.NoError(t, run(t.Context(), opts))
	assert.Zero(t, record.connected+record.deployed)
	assert.Equal(t, 1, fake.publishes)
}

// --env-file is deployed with a project directory; outside one it would be
// ignored, so it is refused before anything is shared.
func TestShareRefusesAnEnvFileOutsideAProjectDirectory(t *testing.T) {
	setTestHome(t)
	fake, server := newFakeAPI(t)
	opts, _ := testOptions(t, server, "", directory{}, false, &steps{})
	opts.yes, opts.envFile = true, "prod.env"

	err := run(t.Context(), opts)
	require.ErrorContains(t, err, "--env-file is deployed with a project directory, and this is not one")
	assert.Contains(t, err.Error(), "volcano cloud variables deploy --file prod.env")
	assert.Empty(t, fake.calls)
}

func TestShareRefusesAnEnvFileForAProjectThatDeploysFromGit(t *testing.T) {
	setTestHome(t)
	fake, server := newFakeAPI(t)
	record := &steps{}
	opts, _ := testOptions(t, server, "", linkedDir(), true, record)
	opts.yes, opts.envFile = true, "prod.env"

	require.ErrorContains(t, run(t.Context(), opts), "Beta deploys from its Git repository, so this directory and its env file are not deployed")
	assert.Zero(t, record.deployed)
	assert.Empty(t, fake.calls)
}

// VOLCANO_PROJECT_ID selecting another project than the directory's link
// would deploy and share that project after the owner confirmed the linked
// one, so nothing is deployed.
func TestShareRefusesAProjectOverrideThatIsNotTheLinkedProject(t *testing.T) {
	setTestHome(t)
	other := "44444444-4444-4444-8444-444444444444"
	t.Setenv("VOLCANO_PROJECT_ID", other)
	fake, server := newFakeAPI(t)
	record := &steps{}
	opts, _ := testOptions(t, server, "", linkedDir(), false, record)
	opts.yes = true

	err := run(t.Context(), opts)
	require.ErrorContains(t, err, "VOLCANO_PROJECT_ID selects project "+other+", but this directory is linked to Beta")
	assert.Zero(t, record.deployed)
	assert.Empty(t, fake.calls)
}

// A project whose Git repository is its source refuses direct deploys, so share
// says so and shares what the project runs.
func TestShareNeverDeploysAProjectThatDeploysFromGit(t *testing.T) {
	setTestHome(t)
	fake, server := newFakeAPI(t)
	record := &steps{}
	opts, out := testOptions(t, server, "", linkedDir(), true, record)
	opts.yes = true

	require.NoError(t, run(t.Context(), opts))
	assert.Zero(t, record.deployed)
	assert.Contains(t, out.String(), "Beta deploys from its Git repository, so this directory is not deployed")
	assert.Equal(t, 1, fake.publishes)
}

// A project directory not on Volcano Cloud yet gets a cloud project and is
// deployed to it, then shared.
func TestShareCreatesDeploysAndSharesALocalOnlyProject(t *testing.T) {
	setTestHome(t)
	interactive(t)
	fake, server := newFakeAPI(t)
	record := &steps{}
	opts, out := testOptions(t, server, "y\ny\ny\n", directory{path: "/work/shop"}, false, record)

	require.NoError(t, run(t.Context(), opts))

	assert.Equal(t, 1, record.connected)
	assert.Equal(t, "shop", record.connectName, "connect is given the confirmed name, so it never asks for one")
	assert.Equal(t, 1, record.deployed)
	for _, want := range []string{
		"This project isn't on Volcano Cloud yet.", "Create cloud project shop, deploy it, and share it?",
		"[1/3] Create the cloud project and link this directory", "[2/3] Deploy this directory to it", "[3/3] Share it",
	} {
		assert.Contains(t, out.String(), want)
	}
	assert.Equal(t, 1, fake.publishes)
}

func TestShareDeclinedPutsNothingOnTheCloud(t *testing.T) {
	setTestHome(t)
	interactive(t)
	fake, server := newFakeAPI(t)
	record := &steps{}
	opts, _ := testOptions(t, server, "n\n", directory{path: "/work/shop"}, false, record)

	require.NoError(t, run(t.Context(), opts))
	assert.Zero(t, record.connected+record.deployed)
	assert.Empty(t, fake.calls)
}

// A failed step stops the run, says what exists, and how to continue.
func TestShareStopsAtAFailedStepAndSaysHowToContinue(t *testing.T) {
	t.Run("connect", func(t *testing.T) {
		setTestHome(t)
		fake, server := newFakeAPI(t)
		record := &steps{connectErr: errors.New("project limit reached")}
		opts, _ := testOptions(t, server, "", directory{path: "/work/shop"}, false, record)
		opts.yes = true

		err := run(t.Context(), opts)
		require.ErrorContains(t, err, "creating the cloud project: project limit reached")
		assert.Contains(t, err.Error(), "Nothing was deployed or shared")
		assert.Zero(t, record.deployed)
		assert.Empty(t, fake.calls)
	})
	t.Run("deploy", func(t *testing.T) {
		setTestHome(t)
		fake, server := newFakeAPI(t)
		record := &steps{deployErr: errors.New("functions: build failed")}
		opts, _ := testOptions(t, server, "", directory{path: "/work/shop"}, false, record)
		opts.yes = true

		err := run(t.Context(), opts)
		require.ErrorContains(t, err, "deploying to the cloud project: functions: build failed")
		assert.Contains(t, err.Error(), "The cloud project exists and this directory is linked to it")
		assert.Contains(t, err.Error(), "run volcano share again: it uses the linked project")
		assert.Empty(t, fake.calls, "nothing is shared after a failed deploy")
	})
}

// A link that was switched off goes on only after the new release is
// published, so a failed publish never switches an old release back on.
func TestShareSwitchesALinkOnOnlyAfterPublishing(t *testing.T) {
	setTestHome(t)
	fake, server := newFakeAPI(t)
	fake.links = []map[string]any{linkPayload("disabled")}
	opts, _ := testOptions(t, server, "", directory{}, false, &steps{})
	opts.yes = true

	require.NoError(t, run(t.Context(), opts))
	publishAt, patchAt := indexOf(fake.calls, "POST "+releasesPath), indexOf(fake.calls, "PATCH "+sharePath)
	require.GreaterOrEqual(t, publishAt, 0)
	assert.Greater(t, patchAt, publishAt)
	assert.Equal(t, []map[string]any{{"status": "active"}}, fake.bodies["PATCH "+sharePath])
}

func TestShareFailedPublishLeavesAnOffLinkOff(t *testing.T) {
	setTestHome(t)
	fake, server := newFakeAPI(t)
	fake.links = []map[string]any{linkPayload("disabled")}
	fake.publish = func(int, map[string]any) (int, any) {
		return http.StatusUnprocessableEntity, map[string]any{"error": "The project's deployed source cannot be released as it is."}
	}
	opts, _ := testOptions(t, server, "", directory{}, false, &steps{})
	opts.yes = true

	require.Error(t, run(t.Context(), opts))
	assert.NotContains(t, fake.calls, "PATCH "+sharePath)
}

func TestShareSetsDescriptionAndResharingOnAnExistingLink(t *testing.T) {
	setTestHome(t)
	fake, server := newFakeAPI(t)
	fake.links = []map[string]any{linkPayload("active")}
	opts, _ := testOptions(t, server, "", directory{}, false, &steps{})
	opts.yes = true
	description := "Notes that sync"
	opts.description, opts.allowReshare = &description, new(true)

	require.NoError(t, run(t.Context(), opts))
	assert.Equal(t, []map[string]any{{"description": description, "allow_reshare": true}}, fake.bodies["PATCH "+sharePath])
}

// --description and --allow-reshare change an existing share only once the
// release is published, so declining leaves the live share as it was.
func TestShareDeclinedLeavesTheDescriptionAndResharingAlone(t *testing.T) {
	setTestHome(t)
	interactive(t)
	fake, server := newFakeAPI(t)
	fake.links = []map[string]any{linkPayload("active")}
	opts, _ := testOptions(t, server, "y\nn\n", directory{}, false, &steps{})
	description := "Notes that sync"
	opts.description, opts.allowReshare = &description, new(true)

	require.NoError(t, run(t.Context(), opts))
	assert.Zero(t, fake.publishes)
	assert.NotContains(t, fake.calls, "PATCH "+sharePath)
}

func TestShareChangesDescriptionOnlyAfterPublishing(t *testing.T) {
	setTestHome(t)
	fake, server := newFakeAPI(t)
	fake.links = []map[string]any{linkPayload("disabled")}
	opts, _ := testOptions(t, server, "", directory{}, false, &steps{})
	opts.yes = true
	description := "Notes that sync"
	opts.description = &description

	require.NoError(t, run(t.Context(), opts))
	publishAt, patchAt := indexOf(fake.calls, "POST "+releasesPath), indexOf(fake.calls, "PATCH "+sharePath)
	require.GreaterOrEqual(t, publishAt, 0)
	assert.Greater(t, patchAt, publishAt)
	assert.Equal(t, []map[string]any{{"description": description, "status": "active"}}, fake.bodies["PATCH "+sharePath],
		"one update switches the share on and sets the description")
}

func TestShareExplainsACopyThatMayNotBeShared(t *testing.T) {
	setTestHome(t)
	fake, server := newFakeAPI(t)
	fake.create = func(map[string]any) (int, any) {
		return http.StatusForbidden, map[string]any{
			"error": "This project is a copy of someone else's shared project, and they don't allow re-sharing.",
			"code":  "reshare_not_allowed",
		}
	}
	opts, _ := testOptions(t, server, "", directory{}, false, &steps{})
	opts.yes = true

	err := run(t.Context(), opts)
	require.ErrorContains(t, err, "they don't allow re-sharing")
	assert.Contains(t, err.Error(), "its source does not let copies be shared again")
	assert.Zero(t, fake.drafts)
}

// A fresh build that asks for a variable the owner did not confirm is not
// published under --yes: the command shows it and stops.
func TestShareProjectChangedStopsUnderYes(t *testing.T) {
	setTestHome(t)
	fake, server := newFakeAPI(t)
	fake.publish = func(int, map[string]any) (int, any) {
		draft := draftPayloadWithID("draft-2", "admin")
		draft["variables"] = append(draft["variables"].([]any), map[string]any{"name": "REGION", "required": true, "secret": true})
		return http.StatusConflict, map[string]any{
			"error": "A release carries every draft variable; confirm these too: REGION.", "code": "draft_changed",
			"draft": draft,
		}
	}
	opts, out := testOptions(t, server, "", directory{}, false, &steps{})
	opts.yes = true

	err := run(t.Context(), opts)
	require.ErrorContains(t, err, "nothing was shared, because the project changed")
	assert.Contains(t, out.String(), "confirm these too: REGION")
	assert.Contains(t, out.String(), "REGION (starts empty, secret)", "the fresh build is shown")
	assert.Equal(t, 1, fake.publishes)
}

// A fresh build with nothing the owner did not already confirm is published
// once with its own draft id, without asking again.
func TestShareProjectChangedRetriesOnceWithWhatWasConfirmed(t *testing.T) {
	setTestHome(t)
	fake, server := newFakeAPI(t)
	fake.publish = func(call int, _ map[string]any) (int, any) {
		if call == 1 {
			return http.StatusConflict, map[string]any{
				"error": "The project changed since the draft was reviewed.", "code": "draft_changed",
				"draft": draftPayloadWithID("draft-2", "admin"),
			}
		}
		return http.StatusCreated, releasePayload()
	}
	opts, out := testOptions(t, server, "", directory{}, false, &steps{})
	opts.yes = true

	require.NoError(t, run(t.Context(), opts))
	require.Len(t, fake.bodies["POST "+releasesPath], 2)
	assert.Equal(t, "draft-1", fake.bodies["POST "+releasesPath][0]["draft_id"])
	assert.Equal(t, "draft-2", fake.bodies["POST "+releasesPath][1]["draft_id"])
	assert.Contains(t, out.String(), "Its new build has the variables and service key functions you confirmed")
	assert.Contains(t, out.String(), "Shared. Anyone can fork it from")
}

// The retry happens once: a second changed build goes to the owner.
func TestShareProjectChangedTwiceStopsUnderYes(t *testing.T) {
	setTestHome(t)
	fake, server := newFakeAPI(t)
	fake.publish = func(call int, _ map[string]any) (int, any) {
		return http.StatusConflict, map[string]any{
			"error": "The project changed since the draft was reviewed.", "code": "draft_changed",
			"draft": draftPayloadWithID(fmt.Sprintf("draft-%d", call+1), "admin"),
		}
	}
	opts, _ := testOptions(t, server, "", directory{}, false, &steps{})
	opts.yes = true

	require.ErrorContains(t, run(t.Context(), opts), "nothing was shared, because the project changed")
	assert.Equal(t, 2, fake.publishes)
}

// At a terminal, a fresh build that gives the service key to a new function is
// shown and confirmed again before it is published.
func TestShareProjectChangedAsksAgainAtATerminal(t *testing.T) {
	setTestHome(t)
	interactive(t)
	fake, server := newFakeAPI(t)
	fake.publish = func(call int, _ map[string]any) (int, any) {
		if call == 1 {
			return http.StatusConflict, map[string]any{
				"error": "Confirm the service key functions the draft expects: admin, worker.", "code": "draft_changed",
				"draft": draftPayloadWithID("draft-2", "admin", "worker"),
			}
		}
		return http.StatusCreated, releasePayload()
	}
	opts, out := testOptions(t, server, "y\ny\ny\ny\n", directory{}, false, &steps{})

	require.NoError(t, run(t.Context(), opts))
	require.Len(t, fake.bodies["POST "+releasesPath], 2)
	second := fake.bodies["POST "+releasesPath][1]
	assert.Equal(t, "draft-2", second["draft_id"])
	assert.Equal(t, []any{"admin", "worker"}, second["service_key_functions"])
	assert.Equal(t, 2, strings.Count(out.String(), "Give them the service key?"))
}

// The refusals the share routes document say what to do.
func TestShareExplainsDocumentedRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		answer any
		want   []string
	}{
		{
			name: "taken down", status: http.StatusForbidden,
			answer: map[string]any{"error": "A share of this project was taken down.", "code": "operator_disabled"},
			want:   []string{"Volcano took a share of this project down", "Contact support"},
		},
		{
			name: "source unavailable", status: http.StatusForbidden,
			answer: map[string]any{"error": "The source is not available.", "code": "source_unavailable"},
			want:   []string{"the share it was made from is not available to share from right now"},
		},
		{
			name: "build budget", status: http.StatusTooManyRequests,
			answer: retryAfter{seconds: "600", body: map[string]any{"error": "Too many release builds.", "code": "release_builds_limited"}},
			want:   []string{"30 are allowed an hour", "Try again in 10m0s."},
		},
		{
			name: "builds busy", status: http.StatusServiceUnavailable,
			answer: retryAfter{seconds: "20", body: map[string]any{"error": "Release builds are busy.", "code": "builds_busy"}},
			want:   []string{"Volcano is busy building other releases", "Try again in 20s."},
		},
		{
			name: "not available", status: http.StatusNotFound,
			answer: map[string]any{"error": "not available", "code": "feature_unavailable"},
			want:   []string{"Publishing a shared project's release is not available in this environment."},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setTestHome(t)
			fake, server := newFakeAPI(t)
			fake.draft = func(int) (int, any) { return tc.status, tc.answer }
			opts, _ := testOptions(t, server, "", directory{}, false, &steps{})
			opts.yes = true

			err := run(t.Context(), opts)
			require.Error(t, err)
			for _, want := range tc.want {
				assert.Contains(t, err.Error(), want)
			}
			assert.Zero(t, fake.publishes)
		})
	}
}

// A published share whose link does not serve says so instead of claiming
// anyone can fork it. --link takes the share's link.
func TestShareSaysWhenTheLinkDoesNotServe(t *testing.T) {
	setTestHome(t)
	fake, server := newFakeAPI(t)
	fake.share = func() map[string]any {
		share := linkPayload("active")
		share["serving"] = false
		return share
	}
	opts, out := testOptions(t, server, "", directory{}, false, &steps{})
	opts.yes = true
	opts.linkID = "https://volcano.dev/deploy/" + linkID

	require.NoError(t, run(t.Context(), opts))
	assert.NotContains(t, fake.calls, "GET "+sharesPath, "--link names the share")
	assert.Contains(t, out.String(), "Published. Its link is https://volcano.dev/deploy/"+linkID)
	assert.Contains(t, out.String(), "the link is not serving right now")
	assert.NotContains(t, out.String(), "Anyone can fork it")
}

func TestShareStatusSaysVolcanoTookTheShareDown(t *testing.T) {
	setTestHome(t)
	fake, server := newFakeAPI(t)
	taken := linkPayload("disabled")
	taken["disabled_reason"] = "operator"
	fake.links = []map[string]any{taken}
	deps := cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}

	out, err := runCommand(t, New(deps), "status")
	require.NoError(t, err)
	assert.Contains(t, out, "Volcano took this share down, and only Volcano can switch it on again")
}

func TestShareDeclinedServiceKeySharesNothing(t *testing.T) {
	setTestHome(t)
	interactive(t)
	fake, server := newFakeAPI(t)
	opts, out := testOptions(t, server, "n\n", directory{}, false, &steps{})

	require.NoError(t, run(t.Context(), opts))
	assert.Zero(t, fake.publishes)
	assert.Contains(t, out.String(), "Nothing was shared. Sharing needs the service key functions confirmed.")
}

func TestShareWithoutYesFailsWhereNothingCanAnswer(t *testing.T) {
	setTestHome(t)
	t.Setenv("VOLCANO_NONINTERACTIVE", "1")
	fake, server := newFakeAPI(t)
	opts, _ := testOptions(t, server, "", directory{path: "/work/shop"}, false, &steps{})

	require.ErrorContains(t, run(t.Context(), opts), "pass --yes")
	assert.Empty(t, fake.calls)
}

func TestShareJSONPrintsTheLinkReleaseAndSnippets(t *testing.T) {
	setTestHome(t)
	_, server := newFakeAPI(t)
	opts, _ := testOptions(t, server, "", directory{}, false, &steps{})
	var stdout bytes.Buffer
	opts.yes, opts.jsonOutput, opts.stdout = true, true, &stdout

	require.NoError(t, run(t.Context(), opts))
	var got map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &got), stdout.String())
	assert.Equal(t, "https://volcano.dev/deploy/"+linkID, got["url"])
	assert.Contains(t, got["markdown"], "Deploy with Volcano")
	assert.NotNil(t, got["release"])
}

func TestShareOffStatusAndReleases(t *testing.T) {
	setTestHome(t)
	fake, server := newFakeAPI(t)
	fake.links = []map[string]any{linkPayload("active")}
	deps := cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}

	out, err := runCommand(t, New(deps), "status")
	require.NoError(t, err)
	assert.Contains(t, out, "Sharing: on")
	assert.Contains(t, out, "Link: https://volcano.dev/deploy/"+linkID)
	assert.Contains(t, out, "Contents: 2 function(s), 1 frontend(s), 2 variable(s)")
	assert.Equal(t, 1, fake.statsCalls)

	out, err = runCommand(t, New(deps), "off")
	require.NoError(t, err)
	assert.Contains(t, out, "Sharing is off.")
	assert.Equal(t, []map[string]any{{"status": "disabled"}}, fake.bodies["PATCH "+sharePath])

	out, err = runCommand(t, New(deps), "releases")
	require.NoError(t, err)
	assert.Contains(t, out, releaseID)
}

func TestShareSubcommandsSayWhenNothingIsShared(t *testing.T) {
	setTestHome(t)
	_, server := newFakeAPI(t)
	deps := cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}
	for _, sub := range []string{"status", "off", "releases"} {
		_, err := runCommand(t, New(deps), sub)
		require.ErrorContains(t, err, "this project is not shared yet. Share it with volcano share", sub)
	}
}

func TestCheckDescription(t *testing.T) {
	got, err := checkDescription("  Notes that sync  ")
	require.NoError(t, err)
	assert.Equal(t, "Notes that sync", got)
	_, err = checkDescription("one\ntwo")
	require.ErrorContains(t, err, "one line")
	_, err = checkDescription(strings.Repeat("é", 281))
	require.ErrorContains(t, err, "is 281 characters; use at most 280")
}

func TestShareRefusesConflictingFlags(t *testing.T) {
	setTestHome(t)
	_, server := newFakeAPI(t)
	deps := cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}
	_, err := runCommand(t, New(deps), "--allow-reshare", "--no-allow-reshare")
	require.ErrorContains(t, err, "none of the others can be")
	_, err = runCommand(t, New(deps), "--no-deploy", "--env-file", "cloud.env")
	require.ErrorContains(t, err, "none of the others can be")
}

func runCommand(t *testing.T, cmd interface {
	SetOut(io.Writer)
	SetErr(io.Writer)
	SetArgs([]string)
	Execute() error
}, args ...string,
) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func indexOf(calls []string, call string) int {
	for i, c := range calls {
		if c == call {
			return i
		}
	}
	return -1
}

func setTestHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "")
	t.Setenv("VOLCANO_PROJECT_ID", "")
	t.Setenv("VOLCANO_API_URL", "")
	cfg := &cliconfig.Config{UserToken: "token", CurrentProject: &cliconfig.ProjectConfig{ID: projectID, Name: "Beta"}}
	require.NoError(t, cfg.Save())
}

// interactive makes the environment say a person is at the terminal, so the
// test's stdin answers the prompts, even when the tests run in CI.
func interactive(t *testing.T) {
	t.Helper()
	t.Setenv("CI", "")
	t.Setenv("VOLCANO_NONINTERACTIVE", "")
	t.Setenv("TERM", "xterm-256color")
}

func linkPayload(status string) map[string]any {
	link := map[string]any{
		"id": linkID, "project_id": projectID, "suggested_name": "my-app", "description": "", "allow_reshare": false,
		"status": status, "serving": status == "active",
		"current_release": map[string]any{"id": releaseID, "published_at": "2026-10-20T09:00:00Z"},
		"created_at":      "2026-10-19T21:12:00Z", "updated_at": "2026-10-19T21:12:00Z",
	}
	if status == "disabled" {
		link["disabled_reason"] = "owner"
	}
	return link
}

func draftPayload(serviceKey ...string) map[string]any {
	return draftPayloadWithID("draft-1", serviceKey...)
}

func draftPayloadWithID(draftID string, serviceKey ...string) map[string]any {
	if serviceKey == nil {
		serviceKey = []string{}
	}
	functions := []any{map[string]any{"name": "api", "kind": "standard", "visibility": "public"}}
	for _, name := range serviceKey {
		functions = append(functions, map[string]any{"name": name, "kind": "standard", "visibility": "private"})
	}
	return map[string]any{
		"draft_id":  draftID,
		"functions": functions,
		"frontends": []string{"web"},
		"variables": []any{
			map[string]any{"name": "NEXT_PUBLIC_TITLE", "required": true, "secret": false},
			map[string]any{"name": "OPENAI_API_KEY", "required": true, "secret": true},
		},
		"service_key_functions": serviceKey,
		"omissions":             []any{map[string]any{"kind": "secret_file", "path": ".env"}},
		"frontend_variables":    map[string]any{"web": []string{"NEXT_PUBLIC_TITLE"}},
	}
}

func releasePayload() map[string]any {
	release := draftPayload("admin")
	release["id"] = releaseID
	delete(release, "draft_id")
	delete(release, "frontend_variables")
	release["share_id"] = linkID
	release["project_id"] = projectID
	release["published_at"] = "2026-10-20T09:00:00Z"
	release["published_by"] = "user_2a7f"
	release["sha256"] = strings.Repeat("ab", 32)
	release["size_bytes"] = 48211
	return release
}

func TestConnectArgsNameTheConfirmedProject(t *testing.T) {
	assert.Equal(t, []string{".", "--skip-skills", "--name", "shop"}, connectArgs(options{newProjectName: "shop"}))
}
