package deploy

import (
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	cliconfig "github.com/Kong/volcano-cli/internal/config"
)

// grantRouteTable is Hosting's delegated grant route table, copied from
// DefaultGrantRoutes() in internal/middleware/delegated_grant_routes.go at
// Kong/volcano-hosting 1749144404 (C3, #3313). A pg- grant is refused on any
// other route with 403 grant_route_not_allowed. Update it with that file.
var grantRouteTable = []string{
	"GET /projects/{id}", "GET /projects/{id}/capabilities", "GET /projects/{id}/config",
	"GET /projects/{id}/deployments", "POST /projects/{id}/logs/search", "POST /projects/{id}/logs/stream",
	"GET /projects/{id}/functions", "GET /projects/{id}/functions/{functionId}",
	"GET /projects/{id}/functions/{functionId}/deployments", "GET /projects/{id}/functions/{functionId}/schedulers",
	"GET /projects/{id}/functions/{functionId}/schedulers/{schedulerId}",
	"GET /projects/{id}/durable-functions", "GET /projects/{id}/durable-functions/{functionId}",
	"GET /projects/{id}/frontends", "GET /projects/{id}/frontends/{frontendId}",
	"GET /projects/{id}/frontends/{frontendId}/deployments", "GET /projects/{id}/frontends/{frontendId}/serving",
	"POST /projects/{id}/functions", "POST /projects/{id}/functions/batch", "PATCH /projects/{id}/functions/{functionId}",
	"POST /projects/{id}/functions/{functionId}/schedulers",
	"PATCH /projects/{id}/functions/{functionId}/schedulers/{schedulerId}",
	"DELETE /projects/{id}/functions/{functionId}/schedulers/{schedulerId}",
	"POST /projects/{id}/durable-functions",
	"PUT /projects/{id}/config", "POST /projects/{id}/variables", "PUT /projects/{id}/variables/{name}",
	"DELETE /projects/{id}/variables/{name}",
	"GET /projects/{id}/databases/{databaseName}/migrations", "GET /projects/{id}/databases/{databaseName}/migration-runs",
	"GET /projects/{id}/databases/{databaseName}/migration-runs/{runId}",
	"POST /projects/{id}/databases/{databaseName}/migration-runs",
	"POST /projects/{id}/frontends", "POST /projects/{id}/frontends/{frontendId}/deployments",
	"POST /projects/{id}/frontends/{frontendId}/redeploy",
}

// grantMayCall reports whether a grant may call method and path. The public
// runtime catalog runs no auth middleware, so the grant branch never sees it.
func grantMayCall(method, path string) bool {
	if method == http.MethodGet && path == "/functions/runtimes" {
		return true
	}
	got := strings.Split(path, "/")
	for _, entry := range grantRouteTable {
		entryMethod, template, _ := strings.Cut(entry, " ")
		want := strings.Split(template, "/")
		if entryMethod != method || len(want) != len(got) {
			continue
		}
		match := true
		for i := range want {
			if got[i] == "" && want[i] != "" || !strings.HasPrefix(want[i], "{") && want[i] != got[i] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// grantGate refuses what Hosting refuses a grant, records it, and passes the
// rest to next.
type grantGate struct {
	mu      sync.Mutex
	refused []string
	auth    []string
	// also lets a grant call these "METHOD path" requests too.
	also []string
	next http.HandlerFunc
}

func (g *grantGate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	g.auth = append(g.auth, r.Header.Get("Authorization"))
	if !grantMayCall(r.Method, r.URL.Path) && !slices.Contains(g.also, r.Method+" "+r.URL.Path) {
		g.refused = append(g.refused, r.Method+" "+r.URL.Path)
		g.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"delegated grants cannot be used on this endpoint","code":"grant_route_not_allowed"}`))
		return
	}
	g.mu.Unlock()
	g.next(w, r)
}

// sourceExportRead is the read `volcano cloud deploy` makes before deploying
// code, to refuse a project that deploys from Git. It is not on the grant
// table yet, so a whole-project deploy fails under a grant until Hosting adds
// it (as project.read). Per-resource deploys don't make it.
const sourceExportRead = "GET /projects/" + deployProjectID + "/source-export"

func TestDeployUnderAGrantIsRefusedTheSourceModeRead(t *testing.T) {
	setDeployCommandTestHome(t)
	t.Setenv("VOLCANO_TOKEN", cliconfig.DelegatedGrantPrefix+"grant-secret")
	writeFullProject(t)
	fake := newFakeVolcano(t)
	gate := &grantGate{next: fake.serve}
	server := fake.start()
	server.Config.Handler = gate

	out, err := executeDeployCommand(t, New(fake.cloudDeps(server)))
	require.Error(t, err)
	assert.Equal(t, []string{sourceExportRead}, gate.refused)
	assert.Empty(t, fake.recorded(), "refused before any change")
	assert.NotContains(t, out, "grant-secret")
}

// With the source mode read allowed, every other request a whole-project
// deploy makes, migrations included, is on the grant table, and carries the
// grant.
func TestDeployUnderAGrantCallsOnlyGrantRoutesOtherwise(t *testing.T) {
	setDeployCommandTestHome(t)
	t.Setenv("VOLCANO_TOKEN", cliconfig.DelegatedGrantPrefix+"grant-secret")
	writeFullProject(t)
	fake := newFakeVolcano(t)
	gate := &grantGate{next: fake.serve, also: []string{sourceExportRead}}
	server := fake.start()
	server.Config.Handler = gate

	out, err := executeDeployCommand(t, New(fake.cloudDeps(server)))
	require.NoError(t, err, out)
	assert.Empty(t, gate.refused)
	for _, auth := range gate.auth {
		assert.Equal(t, "Bearer "+cliconfig.DelegatedGrantPrefix+"grant-secret", auth)
	}
	assert.NotContains(t, out, "grant-secret")
}

// `deploy --with-migrations` under a grant applies through a migration run and
// never reads the database, whose answer holds the connection string.
func TestDeployWithMigrationsUnderAGrantUsesAMigrationRun(t *testing.T) {
	setDeployCommandTestHome(t)
	t.Setenv("VOLCANO_TOKEN", cliconfig.DelegatedGrantPrefix+"grant-secret")
	writeDeployProjectFile(t, "volcano/migrations/001_init.sql", "create table notes (id int);")
	writeDeployProjectFile(t, "volcano/functions/hello.js", `exports.handler = async () => ({});`)
	fake := newFakeVolcano(t)
	runs := "/projects/" + deployProjectID + "/databases/app/migration-runs"
	const runID = "44444444-4444-4444-8444-444444444444"
	var runCalls []string
	gate := &grantGate{also: []string{sourceExportRead}, next: func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, runs) {
			runCalls = append(runCalls, r.Method+" "+r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"id":"` + runID + `","status":"succeeded","phase":"apply","source":"api","warnings":[],` +
				`"database":{"id":"66666666-6666-4666-8666-666666666666","name":"app"},` +
				`"files":[{"name":"001_init.sql","outcome":"applied"}],` +
				`"created_at":"2026-11-11T00:00:00Z","updated_at":"2026-11-11T00:00:00Z"}`))
			return
		}
		fake.serve(w, r)
	}}
	server := fake.start()
	server.Config.Handler = gate

	out, err := executeDeployCommand(t, New(fake.cloudDeps(server)), "--with-migrations", "-d", "app")
	require.NoError(t, err, out)
	assert.Empty(t, gate.refused)
	assert.Equal(t, []string{"POST " + runs}, runCalls)
	assert.NotContains(t, fake.reads, "database app")
	assert.Contains(t, out, "001_init.sql: applied")
}

// A spent budget on any mutation keeps the API's error, so the CLI exits 6
// and the worker can ask for a new grant.
func TestDeployUnderAGrantKeepsTheBudgetRefusalOnEveryMutation(t *testing.T) {
	project := "/projects/" + deployProjectID
	for _, route := range []string{
		"POST " + project + "/variables",
		"POST " + project + "/functions/batch",
		"POST " + project + "/durable-functions",
		"POST " + project + "/frontends",
		"PUT " + project + "/config",
	} {
		t.Run(route, func(t *testing.T) {
			setDeployCommandTestHome(t)
			t.Setenv("VOLCANO_TOKEN", cliconfig.DelegatedGrantPrefix+"grant-secret")
			writeFullProject(t)
			fake := newFakeVolcano(t)
			var hit bool
			gate := &grantGate{also: []string{sourceExportRead}, next: func(w http.ResponseWriter, r *http.Request) {
				// The config step's dry run is a read in all but method.
				if r.Method+" "+r.URL.Path == route && r.URL.Query().Get("dry_run") != "true" {
					hit = true
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusTooManyRequests)
					_, _ = w.Write([]byte(`{"error":"the delegated grant has no mutations left; ask the app for a new grant","code":"grant_budget_exhausted"}`))
					return
				}
				fake.serve(w, r)
			}}
			server := fake.start()
			server.Config.Handler = gate

			out, err := executeDeployCommand(t, New(fake.cloudDeps(server)))
			require.Error(t, err, out)
			require.True(t, hit, "the deploy never reached %s", route)
			assert.True(t, api.GrantBudgetExhausted(err), "%v", err)
		})
	}
}
