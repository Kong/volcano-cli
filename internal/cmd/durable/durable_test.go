package durable

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestDurableListPopulatedAndEmpty(t *testing.T) {
	for _, tc := range []struct {
		name  string
		body  map[string]any
		args  []string
		query string
		want  []string
	}{
		{
			name: "populated",
			body: map[string]any{
				"data":     []any{durableFunctionPayload("order-pipeline", "private")},
				"has_more": true,
				"page":     3,
				"limit":    25,
				"total":    61,
			},
			args:  []string{"list", "--page", "3", "--limit", "25"},
			query: "page=3&limit=25",
			want: []string{
				"order-pipeline",
				"nodejs24.x",
				"active",
				"Showing 1 of 61 durable function(s) (page 3, limit 25)",
				"Next page: volcano cloud durable list --page 4 --limit 25",
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
			args:  []string{"list"},
			query: "page=1&limit=100",
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
				assert.Equal(t, tc.query, r.URL.RawQuery)
				writeDurableCommandJSON(t, w, http.StatusOK, tc.body)
			}))
			defer server.Close()

			out, err := executeDurableCommand(t, newCloudDurableCommand(server), tc.args...)
			require.NoError(t, err)
			for _, want := range tc.want {
				assert.Contains(t, out, want)
			}
		})
	}
}

func TestDurableGetRendersVisibility(t *testing.T) {
	for _, visibility := range []string{"private", "authenticated", "public"} {
		t.Run(visibility, func(t *testing.T) {
			setDurableCommandTestHome(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/projects/"+durableProjectID+"/durable-functions/order-pipeline", r.URL.Path)
				writeDurableCommandJSON(t, w, http.StatusOK, durableFunctionPayload("order-pipeline", visibility))
			}))
			defer server.Close()

			out, err := executeDurableCommand(t, newCloudDurableCommand(server), "get", "order-pipeline")
			require.NoError(t, err)
			assert.Contains(t, out, "Visibility: "+visibility)
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

// An omitted --input starts the execution with no input at all. A JSON null
// body is not that: the API reads it as valid JSON and hands it to the function
// as its payload, so only an empty body carries "no input".
func TestDurableStartWithoutInputSendsNoBody(t *testing.T) {
	setDurableCommandTestHome(t)
	var raw []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		raw, err = io.ReadAll(r.Body)
		require.NoError(t, err)
		writeDurableCommandJSON(t, w, http.StatusAccepted,
			durableExecutionPayload(durableExecutionA, "generated", "pending"))
	}))
	defer server.Close()

	_, err := executeDurableCommand(t, newCloudDurableCommand(server), "start", "order-pipeline")
	require.NoError(t, err)
	assert.Empty(t, raw)
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
			writeDurableCommandJSON(t, w, http.StatusOK, durableFunctionPayload("order-pipeline", "private"))
		case r.Method == http.MethodDelete && r.URL.Path == path+durableFunctionID:
			deleted = true
			w.WriteHeader(http.StatusAccepted)
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
		"deploy", "-f", "order-pipeline", "--public", "--visibility", "authenticated")
	require.ErrorContains(t, err, "--public is the same as --visibility public")

	_, err = executeDurableCommand(t, newCloudDurableCommand(nil),
		"deploy", "-f", "order-pipeline", "--visibility", "everyone")
	require.ErrorContains(t, err, `invalid --visibility "everyone"`)
}

// --private used to keep signed-in users in, which is the authenticated level
// now. Mapping it to either level would surprise someone, so it is refused.
func TestDurableDeployRefusesThePrivateFlag(t *testing.T) {
	setDurableCommandTestHome(t)
	_, err := executeDurableCommand(t, newCloudDurableCommand(nil), "deploy", "-f", "order-pipeline", "--private")
	require.ErrorContains(t, err, "--private is no longer accepted")
	require.ErrorContains(t, err, "--visibility authenticated")
}

// Visibility is one value applied to every function a run deploys, and a durable
// function has no update endpoint: undoing an accidental flip is a redeploy of
// each one. So `--all --visibility public` would quietly make every function
// the manifest declares durable startable by the anon key.
func TestDurableDeployRefusesVisibilityFlagsWithAll(t *testing.T) {
	setDurableCommandTestHome(t)

	for _, flags := range [][]string{{"--public"}, {"--visibility", "authenticated"}} {
		_, err := executeDurableCommand(t, newCloudDurableCommand(nil), append([]string{"deploy", "--all"}, flags...)...)
		require.ErrorContains(t, err, "cannot use --visibility or --public with --all")
	}
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
				"name":       r.FormValue("name"),
				"runtime":    r.FormValue("runtime"),
				"handler":    r.FormValue("handler"),
				"visibility": r.FormValue("visibility"),
			}
			require.Len(t, r.MultipartForm.File["code"], 1)
			writeDurableCommandJSON(t, w, http.StatusCreated, durableFunctionPayload("order-pipeline", "authenticated"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	out, err := executeDurableCommand(t, newCloudDurableCommand(server),
		"deploy", "-f", "volcano/functions/order-pipeline.js", "--visibility", "authenticated")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"name": "order-pipeline", "runtime": "nodejs24.x", "handler": "handler", "visibility": "authenticated",
	}, fields)
	assert.Contains(t, out, "Deploying order-pipeline")
	assert.Contains(t, out, "Visibility: authenticated")
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
			writeDurableCommandJSON(t, w, http.StatusCreated, durableFunctionPayload("order-pipeline", "private"))
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

// A redeploy is the only way back from public, since a durable function has no
// update endpoint, and --public is still accepted as the old spelling.
func TestDurableDeploySendsEachVisibility(t *testing.T) {
	for _, tc := range []struct {
		flags []string
		want  string
	}{
		{flags: []string{"--visibility", "private"}, want: "private"},
		{flags: []string{"--visibility", "Authenticated"}, want: "authenticated"},
		{flags: []string{"--public"}, want: "public"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			testDurableDeploySendsVisibility(t, tc.flags, tc.want)
		})
	}
}

func testDurableDeploySendsVisibility(t *testing.T, flags []string, want string) {
	t.Helper()
	setDurableCommandTestHome(t)
	t.Chdir(t.TempDir())
	writeDurableProjectFile(t, "volcano/functions/order-pipeline.js",
		`exports.handler = async () => ({ ok: true });`)

	var sentVisibility, sentIsPublic []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case writeDurableRuntimesResponse(t, w, r):
			return
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+durableProjectID+"/durable-functions":
			require.NoError(t, r.ParseMultipartForm(4*1024*1024))
			sentVisibility = r.MultipartForm.Value["visibility"]
			sentIsPublic = r.MultipartForm.Value["is_public"]
			writeDurableCommandJSON(t, w, http.StatusOK, durableFunctionPayload("order-pipeline", want))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	out, err := executeDurableCommand(t, newCloudDurableCommand(server),
		append([]string{"deploy", "-f", "order-pipeline"}, flags...)...)
	require.NoError(t, err)
	assert.Equal(t, []string{want}, sentVisibility)
	assert.Empty(t, sentIsPublic)
	assert.Contains(t, out, "1/1 durable function(s) deployment started")
}

// Omitting the visibility flags sends no visibility field, which the API reads
// as "keep what the function has". A redeploy must not silently make a public
// function private.
func TestDurableDeployLeavesVisibilityAloneByDefault(t *testing.T) {
	setDurableCommandTestHome(t)
	t.Chdir(t.TempDir())
	writeDurableProjectFile(t, "volcano/functions/order-pipeline.js",
		`exports.handler = async () => ({ ok: true });`)

	var sent []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case writeDurableRuntimesResponse(t, w, r):
			return
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+durableProjectID+"/durable-functions":
			require.NoError(t, r.ParseMultipartForm(4*1024*1024))
			sent = slices.Concat(r.MultipartForm.Value["visibility"], r.MultipartForm.Value["is_public"])
			writeDurableCommandJSON(t, w, http.StatusOK, durableFunctionPayload("order-pipeline", "public"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	_, err := executeDurableCommand(t, newCloudDurableCommand(server),
		"deploy", "-f", "order-pipeline")
	require.NoError(t, err)
	assert.Empty(t, sent)
}

// A new durable function starts private, which refuses the signed-in users an
// app usually starts executions as, and there is no update command to fix that
// afterwards. The hint leads with config deploy, which needs no rebuild, and
// names the redeploy as the alternative for a function the manifest leaves
// alone. Existing functions and ones the manifest keeps private get nothing.
func TestDurableDeployHintsHowToOpenNewPrivateFunctions(t *testing.T) {
	setDurableCommandTestHome(t)
	t.Chdir(t.TempDir())
	for _, name := range []string{"order-pipeline", "reports", "nightly", "existing"} {
		writeDurableProjectFile(t, "volcano/functions/"+name+".js", `exports.handler = async () => ({});`)
	}
	writeDurableProjectFile(t, "volcano-config.yaml", `version: 1
functions:
  - name: order-pipeline
    kind: durable
  - name: reports
    kind: durable
    visibility: authenticated
  - name: nightly
    kind: durable
    visibility: private
  - name: existing
    kind: durable
`)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case writeDurableRuntimesResponse(t, w, r):
			return
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+durableProjectID+"/durable-functions":
			require.NoError(t, r.ParseMultipartForm(4*1024*1024))
			name := r.FormValue("name")
			status := http.StatusCreated
			if name == "existing" {
				status = http.StatusOK
			}
			writeDurableCommandJSON(t, w, status, durableFunctionPayload(name, "private"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	out, err := executeDurableCommand(t, newCloudDurableCommand(server), "deploy", "--all")
	require.NoError(t, err)
	assert.Contains(t, out, "New durable functions are private: only service keys and schedulers can start executions.\n"+
		"volcano-config.yaml declares a visibility for reports; apply it with:\n"+
		"  volcano cloud config deploy\n"+
		"To let your project's signed-in users in, declare visibility: authenticated for order-pipeline in volcano-config.yaml and run:\n"+
		"  volcano cloud config deploy\n"+
		"Or redeploy with --visibility, which rebuilds the function:\n"+
		"  volcano cloud durable deploy -f order-pipeline --visibility authenticated\n")
	assert.NotContains(t, out, "-f reports")
	assert.NotContains(t, out, "nightly;")
	assert.NotContains(t, out, "-f nightly")
	assert.NotContains(t, out, "-f existing")
}

func TestDurableDeployGivesNoHintWhenTheFlagChoosesPrivate(t *testing.T) {
	setDurableCommandTestHome(t)
	t.Chdir(t.TempDir())
	writeDurableProjectFile(t, "volcano/functions/order-pipeline.js", `exports.handler = async () => ({});`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case writeDurableRuntimesResponse(t, w, r):
			return
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+durableProjectID+"/durable-functions":
			writeDurableCommandJSON(t, w, http.StatusCreated, durableFunctionPayload("order-pipeline", "private"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	out, err := executeDurableCommand(t, newCloudDurableCommand(server),
		"deploy", "-f", "order-pipeline", "--visibility", "private")
	require.NoError(t, err)
	assert.Contains(t, out, "Visibility: private")
	assert.NotContains(t, out, "New durable functions are private")
}

// A server that predates visibility levels drops the multipart field it does
// not know and deploys without reporting a level. A deploy that asked for one
// fails; one that did not warns instead of guessing a level.
func TestDurableDeployOnAServerWithoutVisibilityLevels(t *testing.T) {
	for _, tc := range []struct {
		name       string
		args       []string
		wantErr    string
		wantStderr string
	}{
		{
			name: "flag ignored",
			args: []string{"deploy", "-f", "order-pipeline", "--visibility", "authenticated"},
			wantErr: "the server does not support visibility levels yet: 'order-pipeline' was deployed, " +
				"but check who can start its executions with volcano cloud durable get order-pipeline",
		},
		{
			name:       "no flag",
			args:       []string{"deploy", "-f", "order-pipeline"},
			wantStderr: "the server does not support visibility levels yet, so it reported no visibility for order-pipeline",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setDurableCommandTestHome(t)
			t.Chdir(t.TempDir())
			writeDurableProjectFile(t, "volcano/functions/order-pipeline.js", `exports.handler = async () => ({});`)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case writeDurableRuntimesResponse(t, w, r):
					return
				case r.Method == http.MethodPost && r.URL.Path == "/projects/"+durableProjectID+"/durable-functions":
					payload := durableFunctionPayload("order-pipeline", "")
					delete(payload, "visibility")
					writeDurableCommandJSON(t, w, http.StatusCreated, payload)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			cmd := newCloudDurableCommand(server)
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.SetArgs(tc.args)
			err := cmd.Execute()
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.NotContains(t, stdout.String(), "Visibility:")
			assert.NotContains(t, stdout.String(), "New durable functions are private")
			assert.Contains(t, stderr.String(), tc.wantStderr)
		})
	}
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
			writeDurableCommandJSON(t, w, http.StatusCreated, durableFunctionPayload("order-pipeline", "private"))
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

// --file has to honour the manifest too. A function's kind is fixed when it is
// created, so creating a name the manifest declares standard as durable strands
// it: every later `functions deploy` and `config deploy` of that name fails.
func TestDurableDeployRefusesAFileTargetTheManifestDeclaresStandard(t *testing.T) {
	for _, target := range []string{"hello", "volcano/functions/hello.js"} {
		t.Run(target, func(t *testing.T) {
			setDurableCommandTestHome(t)
			t.Chdir(t.TempDir())
			writeDurableProjectFile(t, "volcano/functions/hello.js", `exports.handler = async () => ({});`)
			writeDurableProjectFile(t, "volcano-config.yaml", `version: 1
project:
  name: beta
functions:
  - name: hello
  - name: order-pipeline
    kind: durable
`)

			var deployed bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case writeDurableRuntimesResponse(t, w, r):
					return
				case r.Method == http.MethodPost:
					deployed = true
					writeDurableCommandJSON(t, w, http.StatusCreated, durableFunctionPayload("hello", "private"))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			_, err := executeDurableCommand(t, newCloudDurableCommand(server), "deploy", "-f", target)
			require.Error(t, err)
			assert.Contains(t, err.Error(), `"hello" is declared a standard function in volcano-config.yaml`)
			assert.Contains(t, err.Error(), "volcano cloud functions deploy -f hello")
			assert.False(t, deployed, "nothing should be uploaded once the kind is refused")
		})
	}
}

// A name the manifest does not mention is what --file exists for, so the guard
// has to key on an explicit standard declaration rather than on absence.
func TestDurableDeployAcceptsAFileTargetTheManifestDoesNotDeclare(t *testing.T) {
	setDurableCommandTestHome(t)
	t.Chdir(t.TempDir())
	writeDurableProjectFile(t, "volcano/functions/order-pipeline.js", `exports.handler = async () => ({});`)
	writeDurableProjectFile(t, "volcano-config.yaml", `version: 1
project:
  name: beta
functions:
  - name: hello
`)

	var deployed []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case writeDurableRuntimesResponse(t, w, r):
			return
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+durableProjectID+"/durable-functions":
			require.NoError(t, r.ParseMultipartForm(4*1024*1024))
			deployed = append(deployed, r.FormValue("name"))
			writeDurableCommandJSON(t, w, http.StatusCreated, durableFunctionPayload("order-pipeline", "private"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	_, err := executeDurableCommand(t, newCloudDurableCommand(server), "deploy", "-f", "order-pipeline")
	require.NoError(t, err)
	assert.Equal(t, []string{"order-pipeline"}, deployed)
}

// The manifest is the only place a durable function's variable scope can be
// declared, and the API reads a declaration only from the deploy that sends
// one. Left out, a durable function the manifest scopes is created with every
// project variable in its environment — the widest possible reading of an entry
// that asked for the narrowest.
func TestDurableDeploySendsTheManifestVariableScope(t *testing.T) {
	setDurableCommandTestHome(t)
	t.Chdir(t.TempDir())
	writeDurableProjectFile(t, "volcano/functions/order-pipeline.js", `exports.handler = async () => ({});`)
	writeDurableProjectFile(t, "volcano-config.yaml", `version: 1
project:
  name: beta
functions:
  - name: order-pipeline
    kind: durable
    variable_scope: scoped
    variables:
      - STRIPE_SECRET
`)

	var fields map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case writeDurableRuntimesResponse(t, w, r):
			return
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+durableProjectID+"/durable-functions":
			require.NoError(t, r.ParseMultipartForm(4*1024*1024))
			fields = map[string]string{
				"variable_scope": r.FormValue("variable_scope"),
				"variables":      r.FormValue("variables"),
			}
			writeDurableCommandJSON(t, w, http.StatusCreated, durableFunctionPayload("order-pipeline", "private"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	_, err := executeDurableCommand(t, newCloudDurableCommand(server), "deploy", "--all")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"variable_scope": "scoped",
		"variables":      `["STRIPE_SECRET"]`,
	}, fields)
}

// And a manifest that declares no scope sends neither field, which is what
// leaves an existing function's scope alone on redeploy.
func TestDurableDeployWithoutADeclarationSendsNoScope(t *testing.T) {
	setDurableCommandTestHome(t)
	t.Chdir(t.TempDir())
	writeDurableProjectFile(t, "volcano/functions/order-pipeline.js", `exports.handler = async () => ({});`)
	writeDurableProjectFile(t, "volcano-config.yaml", `version: 1
project:
  name: beta
functions:
  - name: order-pipeline
    kind: durable
`)

	var form map[string][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case writeDurableRuntimesResponse(t, w, r):
			return
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+durableProjectID+"/durable-functions":
			require.NoError(t, r.ParseMultipartForm(4*1024*1024))
			form = r.MultipartForm.Value
			writeDurableCommandJSON(t, w, http.StatusCreated, durableFunctionPayload("order-pipeline", "private"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	_, err := executeDurableCommand(t, newCloudDurableCommand(server), "deploy", "--all")
	require.NoError(t, err)
	assert.NotContains(t, form, "variable_scope")
	assert.NotContains(t, form, "variables")
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

// Local development runs durable executions on its own engine, so the local
// tree gets the same command the cloud tree does rather than a stub that
// refuses. This is what stops the two drifting: a subcommand added to one and
// not the other would show up here as a tree that is missing it.
func TestLocalTreeOffersTheSameDurableCommands(t *testing.T) {
	t.Parallel()

	cloud := New(cliruntime.Deps{CommandPathPrefix: "volcano cloud"})
	local := NewLocal(cliruntime.Deps{})

	assert.False(t, local.Hidden, "durable is a local capability now, so local help lists it")
	assert.ElementsMatch(t, subcommandNames(cloud), subcommandNames(local),
		"the local tree offers the same durable subcommands as the cloud tree")
}

func subcommandNames(cmd *cobra.Command) []string {
	names := make([]string, 0, len(cmd.Commands()))
	for _, sub := range cmd.Commands() {
		names = append(names, sub.Name())
	}
	return names
}
