package deploy

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	cliconfig "github.com/Kong/volcano-cli/internal/config"
)

// The kinds of grant route, as Hosting's GrantRouteKind. A read and an invoke
// spend no mutation; a mutation spends one.
const (
	grantRead     = "read"
	grantInvoke   = "invoke"
	grantMutation = "mutation"
)

// grantRouteTable is Hosting's delegated grant route table, "METHOD template"
// to kind, copied from DefaultGrantRoutes() in
// internal/middleware/delegated_grant_routes.go at Kong/volcano-hosting
// 2f42b4c314 (tkkhq/grant-acceptance-invoke, C6, #3329), the top of the grant
// chain. A pg- grant is refused on any other route with 403
// grant_route_not_allowed. Update it with that file.
var grantRouteTable = map[string]string{
	"GET /projects/{id}":                                                 grantRead,
	"GET /projects/{id}/capabilities":                                    grantRead,
	"GET /projects/{id}/config":                                          grantRead,
	"GET /projects/{id}/deployments":                                     grantRead,
	"POST /projects/{id}/logs/search":                                    grantRead,
	"POST /projects/{id}/logs/stream":                                    grantRead,
	"GET /projects/{id}/functions":                                       grantRead,
	"GET /projects/{id}/functions/{functionId}":                          grantRead,
	"GET /projects/{id}/functions/{functionId}/deployments":              grantRead,
	"GET /projects/{id}/functions/{functionId}/schedulers":               grantRead,
	"GET /projects/{id}/functions/{functionId}/schedulers/{schedulerId}": grantRead,
	"GET /projects/{id}/durable-functions":                               grantRead,
	"GET /projects/{id}/durable-functions/{functionId}":                  grantRead,
	"GET /projects/{id}/frontends":                                       grantRead,
	"GET /projects/{id}/frontends/{frontendId}":                          grantRead,
	"GET /projects/{id}/frontends/{frontendId}/deployments":              grantRead,
	"GET /projects/{id}/frontends/{frontendId}/serving":                  grantRead,
	"GET /projects/{id}/source-export":                                   grantRead,

	"POST /projects/{id}/functions":                                         grantMutation,
	"POST /projects/{id}/functions/batch":                                   grantMutation,
	"PATCH /projects/{id}/functions/{functionId}":                           grantMutation,
	"POST /projects/{id}/functions/{functionId}/schedulers":                 grantMutation,
	"PATCH /projects/{id}/functions/{functionId}/schedulers/{schedulerId}":  grantMutation,
	"DELETE /projects/{id}/functions/{functionId}/schedulers/{schedulerId}": grantMutation,
	"POST /projects/{id}/durable-functions":                                 grantMutation,

	"POST /projects/{id}/durable-functions/{functionId}/executions":              grantInvoke,
	"GET /projects/{id}/durable-functions/{functionId}/executions/{executionId}": grantRead,

	"PUT /projects/{id}/config":              grantMutation,
	"POST /projects/{id}/variables":          grantMutation,
	"PUT /projects/{id}/variables/{name}":    grantMutation,
	"DELETE /projects/{id}/variables/{name}": grantMutation,

	"GET /projects/{id}/databases/{databaseName}/migrations":             grantRead,
	"GET /projects/{id}/databases/{databaseName}/migration-runs":         grantRead,
	"GET /projects/{id}/databases/{databaseName}/migration-runs/{runId}": grantRead,
	"POST /projects/{id}/databases/{databaseName}/migration-runs":        grantMutation,
	"POST /projects/{id}/databases/{databaseName}/migrations/baseline":   grantMutation,

	"POST /projects/{id}/frontends":                          grantMutation,
	"POST /projects/{id}/frontends/{frontendId}/deployments": grantMutation,
	"POST /projects/{id}/frontends/{frontendId}/redeploy":    grantMutation,
}

// grantDryRunReads are the mutations whose dry_run=true request is a read, as
// Hosting's GrantRoute.DryRunReads. Only config apply has one.
var grantDryRunReads = []string{"PUT /projects/{id}/config"}

// grantRouteKind returns the kind of r on the grant table, and false when the
// table has no route for it. The public runtime catalog runs no auth
// middleware, so the grant branch never sees it.
func grantRouteKind(r *http.Request) (string, bool) {
	if r.Method == http.MethodGet && r.URL.Path == "/functions/runtimes" {
		return grantRead, true
	}
	got := strings.Split(r.URL.Path, "/")
	for entry, kind := range grantRouteTable {
		method, template, _ := strings.Cut(entry, " ")
		want := strings.Split(template, "/")
		if method != r.Method || len(want) != len(got) {
			continue
		}
		match := true
		for i := range want {
			if got[i] == "" && want[i] != "" || !strings.HasPrefix(want[i], "{") && want[i] != got[i] {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		if dryRun, err := strconv.ParseBool(r.URL.Query().Get("dry_run")); err == nil && dryRun &&
			slices.Contains(grantDryRunReads, entry) {
			return grantRead, true
		}
		return kind, true
	}
	return "", false
}

// grantGate refuses what Hosting refuses a grant, records it, and passes the
// rest to next.
type grantGate struct {
	mu      sync.Mutex
	refused []string
	auth    []string
	next    http.HandlerFunc
}

func (g *grantGate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	g.auth = append(g.auth, r.Header.Get("Authorization"))
	if _, ok := grantRouteKind(r); !ok {
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

// Every request a whole-project deploy makes, the source mode read and
// migrations included, is on the grant table, and carries the grant.
func TestDeployUnderAGrantCallsOnlyGrantRoutes(t *testing.T) {
	setDeployCommandTestHome(t)
	t.Setenv("VOLCANO_TOKEN", cliconfig.DelegatedGrantPrefix+"grant-secret")
	writeFullProject(t)
	fake := newFakeVolcano(t)
	gate := &grantGate{next: fake.serve}
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
	gate := &grantGate{next: func(w http.ResponseWriter, r *http.Request) {
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
			gate := &grantGate{next: func(w http.ResponseWriter, r *http.Request) {
				// The config step's dry run is a read on the grant table, so it
				// spends nothing.
				if kind, _ := grantRouteKind(r); r.Method+" "+r.URL.Path == route && kind == grantMutation {
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
