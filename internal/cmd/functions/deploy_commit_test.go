package functions

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/deploycommit/deploycommittest"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// shopFiles is a project with a single-file function, a directory function
// with its own dependency manifest, a root dependency manifest, and a
// manifest that declares both.
var shopFiles = map[string]string{
	"package.json":                          `{"name":"shop","private":true}`,
	"volcano/functions/hello.js":            `exports.handler = async () => ({ statusCode: 200 });`,
	"volcano/functions/orders/index.js":     "exports.handler = async () => ({ statusCode: 200 });\n",
	"volcano/functions/orders/package.json": `{"name":"orders","private":true}`,
	"volcano/functions/orders/helper.js":    "exports.help = 1;\n",
	"volcano-config.yaml":                   "version: 1\nfunctions:\n  - name: hello\n  - name: orders\n",
	".gitignore":                            ".env\n",
}

// writeShop writes the shop project in a new working directory, with a
// signed-in config.
func writeShop(t *testing.T, files map[string]string) {
	t.Helper()
	setFunctionCommandTestHome(t)
	saveFunctionCommandTestConfig(t)
	t.Chdir(t.TempDir())
	for path, content := range files {
		require.NoError(t, writeProjectFile(path, content))
	}
}

// shop writes the shop project and commits it to a repository whose origin
// is acme/shop, returning the commit a clean function reports.
func shop(t *testing.T) *api.FunctionCommit {
	t.Helper()
	writeShop(t, shopFiles)
	head := deploycommittest.Repository(t, ".", "https://github.com/acme/shop.git")
	return &api.FunctionCommit{Repository: "acme/shop", SHA: head}
}

// deployCommits runs a functions command and returns the commit each
// function upload reported, by name: nil for an upload that reported none.
func deployCommits(t *testing.T, args ...string) map[string]*api.FunctionCommit {
	t.Helper()
	return deployCommitsWith(t, New, cliruntime.Deps{}, args...)
}

func deployCommitsWith(
	t *testing.T, newCommand func(cliruntime.Deps) *cobra.Command, deps cliruntime.Deps, args ...string,
) map[string]*api.FunctionCommit {
	t.Helper()
	commits := map[string]*api.FunctionCommit{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case writeFunctionRuntimesCommandResponse(w, r):
			return
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+functionProjectID+"/functions":
			require.NoError(t, r.ParseMultipartForm(4*1024*1024))
			name := r.FormValue("name")
			var commit *api.FunctionCommit
			if values, ok := r.MultipartForm.Value["commit"]; ok {
				require.Len(t, values, 1)
				require.NoError(t, json.Unmarshal([]byte(values[0]), &commit))
			}
			commits[name] = commit
			writeFunctionCommandJSON(t, w, http.StatusCreated, functionCommandPayload(functionID, name))
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+functionProjectID+"/functions/batch":
			require.NoError(t, r.ParseMultipartForm(32*1024*1024))
			assert.NotContains(t, r.MultipartForm.Value, "commit", "a batch reports each entry's commit in its manifest entry")
			var manifest []struct {
				Name   string          `json:"name"`
				Commit json.RawMessage `json:"commit"`
			}
			require.NoError(t, json.Unmarshal([]byte(r.FormValue("functions")), &manifest))
			data := make([]any, 0, len(manifest))
			for i, entry := range manifest {
				var commit *api.FunctionCommit
				if entry.Commit != nil {
					// An object, not the JSON-encoded string the single upload takes.
					require.NoError(t, json.Unmarshal(entry.Commit, &commit))
				}
				commits[entry.Name] = commit
				data = append(data, functionCommandPayload(fmt.Sprintf("88888888-8888-4888-8888-%012d", i), entry.Name))
			}
			writeFunctionCommandJSON(t, w, http.StatusAccepted, map[string]any{
				"batch_id": "77777777-7777-4777-8777-000000000003",
				"data":     data,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	deps.HTTPClient, deps.APIBaseURL = server.Client(), server.URL
	_, err := executeFunctionsCommand(t, newCommand(deps), args...)
	require.NoError(t, err)
	return commits
}

func write(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, writeProjectFile(path, content))
}

// A function whose files are all as HEAD holds them reports HEAD.
func TestFunctionsDeploySingleSendsTheCommit(t *testing.T) {
	clean := shop(t)

	commits := deployCommits(t, "deploy", "-f", "volcano/functions/orders/index.js")
	assert.Equal(t, map[string]*api.FunctionCommit{"orders": clean}, commits)
}

func TestFunctionsDeployBatchSendsTheCommit(t *testing.T) {
	clean := shop(t)

	commits := deployCommits(t, "deploy", "--all")
	assert.Equal(t, map[string]*api.FunctionCommit{"hello": clean, "orders": clean}, commits)
}

// Local mode never reports a commit, so the local server gets the upload it
// always has, whatever it does with the field.
func TestFunctionsDeployInLocalModeSendsNoCommit(t *testing.T) {
	shop(t)

	commits := deployCommitsWith(t, NewLocal, cliruntime.Deps{LocalMode: true}, "deploy", "--all")
	assert.Equal(t, map[string]*api.FunctionCommit{"hello": nil, "orders": nil}, commits)
}

// A batch reports a commit for each clean function and none for a dirty one.
func TestFunctionsDeployBatchSendsTheCommitOnlyForCleanFunctions(t *testing.T) {
	clean := shop(t)
	write(t, "volcano/functions/hello.js", `exports.handler = async () => ({ statusCode: 201 });`)

	commits := deployCommits(t, "deploy", "--all")
	assert.Equal(t, map[string]*api.FunctionCommit{"hello": nil, "orders": clean}, commits)
}

// A change to any file the function's upload is made from, or would be made
// from, reports no commit.
func TestFunctionsDeploySendsNoCommitForADirtyFunction(t *testing.T) {
	cases := map[string]func(t *testing.T){
		"edited": func(t *testing.T) {
			t.Helper()
			write(t, "volcano/functions/orders/index.js", "exports.handler = 2;\n")
		},
		"edit staged, then undone on disk": func(t *testing.T) {
			t.Helper()
			write(t, "volcano/functions/orders/index.js", "exports.handler = 2;\n")
			deploycommittest.Git(t, ".", "add", ".")
			write(t, "volcano/functions/orders/index.js", shopFiles["volcano/functions/orders/index.js"])
		},
		"deleted": func(t *testing.T) {
			t.Helper()
			require.NoError(t, os.Remove("volcano/functions/orders/package.json"))
		},
		"untracked": func(t *testing.T) {
			t.Helper()
			write(t, "volcano/functions/orders/extra.js", "exports.extra = 1;\n")
		},
		"added with intent to add": func(t *testing.T) {
			t.Helper()
			write(t, "volcano/functions/orders/extra.js", "exports.extra = 1;\n")
			deploycommittest.Git(t, ".", "add", "--intent-to-add", "volcano/functions/orders/extra.js")
		},
		"dependency manifest above it added": func(t *testing.T) {
			t.Helper()
			write(t, "volcano/functions/package-lock.json", `{"lockfileVersion":3}`)
		},
		"shared library added": func(t *testing.T) {
			t.Helper()
			write(t, "volcano/functions/_shared.js", "exports.shared = 1;\n")
		},
		"manifest edited": func(t *testing.T) {
			t.Helper()
			write(t, "volcano-config.yaml", "version: 1\nfunctions:\n  - name: orders\n")
		},
		"manifest replaced by a symlink to another committed file": func(t *testing.T) {
			t.Helper()
			write(t, "deploy/other.yaml", "version: 1\nfunctions:\n  - name: orders\n")
			deploycommittest.Git(t, ".", "add", "deploy/other.yaml")
			deploycommittest.Git(t, ".", "commit", "--quiet", "-m", "other")
			replaceWithSymlink(t, "volcano-config.yaml", filepath.Join("deploy", "other.yaml"))
		},
		"replaced by a symlink": func(t *testing.T) {
			t.Helper()
			replaceWithSymlink(t, "volcano/functions/orders/helper.js", "index.js")
		},
		"replaced by a directory": func(t *testing.T) {
			t.Helper()
			require.NoError(t, os.Remove("volcano/functions/orders/helper.js"))
			require.NoError(t, os.Mkdir("volcano/functions/orders/helper.js", 0o755))
		},
		".gitignore leaves a committed file out": func(t *testing.T) {
			t.Helper()
			write(t, ".gitignore", ".env\nhelper.js\n")
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			shop(t)
			change(t)

			commits := deployCommits(t, "deploy", "-f", "orders")
			require.Contains(t, commits, "orders")
			assert.Nil(t, commits["orders"])
		})
	}
}

// A staged change to a file the upload leaves out, or to a dependency
// manifest another one takes the place of, leaves the function clean.
func TestFunctionsDeploySendsTheCommitPastChangesItDoesNotUpload(t *testing.T) {
	files := maps.Clone(shopFiles)
	files["volcano/functions/orders/debug.log"] = "log\n"
	writeShop(t, files)
	head := deploycommittest.Repository(t, ".", "https://github.com/acme/shop.git")
	write(t, "volcano/functions/orders/debug.log", "more log\n")
	write(t, "package.json", `{"name":"shop","private":false}`)
	deploycommittest.Git(t, ".", "add", ".")

	commits := deployCommits(t, "deploy", "--all")
	assert.Equal(t, map[string]*api.FunctionCommit{
		// hello's upload holds the root package.json; orders' holds its own.
		"hello":  nil,
		"orders": {Repository: "acme/shop", SHA: head},
	}, commits)
}

// Of the files that could take an archive path, only the one packaging puts
// there counts, and the one that comes first counts even when it is gone.
func TestFunctionsDeployFollowsArchivePrecedence(t *testing.T) {
	files := maps.Clone(shopFiles)
	files["volcano/_util.js"] = "exports.util = 'elsewhere';\n"
	files["volcano/functions/_util.js"] = "exports.util = 'shared';\n"
	files["volcano/functions/orders/_util.js"] = "exports.util = 'orders';\n"
	writeShop(t, files)
	head := deploycommittest.Repository(t, ".", "https://github.com/acme/shop.git")
	clean := &api.FunctionCommit{Repository: "acme/shop", SHA: head}

	commits := deployCommits(t, "deploy", "--all")
	assert.Equal(t, map[string]*api.FunctionCommit{"hello": clean, "orders": clean}, commits)

	// hello's _util.js now comes from volcano/_util.js; orders keeps its own.
	require.NoError(t, os.Remove("volcano/functions/_util.js"))
	commits = deployCommits(t, "deploy", "--all")
	assert.Equal(t, map[string]*api.FunctionCommit{"hello": nil, "orders": clean}, commits)
}

func replaceWithSymlink(t *testing.T, path, target string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("making a symlink on Windows needs a privilege")
	}
	require.NoError(t, os.Remove(path))
	require.NoError(t, os.Symlink(target, path))
}

// The manifest a deploy reads its declarations from is an input even before
// it is committed.
func TestFunctionsDeploySendsNoCommitForAnUncommittedManifest(t *testing.T) {
	files := maps.Clone(shopFiles)
	delete(files, "volcano-config.yaml")
	writeShop(t, files)
	deploycommittest.Repository(t, ".", "https://github.com/acme/shop.git")
	write(t, "volcano-config.yaml", shopFiles["volcano-config.yaml"])

	commits := deployCommits(t, "deploy", "-f", "orders")
	assert.Equal(t, map[string]*api.FunctionCommit{"orders": nil}, commits)
}

// A single-file function's upload holds the dependency manifest above it;
// a directory function's holds its own in place of that one.
func TestFunctionsDeploySendsNoCommitForAFunctionWhoseDependencyManifestChanged(t *testing.T) {
	clean := shop(t)
	write(t, "package.json", `{"name":"shop","private":false}`)

	commits := deployCommits(t, "deploy", "--all")
	assert.Equal(t, map[string]*api.FunctionCommit{"hello": nil, "orders": clean}, commits)
}

// Changes to files the function's upload is not made from leave it clean.
func TestFunctionsDeploySendsTheCommitPastChangesElsewhere(t *testing.T) {
	clean := shop(t)
	// Another function.
	write(t, "volcano/functions/hello.js", `exports.handler = async () => ({ statusCode: 201 });`)
	// A file outside volcano that nothing uploads.
	write(t, "README.md", "# shop\n")
	// What packaging leaves out of every upload, and a file git ignores.
	write(t, "volcano/functions/orders/node_modules/left-pad/index.js", "module.exports = 1;\n")
	write(t, "volcano/functions/orders/.env", "SECRET=1\n")
	// A file in the function's directory that is not one of its own.
	write(t, "volcano/functions/orders/debug.log", "log\n")

	commits := deployCommits(t, "deploy", "-f", "orders")
	assert.Equal(t, map[string]*api.FunctionCommit{"orders": clean}, commits)
}

// A function built into a directory git ignores was made from sources the
// CLI cannot name, so it reports the commit only while the whole repository
// matches it.
func TestFunctionsDeploySendsTheCommitForIgnoredBuildOutputOnlyFromACleanRepository(t *testing.T) {
	cases := map[string]struct {
		change func(t *testing.T)
		clean  bool
	}{
		"nothing changed": {change: func(*testing.T) {}, clean: true},
		"other build output": {clean: true, change: func(t *testing.T) {
			t.Helper()
			write(t, "dist/functions/billing.js", "exports.handler = 3;\n")
		}},
		"source edited": {change: func(t *testing.T) {
			t.Helper()
			write(t, "src/billing.ts", "export const handler = 2;\n")
		}},
		"source deleted": {change: func(t *testing.T) {
			t.Helper()
			require.NoError(t, os.Remove("src/generated/manifest.ts"))
		}},
		"untracked source": {change: func(t *testing.T) {
			t.Helper()
			write(t, "src/new.ts", "export const x = 1;\n")
		}},
		"edit staged": {change: func(t *testing.T) {
			t.Helper()
			write(t, "src/billing.ts", "export const handler = 2;\n")
			deploycommittest.Git(t, ".", "add", "src/billing.ts")
		}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			writeShop(t, map[string]string{
				"package.json":              `{"name":"web","private":true}`,
				"src/billing.ts":            "export const handler = 1;\n",
				"src/generated/manifest.ts": "export const migrations = [];\n",
				".gitignore":                "/dist/\n/volcano/functions/\n",
			})
			head := deploycommittest.Repository(t, ".", "https://github.com/acme/web.git")
			write(t, "volcano/functions/billing.js", "exports.handler = 1;\n")
			write(t, "volcano/functions/package.json", `{"name":"volcano-functions","private":true}`)
			tc.change(t)

			commits := deployCommits(t, "deploy", "--all")
			want := map[string]*api.FunctionCommit{"billing": nil}
			if tc.clean {
				want["billing"] = &api.FunctionCommit{Repository: "acme/web", SHA: head}
			}
			assert.Equal(t, want, commits)
		})
	}
}

// Without a commit to name, the deploy goes ahead and reports none.
func TestFunctionsDeploySendsNoCommitWithoutACommit(t *testing.T) {
	cases := map[string]func(t *testing.T){
		"no repository": func(*testing.T) {},
		"no commit yet": func(t *testing.T) {
			t.Helper()
			deploycommittest.Init(t, ".", "https://github.com/acme/shop.git")
			deploycommittest.Git(t, ".", "add", ".")
		},
	}
	for name, setUp := range cases {
		t.Run(name, func(t *testing.T) {
			writeShop(t, shopFiles)
			t.Setenv("VOLCANO_DEPLOY_COMMIT", "")
			t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(mustGetwd(t)))
			setUp(t)

			commits := deployCommits(t, "deploy", "--all")
			assert.Equal(t, map[string]*api.FunctionCommit{"hello": nil, "orders": nil}, commits)
		})
	}
}

// Functions reached through a symlink to a directory outside the repository
// report no commit.
func TestFunctionsDeploySendsNoCommitForFunctionsOutsideTheRepository(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("making a symlink on Windows needs a privilege")
	}
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "hello.js"), []byte(shopFiles["volcano/functions/hello.js"]), 0o644))
	writeShop(t, map[string]string{"package.json": shopFiles["package.json"]})
	require.NoError(t, os.Mkdir("volcano", 0o755))
	require.NoError(t, os.Symlink(outside, filepath.Join("volcano", "functions")))
	deploycommittest.Repository(t, ".", "https://github.com/acme/shop.git")

	commits := deployCommits(t, "deploy", "-f", "hello")
	assert.Equal(t, map[string]*api.FunctionCommit{"hello": nil}, commits)
}

func mustGetwd(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	return dir
}
