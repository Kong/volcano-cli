package durable

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestDurableListPopulatedAndEmpty(t *testing.T) {
	for _, tc := range []struct {
		name string
		body map[string]any
		want []string
	}{
		{
			name: "populated",
			body: map[string]any{
				"data":     []any{durableFunctionPayload("order-pipeline", false)},
				"has_more": true,
				"page":     1,
				"limit":    100,
				"total":    2,
			},
			want: []string{
				"order-pipeline",
				"nodejs24.x",
				"active",
				"Showing 1 of 2 durable function(s) (page 1, limit 100)",
				"Next page: volcano cloud durable list --page 2 --limit 100",
			},
		},
		{
			name: "empty",
			body: map[string]any{
				"data":     []any{},
				"has_more": false,
				"page":     1,
				"limit":    100,
				"total":    0,
			},
			want: []string{
				"No durable functions deployed",
				"Showing 0 of 0 durable function(s) (page 1, limit 100)",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setDurableCommandTestHome(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "Bearer token", r.Header.Get("Authorization"))
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/projects/"+durableProjectID+"/durable-functions", r.URL.Path)
				assert.Equal(t, "page=1&limit=100", r.URL.RawQuery)
				writeDurableCommandJSON(t, w, http.StatusOK, tc.body)
			}))
			defer server.Close()

			out, err := executeDurableCommand(t, newCloudDurableCommand(server), "list")
			require.NoError(t, err)
			for _, want := range tc.want {
				assert.Contains(t, out, want)
			}
		})
	}
}

// A durable function's visibility governs whether an anon key may start an
// execution, and nothing else: it is never invocable over HTTP the way a public
// standard function is, so the field is labeled for what it does.
func TestDurableGetRendersAnonKeyStart(t *testing.T) {
	for _, tc := range []struct {
		name     string
		isPublic bool
		want     string
	}{
		{name: "public", isPublic: true, want: "Anon Key Start: allowed"},
		{name: "private", isPublic: false, want: "Anon Key Start: denied"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setDurableCommandTestHome(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/projects/"+durableProjectID+"/durable-functions/order-pipeline", r.URL.Path)
				writeDurableCommandJSON(t, w, http.StatusOK, durableFunctionPayload("order-pipeline", tc.isPublic))
			}))
			defer server.Close()

			out, err := executeDurableCommand(t, newCloudDurableCommand(server), "get", "order-pipeline")
			require.NoError(t, err)
			assert.Contains(t, out, tc.want)
			assert.Contains(t, out, "Execution Timeout: 1h0m0s")
			assert.Contains(t, out, "Retention: 30 day(s)")
		})
	}
}

// The API answers 404 for a standard function's name here, because the two
// collections never accept each other's names. The message has to say which
// collection was searched, or a user who mixed the two reads it as a name typo.
func TestDurableGetNamesTheCollectionOnNotFound(t *testing.T) {
	setDurableCommandTestHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeDurableCommandJSON(t, w, http.StatusNotFound, map[string]any{"error": "durable function not found"})
	}))
	defer server.Close()

	_, err := executeDurableCommand(t, newCloudDurableCommand(server), "get", "hello")
	require.ErrorContains(t, err, `durable function "hello" not found`)
}

func TestDurableStartSendsInputAndIdempotencyName(t *testing.T) {
	setDurableCommandTestHome(t)
	var body map[string]any
	var executionName string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/projects/"+durableProjectID+"/durable-functions/order-pipeline/executions", r.URL.Path)
		executionName = r.Header.Get("X-Volcano-Execution-Name")
		require.NoError(t, decodeJSONBody(r, &body))
		writeDurableCommandJSON(t, w, http.StatusAccepted,
			durableExecutionPayload(durableExecutionA, "order-4417", "pending"))
	}))
	defer server.Close()

	out, err := executeDurableCommand(t, newCloudDurableCommand(server),
		"start", "order-pipeline", "--input", `{"order_id":4417}`, "--name", "order-4417")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"order_id": float64(4417)}, body)
	assert.Equal(t, "order-4417", executionName)
	assert.Contains(t, out, "Execution order-4417 started")
	assert.Contains(t, out, "volcano cloud durable executions get order-pipeline "+durableExecutionA)
}

func TestDurableStartReadsInputFromFile(t *testing.T) {
	setDurableCommandTestHome(t)
	t.Chdir(t.TempDir())
	writeDurableProjectFile(t, "order.json", `{"order_id": 99}`)

	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, decodeJSONBody(r, &body))
		writeDurableCommandJSON(t, w, http.StatusAccepted,
			durableExecutionPayload(durableExecutionA, "generated", "pending"))
	}))
	defer server.Close()

	_, err := executeDurableCommand(t, newCloudDurableCommand(server),
		"start", "order-pipeline", "--input", "order.json")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"order_id": float64(99)}, body)
}

func TestDurableStartRejectsInputThatIsNotAJSONObject(t *testing.T) {
	setDurableCommandTestHome(t)
	_, err := executeDurableCommand(t, newCloudDurableCommand(nil),
		"start", "order-pipeline", "--input", "not-json")
	require.ErrorContains(t, err, "input must be a JSON object")
}

// Deleting reads the function first, so the prompt names what is about to go
// and a name belonging to a standard function is refused before anything is
// torn down.
func TestDurableDeleteConfirmsAgainstTheResolvedFunction(t *testing.T) {
	setDurableCommandTestHome(t)
	deleted := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := "/projects/" + durableProjectID + "/durable-functions/"
		switch {
		case r.Method == http.MethodGet && r.URL.Path == path+"order-pipeline":
			writeDurableCommandJSON(t, w, http.StatusOK, durableFunctionPayload("order-pipeline", false))
		case r.Method == http.MethodDelete && r.URL.Path == path+durableFunctionID:
			deleted = true
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	out, err := executeDurableCommand(t, newCloudDurableCommand(server), "delete", "order-pipeline")
	require.NoError(t, err)
	assert.True(t, deleted)
	assert.Contains(t, out, "Delete durable function 'order-pipeline'?")
	assert.Contains(t, out, "Durable function 'order-pipeline' deletion started")
}

func TestDurableDeployRefusesConflictingVisibilityFlags(t *testing.T) {
	setDurableCommandTestHome(t)
	_, err := executeDurableCommand(t, newCloudDurableCommand(nil),
		"deploy", "-f", "order-pipeline", "--public", "--private")
	require.ErrorContains(t, err, "cannot use --public and --private together")
}

func TestDurableDeployRequiresATarget(t *testing.T) {
	setDurableCommandTestHome(t)
	_, err := executeDurableCommand(t, newCloudDurableCommand(nil), "deploy")
	require.ErrorContains(t, err, "specify either --all")

	_, err = executeDurableCommand(t, newCloudDurableCommand(nil), "deploy", "--all", "-f", "order-pipeline")
	require.ErrorContains(t, err, "cannot use --all and --file together")
}

func TestDurableDeployUploadsSourceAndVisibility(t *testing.T) {
	setDurableCommandTestHome(t)
	t.Chdir(t.TempDir())
	writeDurableProjectFile(t, "volcano/functions/order-pipeline.js",
		`exports.handler = async () => ({ ok: true });`)

	var fields map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case writeDurableRuntimesResponse(t, w, r):
			return
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+durableProjectID+"/durable-functions":
			require.NoError(t, r.ParseMultipartForm(4*1024*1024))
			fields = map[string]string{
				"name":      r.FormValue("name"),
				"runtime":   r.FormValue("runtime"),
				"handler":   r.FormValue("handler"),
				"is_public": r.FormValue("is_public"),
			}
			require.Len(t, r.MultipartForm.File["code"], 1)
			writeDurableCommandJSON(t, w, http.StatusCreated, durableFunctionPayload("order-pipeline", true))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	out, err := executeDurableCommand(t, newCloudDurableCommand(server),
		"deploy", "-f", "volcano/functions/order-pipeline.js", "--public")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"name": "order-pipeline", "runtime": "nodejs24.x", "handler": "handler", "is_public": "true",
	}, fields)
	assert.Contains(t, out, "Deploying order-pipeline")
	assert.Contains(t, out, "1/1 durable function(s) deployment started")
}

// Durable execution needs the durable authoring API, so only some runtimes
// qualify. Refusing before the archive is built keeps the message able to name
// the file the runtime was inferred from, which the API's rejection cannot.
func TestDurableDeployRefusesARuntimeWithoutDurableSupport(t *testing.T) {
	setDurableCommandTestHome(t)
	t.Chdir(t.TempDir())
	writeDurableProjectFile(t, "volcano/functions/order-pipeline.py", "def handler(event, context):\n    return {}\n")

	var deployed bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case writeDurableRuntimesResponse(t, w, r):
			return
		case r.Method == http.MethodPost:
			deployed = true
			writeDurableCommandJSON(t, w, http.StatusCreated, durableFunctionPayload("order-pipeline", false))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	_, err := executeDurableCommand(t, newCloudDurableCommand(server), "deploy", "-f", "order-pipeline")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "durable functions cannot run on python3.12")
	assert.Contains(t, err.Error(), "order-pipeline.py")
	assert.Contains(t, err.Error(), "durable runtimes: nodejs24.x")
	assert.False(t, deployed, "nothing should be uploaded once the runtime is refused")
}

// --private is the way back from public, and a durable function has no update
// endpoint, so a redeploy that sends nothing instead of false would make the
// flag unusable.
func TestDurableDeploySendsPrivateVisibility(t *testing.T) {
	setDurableCommandTestHome(t)
	t.Chdir(t.TempDir())
	writeDurableProjectFile(t, "volcano/functions/order-pipeline.js",
		`exports.handler = async () => ({ ok: true });`)

	var sentIsPublic string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case writeDurableRuntimesResponse(t, w, r):
			return
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+durableProjectID+"/durable-functions":
			require.NoError(t, r.ParseMultipartForm(4*1024*1024))
			sentIsPublic = r.FormValue("is_public")
			writeDurableCommandJSON(t, w, http.StatusOK, durableFunctionPayload("order-pipeline", false))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	out, err := executeDurableCommand(t, newCloudDurableCommand(server),
		"deploy", "-f", "order-pipeline", "--private")
	require.NoError(t, err)
	assert.Equal(t, "false", sentIsPublic)
	assert.Contains(t, out, "1/1 durable function(s) deployment started")
}

// Omitting both visibility flags sends no is_public field, which the API reads
// as "keep what the function has". A redeploy must not silently make a public
// function private.
func TestDurableDeployLeavesVisibilityAloneByDefault(t *testing.T) {
	setDurableCommandTestHome(t)
	t.Chdir(t.TempDir())
	writeDurableProjectFile(t, "volcano/functions/order-pipeline.js",
		`exports.handler = async () => ({ ok: true });`)

	var sentIsPublic []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case writeDurableRuntimesResponse(t, w, r):
			return
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+durableProjectID+"/durable-functions":
			require.NoError(t, r.ParseMultipartForm(4*1024*1024))
			sentIsPublic = r.MultipartForm.Value["is_public"]
			writeDurableCommandJSON(t, w, http.StatusOK, durableFunctionPayload("order-pipeline", true))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	_, err := executeDurableCommand(t, newCloudDurableCommand(server),
		"deploy", "-f", "order-pipeline")
	require.NoError(t, err)
	assert.Empty(t, sentIsPublic)
}

// --all takes its targets from the manifest rather than from the scan, because
// the source tree does not say which functions are durable.
func TestDurableDeployAllTakesTargetsFromTheManifest(t *testing.T) {
	setDurableCommandTestHome(t)
	t.Chdir(t.TempDir())
	writeDurableProjectFile(t, "volcano/functions/order-pipeline.js", `exports.handler = async () => ({});`)
	writeDurableProjectFile(t, "volcano/functions/hello.js", `exports.handler = async () => ({});`)
	writeDurableProjectFile(t, "volcano-config.yaml", `version: 1
project:
  name: beta
functions:
  - name: hello
  - name: order-pipeline
    kind: durable
`)

	var deployed []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case writeDurableRuntimesResponse(t, w, r):
			return
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+durableProjectID+"/durable-functions":
			require.NoError(t, r.ParseMultipartForm(4*1024*1024))
			deployed = append(deployed, r.FormValue("name"))
			writeDurableCommandJSON(t, w, http.StatusCreated, durableFunctionPayload("order-pipeline", false))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	out, err := executeDurableCommand(t, newCloudDurableCommand(server), "deploy", "--all")
	require.NoError(t, err)
	assert.Equal(t, []string{"order-pipeline"}, deployed, "a standard function must not be deployed as durable")
	assert.Contains(t, out, "1/1 durable function(s) deployment started")
}

func TestDurableDeployAllWithoutADurableDeclaration(t *testing.T) {
	setDurableCommandTestHome(t)
	t.Chdir(t.TempDir())
	writeDurableProjectFile(t, "volcano-config.yaml", `version: 1
project:
  name: beta
functions:
  - name: hello
`)

	_, err := executeDurableCommand(t, newCloudDurableCommand(nil), "deploy", "--all")
	require.ErrorContains(t, err, "declares no durable functions")
}

func newCloudDurableCommand(server *httptest.Server) *cobra.Command {
	deps := cliruntime.Deps{CommandPathPrefix: "volcano cloud"}
	if server != nil {
		deps.HTTPClient = server.Client()
		deps.APIBaseURL = server.URL
	}
	return New(deps)
}

// Local mode has no durable engine behind the command. Cobra answers an unknown
// subcommand by printing help and exiting 0, so the stub is what makes the
// refusal legible — and it stays hidden, or local help would advertise a
// capability local mode does not have.
func TestDurableIsRefusedInTheLocalTree(t *testing.T) {
	for _, args := range [][]string{
		{"durable", "deploy", "--all"},
		{"durable", "list"},
		{"durable", "start", "order-pipeline", "--input", "{}"},
		{"durable", "executions", "list", "order-pipeline"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			root := &cobra.Command{Use: "volcano"}
			root.AddCommand(NewCloudOnly())
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&out)
			root.SetArgs(args)

			err := root.Execute()
			require.ErrorContains(t, err, `"durable" is a cloud command`)
			require.ErrorContains(t, err, "volcano cloud durable")
		})
	}

	root := &cobra.Command{Use: "volcano"}
	root.AddCommand(NewCloudOnly())
	var help bytes.Buffer
	root.SetOut(&help)
	root.SetArgs([]string{"--help"})
	require.NoError(t, root.Execute())
	assert.NotContains(t, help.String(), "durable")
}
