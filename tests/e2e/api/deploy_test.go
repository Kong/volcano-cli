package api

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// A deploy waits for every build it starts, and the frontend's is the longest.
const apiE2EDeployTimeout = apiE2EFrontendDeploymentTimeout + 10*time.Minute

var apiE2EScriptPattern = regexp.MustCompile(`src="(/_next/static/[^"]+\.js)"`)

// TestAPIE2ECloudDeploy follows the cloud steps `volcano init` prints for the
// notes example, then grows the project and deploys it again. Every step of
// the deploy is a real build, so one project covers them all. The refusals,
// which need no provisioning, are covered by the hosting repo's
// `TestCLIE2ESmokeDeploy`.
func TestAPIE2ECloudDeploy(t *testing.T) {
	env := setupAPIE2E(t, "cloud-deploy")

	env.loginAndUse(t)
	env.runCLI(t, "init", "nextjs", "--example", "notes").
		requireSuccess(t, "Run: volcano cloud deploy --env-file volcano/cloud.env --with-migrations -d <database>")
	// The starter's volcano/volcano.env points at the local server, so the bare
	// command must not reach the cloud project with it.
	env.runCloudCLI(t, "deploy").requireFailure(t,
		"volcano/volcano.env holds the local development server's values",
		"--env-file volcano/cloud.env")

	database := "cli_e2e_" + apiE2ESuffix(t)
	env.runCloudCLI(t, "databases", "create", database, "--region", apiE2EDefaultRegion, "--pg-version", apiE2EDefaultPGVersion).requireSuccess(t, database)
	t.Cleanup(func() {
		_ = env.runCloudCLI(t, "databases", "delete", database, "--yes")
	})
	env.waitForDatabaseActive(t, database, 30*time.Minute)

	anonKey := createAPIE2EAnonKey(t, env.apiURL, env.token, env.projectID, "cli-e2e-deploy", "auth.signup", "auth.signin")
	writeAPIE2EFile(t, filepath.Join(env.projectDir, "volcano", "cloud.env"), fmt.Sprintf(
		"VOLCANO_API_URL=%[1]s\nVOLCANO_ANON_KEY=%[2]s\nVOLCANO_DATABASE=%[3]s\n"+
			"NEXT_PUBLIC_VOLCANO_API_URL=%[1]s\nNEXT_PUBLIC_VOLCANO_ANON_KEY=%[2]s\nNEXT_PUBLIC_VOLCANO_DATABASE=%[3]s\n",
		env.apiURL, anonKey, database))

	first := env.runCloudCLIWithin(t, apiE2EDeployTimeout,
		"deploy", "--env-file", filepath.Join("volcano", "cloud.env"), "--with-migrations", "-d", database)
	first.requireSuccess(t,
		fmt.Sprintf("Deploying to cloud project %q", env.project),
		"1. variables   volcano/cloud.env",
		"2. migrations  volcano/migrations -> database "+database,
		"4. durable     skipped: volcano/volcano-config.yaml declares no durable functions",
		"5. frontends   web (.volcano/source-layout.json)",
		"Applying 001_create_notes.sql... ok",
		"Applying 015_notes_updated_at_trigger_create.sql... ok",
		"✓ notes-summary",
		"✓ web",
		"Deployed in")
	t.Cleanup(func() {
		_ = env.runCloudCLI(t, "frontends", "delete", "web", "--yes")
		_ = env.runCloudCLI(t, "functions", "delete", "notes-summary", "--yes")
	})

	siteURL := apiE2EDeployedFrontendURL(t, first.output, "web")
	waitForAPIE2EFrontendContent(t, siteURL, "Volcano Notes Demo")
	// The browser client works only if the build saw the variables the
	// frontend selects, which next build inlines into the page's scripts.
	requireAPIE2EFrontendScriptsContain(t, siteURL, anonKey)
	env.runCloudCLI(t, "functions", "get", "notes-summary").requireSuccess(t, "Visibility: authenticated")
	env.runCloudCLI(t, "variables", "list").requireNotContains(t, "VOLCANO_SERVICE_KEY")
	// A signed-in user's note goes in and comes back only if the migrations
	// created the table, its user_id default, and its row-level policies.
	userToken := signUpAPIE2EEndUser(t, env.apiURL, anonKey)
	queryURL := env.apiURL + "/databases/" + database + "/query/"
	apiE2EJSONRequest(t, http.MethodPost, queryURL+"insert", userToken,
		map[string]any{"table": "notes", "values": map[string]any{"title": "deployed note"}}, http.StatusOK)
	notes := apiE2EJSONRequest(t, http.MethodPost, queryURL+"select", userToken,
		map[string]any{"table": "notes", "select": []string{"title"}}, http.StatusOK)
	if fmt.Sprint(notes["data"]) != "[map[title:deployed note]]" {
		t.Fatalf("the signed-in user's notes are %v, want the one note it inserted", notes["data"])
	}

	// The project grows a durable function, a routed HTTP function, an auth
	// setting, and a page change. Without --with-migrations the migrations are
	// left alone.
	writeAPIE2EFile(t, filepath.Join(env.projectDir, "volcano", "functions", "order-pipeline.js"), apiE2EDurableSource)
	writeAPIE2ERoutedFunction(t, env.projectDir)
	writeAPIE2EFile(t, filepath.Join(env.projectDir, "volcano", "volcano-config.yaml"), `
version: 1
auth:
  password:
    min_length: 20
functions:
  - name: notes-summary
    visibility: authenticated
  - name: echo
    visibility: public
    invocation_mode: http
  - name: order-pipeline
    kind: durable
frontends:
  - name: web
    variable_scope: scoped
    variables:
      - NEXT_PUBLIC_VOLCANO_API_URL
      - NEXT_PUBLIC_VOLCANO_ANON_KEY
      - NEXT_PUBLIC_VOLCANO_DATABASE
    function_routes:
      - function: echo
        path_prefix: /api/echo
        strip_prefix: true
`)
	writeAPIE2EFile(t, filepath.Join(env.projectDir, "web", "app", "deployed", "page.js"), `export default function Deployed() {
  return <main>Volcano CLI E2E second deploy</main>;
}
`)

	second := env.runCloudCLIWithin(t, apiE2EDeployTimeout, "deploy", "--env-file", filepath.Join("volcano", "cloud.env"))
	second.requireSuccess(t,
		"2. migrations  skipped: pass --with-migrations to apply volcano/migrations",
		"4. durable     order-pipeline",
		"✓ echo",
		"✓ order-pipeline",
		"Deployed in")
	second.requireNotContains(t, "Applying 001_create_notes.sql", "Deploy them with")
	t.Cleanup(func() {
		_ = env.runCloudCLI(t, "durable", "delete", "order-pipeline", "--yes")
		_ = env.runCloudCLI(t, "functions", "delete", "echo", "--yes")
	})

	waitForAPIE2EFrontendContent(t, frontendPathURL(siteURL, "/deployed"), "Volcano CLI E2E second deploy")
	waitForAPIE2ERoutedPath(t, frontendPathURL(siteURL, "/api/echo/ping"), "/ping")
	env.runCloudCLI(t, "functions", "list").requireNotContains(t, "order-pipeline")
	env.runCloudCLI(t, "functions", "get", "notes-summary").requireSuccess(t, "Visibility: authenticated")

	env.runCloudCLI(t, "durable", "get", "order-pipeline").requireSuccess(t, "Status: active")
	started := env.runCloudCLI(t, "durable", "start", "order-pipeline", "--input", `{"order_id":5150}`)
	started.requireSuccess(t, "started")
	env.waitForCloudCLIContains(t, apiE2EDurableExecutionTimeout, "Status: succeeded",
		"durable", "executions", "get", "order-pipeline", apiE2EDurableExecutionID(t, started.output)).
		requireSuccess(t, "5150")

	pulled := filepath.Join(t.TempDir(), "pulled-config.yaml")
	env.runCloudCLI(t, "config", "pull", "-f", pulled).requireSuccess(t, "Configuration written to")
	pulledConfig, err := os.ReadFile(pulled)
	if err != nil {
		t.Fatalf("read pulled config: %v", err)
	}
	if !strings.Contains(string(pulledConfig), "min_length: 20") {
		t.Fatalf("pulled config does not carry the deployed password setting:\n%s", pulledConfig)
	}
}

// apiE2EDeployedFrontendURL reads a frontend's URL from the list a deploy
// prints once its deployments finish.
func apiE2EDeployedFrontendURL(t *testing.T, output, name string) string {
	t.Helper()
	_, list, found := strings.Cut(output, "\nFrontends:\n")
	if found {
		for line := range strings.SplitSeq(list, "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == name {
				return fields[1]
			}
		}
	}
	t.Fatalf("deploy printed no URL for frontend %s:\n%s", name, redactCredentials(output))
	return ""
}

func requireAPIE2EFrontendScriptsContain(t *testing.T, siteURL, want string) {
	t.Helper()
	client := &http.Client{Timeout: 15 * time.Second}
	page := fetchAPIE2EPage(t, client, siteURL)
	base, err := url.Parse(siteURL)
	if err != nil {
		t.Fatalf("frontend site URL %q: %v", siteURL, err)
	}
	scripts := apiE2EScriptPattern.FindAllStringSubmatch(page, -1)
	for _, match := range scripts {
		if strings.Contains(fetchAPIE2EPage(t, client, base.ResolveReference(&url.URL{Path: match[1]}).String()), want) {
			return
		}
	}
	t.Fatalf("none of the %d scripts on %s contain the expected value", len(scripts), siteURL)
}

func fetchAPIE2EPage(t *testing.T, client *http.Client, pageURL string) string {
	t.Helper()
	resp, err := client.Get(pageURL)
	if err != nil {
		t.Fatalf("GET %s: %v", pageURL, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", pageURL, err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s answered %d: %.500s", pageURL, resp.StatusCode, body)
	}
	return string(body)
}
