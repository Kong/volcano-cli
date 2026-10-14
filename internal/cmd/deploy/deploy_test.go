package deploy

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/projectconfig"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// Variables come first so scoped resources can select them, code before
// config so the manifest finds every function and frontend it configures.
func TestDeployRunsEveryStepInOrder(t *testing.T) {
	setDeployCommandTestHome(t)
	writeFullProject(t)
	fake := newFakeVolcano(t)
	server := fake.start()

	out, err := executeDeployCommand(t, New(fake.cloudDeps(server)))
	require.NoError(t, err, out)

	assert.Equal(t, []string{
		"variable API_URL",
		"function hello",
		"durable order-pipeline",
		"frontend web",
		"config",
	}, fake.recorded())
	assert.Equal(t, map[string][]string{
		"name":           {"web"},
		"framework":      {"nextjs"},
		"app_root":       {"web"},
		"variable_scope": {"scoped"},
		"variables":      {"API_URL"},
	}, fake.frontends["web"])
	assert.Contains(t, out, `Deploying to cloud project "Beta"`)
	assert.Contains(t, out, "1. variables   volcano/volcano.env")
	assert.Contains(t, out, "2. migrations  skipped: pass --with-migrations to apply volcano/migrations")
	assert.Contains(t, out, "5. frontends   web (.volcano/source-layout.json)")
	assert.Contains(t, out, "[6/6] config")
	assert.Contains(t, out, "✓ order-pipeline  active")
	assert.NotContains(t, out, "Deploy them with")
	assert.Contains(t, out, "web  https://web.frontends.example.test")
	assert.Contains(t, out, "Deployed in")
}

func TestDeployAppliesManifestVariablesBeforeCode(t *testing.T) {
	setDeployCommandTestHome(t)
	writeDeployProjectFile(t, "volcano/functions/hello.js", `exports.handler = async () => ({});`)
	writeDeployProjectFile(t, "volcano/volcano-config.yaml", `version: 1
variables:
  - name: API_URL
    value: https://api.example.test
shared_variables: [API_URL]
functions:
  - name: hello
    variable_scope: scoped
    variables: [API_URL]
`)
	fake := newFakeVolcano(t)
	server := fake.start()

	out, err := executeDeployCommand(t, New(fake.cloudDeps(server)))
	require.NoError(t, err, out)

	assert.Equal(t, []string{"config", "function hello", "config"}, fake.recorded())
	require.Len(t, fake.configs, 2)
	assert.Equal(t, map[string]any{
		"version":          float64(1),
		"variables":        []any{map[string]any{"name": "API_URL", "value": "https://api.example.test"}},
		"shared_variables": []any{"API_URL"},
	}, fake.configs[0])
	assert.Contains(t, fake.configs[1], "functions")
	assert.Contains(t, out, "1. variables   variable sections of volcano/volcano-config.yaml")
}

// The manifests apply in the order given, and the first one decides which
// functions are durable.
func TestDeployAppliesEveryNamedManifestInOrder(t *testing.T) {
	setDeployCommandTestHome(t)
	writeDeployProjectFile(t, "volcano/functions/order-pipeline.js", `exports.handler = async () => ({});`)
	writeDeployProjectFile(t, "volcano/app.yaml", "version: 1\nfunctions:\n  - name: order-pipeline\n    kind: durable\n")
	writeDeployProjectFile(t, "volcano/auth.yaml", "version: 1\nauth:\n  email:\n    enabled: true\n")
	fake := newFakeVolcano(t)
	server := fake.start()

	out, err := executeDeployCommand(t, New(fake.cloudDeps(server)),
		"-c", "volcano/app.yaml", "-c", "volcano/auth.yaml", "--no-wait")
	require.NoError(t, err, out)

	assert.Equal(t, []string{"durable order-pipeline", "config", "config"}, fake.recorded())
	require.Len(t, fake.configs, 2)
	assert.Contains(t, fake.configs[0], "functions")
	assert.Contains(t, fake.configs[1], "auth")
	assert.Contains(t, out, "6. config      volcano/app.yaml, volcano/auth.yaml")
}

// A first manifest that declares the default manifest's durable functions
// itself decides their kinds as well as the default would.
func TestDeployAcceptsAFirstManifestThatDeclaresTheDurableFunctions(t *testing.T) {
	setDeployCommandTestHome(t)
	writeDeployProjectFile(t, "volcano/functions/order-pipeline.js", `exports.handler = async () => ({});`)
	writeDeployProjectFile(t, "volcano/volcano-config.yaml", "version: 1\nfunctions:\n  - name: order-pipeline\n    kind: durable\n")
	writeDeployProjectFile(t, "volcano/cloud.yaml", "version: 1\nfunctions:\n  - name: order-pipeline\n    kind: durable\n    visibility: authenticated\n")
	fake := newFakeVolcano(t)
	server := fake.start()

	out, err := executeDeployCommand(t, New(fake.cloudDeps(server)), "-c", "volcano/cloud.yaml", "--no-wait")
	require.NoError(t, err, out)

	assert.Equal(t, []string{"durable order-pipeline", "config"}, fake.recorded())
}

// A manifest that owns the project's variables still deploys in a project
// whose volcano/volcano.env serves local runs.
// A cloud server on this machine is the project's API, so an env file naming
// it is the project's own values rather than the local development server's.
func TestDeployAcceptsTheAPIURLItDeploysTo(t *testing.T) {
	setDeployCommandTestHome(t)
	writeFullProject(t)
	fake := newFakeVolcano(t)
	server := fake.start()
	writeDeployProjectFile(t, "volcano/cloud.env", "API_URL=https://api.example.test\n"+
		"VOLCANO_API_URL="+server.URL+"\nNEXT_PUBLIC_VOLCANO_API_URL="+server.URL+"\n")

	out, err := executeDeployCommand(t, New(fake.cloudDeps(server)), "--env-file", "volcano/cloud.env")
	require.NoError(t, err, out)
	assert.Contains(t, out, "1. variables   volcano/cloud.env")

	writeDeployProjectFile(t, "volcano/cloud.env", "VOLCANO_API_URL=http://localhost:1\n")
	out, err = executeDeployCommand(t, New(fake.cloudDeps(server)), "--env-file", "volcano/cloud.env")
	require.Error(t, err)
	assert.Contains(t, err.Error()+out, "volcano/cloud.env holds the local development server's values (VOLCANO_API_URL)")
}

func TestDeployNoEnvFileLeavesTheEnvFileOut(t *testing.T) {
	setDeployCommandTestHome(t)
	writeDeployProjectFile(t, "volcano/volcano.env", "VOLCANO_API_URL=http://localhost:8000\n")
	writeDeployProjectFile(t, "volcano/volcano-config.yaml", "version: 1\nvariables:\n  - name: API_URL\n    value: https://api.example.test\n")
	fake := newFakeVolcano(t)
	server := fake.start()

	out, err := executeDeployCommand(t, New(fake.cloudDeps(server)), "--no-env-file")
	require.NoError(t, err, out)

	assert.Equal(t, []string{"config", "config"}, fake.recorded())
	assert.Contains(t, out, "1. variables   variable sections of volcano/volcano-config.yaml")
}

// A deploy can set a frontend's selection only to all or scoped with a list,
// so it says when a build won't get the selection the manifest declares.
func TestDeployWarnsWhenOnlyConfigCanApplyAFrontendSelection(t *testing.T) {
	setDeployCommandTestHome(t)
	writeDeployProjectFile(t, "volcano/volcano-config.yaml", "version: 1\nfrontends:\n  - name: web\n    variable_scope: shared\n")
	writeDeployProjectFile(t, ".volcano/source-layout.json",
		`{"version": 1, "frontends": [{"name": "web", "workspace": "", "app_root": "web"}]}`)
	writeDeployProjectFile(t, "web/package.json", `{"name": "web", "private": true}`)
	fake := newFakeVolcano(t)
	server := fake.start()

	out, err := executeDeployCommand(t, New(fake.cloudDeps(server)), "--no-wait")
	require.NoError(t, err, out)

	assert.NotContains(t, fake.frontends["web"], "variable_scope")
	assert.Contains(t, out, `Warning: a deploy can't apply the variable selection volcano/volcano-config.yaml declares for frontend "web"`)
	assert.Contains(t, out, "deploy again to rebuild with it")
}

// Frontend uploads archive their directory, which can hold the env file this
// run deployed; the packager's patterns only cover the usual names.
func TestDeployLeavesTheEnvFileOutOfFrontendUploads(t *testing.T) {
	setDeployCommandTestHome(t)
	writeDeployProjectFile(t, "cloud.env", "STRIPE_SECRET_KEY=sk_live_secret\n")
	writeDeployProjectFile(t, ".volcano/source-layout.json",
		`{"version": 1, "frontends": [{"name": "web", "workspace": "", "app_root": "web"}]}`)
	writeDeployProjectFile(t, "web/package.json", `{"name": "web", "private": true}`)
	fake := newFakeVolcano(t)
	server := fake.start()

	out, err := executeDeployCommand(t, New(fake.cloudDeps(server)), "--env-file", "cloud.env", "--no-wait")
	require.NoError(t, err, out)

	assert.Contains(t, fake.archives["web"], "web/package.json")
	assert.NotContains(t, fake.archives["web"], "cloud.env")
}

func TestDeploySkipsStepsWithNothingToDeploy(t *testing.T) {
	setDeployCommandTestHome(t)
	writeDeployProjectFile(t, "volcano-config.yaml", "version: 1\nrealtime:\n  enabled: true\n")
	fake := newFakeVolcano(t)
	server := fake.start()

	out, err := executeDeployCommand(t, New(fake.cloudDeps(server)))
	require.NoError(t, err, out)

	assert.Equal(t, []string{"config"}, fake.recorded())
	// No code step, so the project's deploy source does not matter.
	assert.NotContains(t, fake.reads, "source-export")
	for _, want := range []string{
		"1. variables   skipped: no env file, and no manifest declares variables",
		"3. functions   skipped: no volcano/functions directory",
		"4. durable     skipped: volcano-config.yaml declares no durable functions",
		"5. frontends   skipped: no .volcano/source-layout.json",
	} {
		assert.Contains(t, out, want)
	}
}

func TestDeployNoFrontendsLeavesThemOut(t *testing.T) {
	setDeployCommandTestHome(t)
	writeFullProject(t)
	fake := newFakeVolcano(t)
	server := fake.start()

	out, err := executeDeployCommand(t, New(fake.cloudDeps(server)), "--no-frontends")
	require.NoError(t, err, out)

	assert.NotContains(t, fake.recorded(), "frontend web")
	assert.Contains(t, out, "5. frontends   skipped: --no-frontends")
}

// A refusal comes before anything changes, so these runs send no mutating
// request at all.
func TestDeployRefusesBeforeChangingAnything(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T)
		args  []string
		want  []string
	}{
		{
			name: "default env file next to manifest variables",
			setup: func(t *testing.T) {
				t.Helper()
				writeDeployProjectFile(t, "volcano/volcano.env", "API_URL=https://api.example.test\n")
				writeDeployProjectFile(t, "volcano/volcano-config.yaml", "version: 1\nvariables:\n  - name: API_URL\n    value: x\n")
			},
			want: []string{
				"both volcano/volcano.env and the variables section of volcano/volcano-config.yaml set project variables",
				"pass --no-env-file, or remove the variables section from volcano/volcano-config.yaml",
			},
		},
		{
			name: "named env file next to manifest variables",
			setup: func(t *testing.T) {
				t.Helper()
				writeDeployProjectFile(t, "local.env", "API_URL=https://api.example.test\n")
				writeDeployProjectFile(t, "volcano/auth.yaml", "version: 1\nvariables: []\n")
			},
			args: []string{"--env-file", "local.env", "-c", "volcano/auth.yaml"},
			want: []string{"replace --env-file with --no-env-file, or remove the variables section from volcano/auth.yaml"},
		},
		{
			name: "env file and no env file",
			args: []string{"--env-file", "volcano/cloud.env", "--no-env-file"},
			want: []string{"--env-file and --no-env-file can't be used together"},
		},
		{
			name: "two manifests declaring variables",
			setup: func(t *testing.T) {
				t.Helper()
				writeDeployProjectFile(t, "volcano/app.yaml", "version: 1\nvariables:\n  - name: API_URL\n    value: x\n")
				writeDeployProjectFile(t, "volcano/auth.yaml", "version: 1\nvariables:\n  - name: AUTH_SECRET\n    value: y\n")
			},
			args: []string{"-c", "volcano/app.yaml", "-c", "volcano/auth.yaml"},
			want: []string{
				"volcano/app.yaml and volcano/auth.yaml each declare variables, and applying one deletes every entry it does not list",
				"Declare variables in one manifest",
			},
		},
		{
			name: "two manifests declaring frontend shared variables",
			setup: func(t *testing.T) {
				t.Helper()
				writeDeployProjectFile(t, "volcano/app.yaml", "version: 1\nfrontend_shared_variables: [API_URL]\n")
				writeDeployProjectFile(t, "volcano/web.yaml", "version: 1\nfrontend_shared_variables: []\n")
			},
			args: []string{"-c", "volcano/app.yaml", "-c", "volcano/web.yaml"},
			want: []string{"volcano/app.yaml and volcano/web.yaml each declare frontend_shared_variables"},
		},
		{
			name: "default manifest with durable functions left out",
			setup: func(t *testing.T) {
				t.Helper()
				writeDeployProjectFile(t, "volcano/functions/order-pipeline.js", `exports.handler = async () => ({});`)
				writeDeployProjectFile(t, "volcano/volcano-config.yaml", "version: 1\nfunctions:\n  - name: order-pipeline\n    kind: durable\n")
				writeDeployProjectFile(t, "volcano/auth.yaml", "version: 1\nauth:\n  email:\n    enabled: true\n")
			},
			args: []string{"-c", "volcano/auth.yaml"},
			want: []string{
				"volcano/volcano-config.yaml declares durable functions (order-pipeline) that volcano/auth.yaml, the first manifest, does not",
				"a function's kind can't change",
				"Pass volcano/volcano-config.yaml first, or declare them durable in volcano/auth.yaml",
			},
		},
		{
			name: "missing named manifest",
			args: []string{"-c", "volcano/missing.yaml"},
			want: []string{"specified file not found: volcano/missing.yaml"},
		},
		{
			name: "missing named env file",
			args: []string{"--env-file", "volcano/missing.env"},
			want: []string{"volcano/missing.env"},
		},
		{
			name: "migrations without a database",
			setup: func(t *testing.T) {
				t.Helper()
				writeDeployProjectFile(t, "volcano/migrations/001_init.sql", "select 1;")
			},
			args: []string{"--with-migrations"},
			want: []string{"--with-migrations needs --database: no manifest declares a database to default to"},
		},
		{
			name: "migrations with two declared databases",
			setup: func(t *testing.T) {
				t.Helper()
				writeDeployProjectFile(t, "volcano-config.yaml", `version: 1
databases:
  - {name: app, region: aws-us-east-1, pg_version: "17"}
  - {name: analytics, region: aws-us-east-1, pg_version: "17"}
`)
			},
			args: []string{"--with-migrations"},
			want: []string{"the manifests declare 2 databases (app, analytics)"},
		},
		{
			name: "database without migrations",
			args: []string{"-d", "app"},
			want: []string{`--database only picks where migrations go; add --with-migrations to apply them to "app"`},
		},
		{
			name: "local server values in the env file",
			setup: func(t *testing.T) {
				t.Helper()
				writeDeployProjectFile(t, "volcano/volcano.env", "VOLCANO_API_URL=http://localhost:8000\n"+
					"VOLCANO_SERVICE_KEY=sk-1111111111111111111111111111111111111111\n"+
					"NEXT_PUBLIC_VOLCANO_API_URL=http://127.0.0.1:8000\n"+
					"NEXT_PUBLIC_VOLCANO_ANON_KEY=ak-0000000000000000000000000000000000000000\n"+
					"VOLCANO_DATABASE=app\nGREETING=Hello\n")
			},
			want: []string{
				"volcano/volcano.env holds the local development server's values (NEXT_PUBLIC_VOLCANO_ANON_KEY, " +
					"NEXT_PUBLIC_VOLCANO_API_URL, VOLCANO_API_URL, VOLCANO_SERVICE_KEY)",
				"--env-file volcano/cloud.env",
			},
		},
		{
			name: "durable functions in a later manifest",
			setup: func(t *testing.T) {
				t.Helper()
				writeDeployProjectFile(t, "volcano/auth.yaml", "version: 1\nauth:\n  email:\n    enabled: true\n")
				writeDeployProjectFile(t, "volcano/volcano-config.yaml", "version: 1\nfunctions:\n  - name: order-pipeline\n    kind: durable\n")
			},
			args: []string{"-c", "volcano/auth.yaml", "-c", "volcano/volcano-config.yaml"},
			want: []string{
				"volcano/volcano-config.yaml declares durable functions (order-pipeline), which a deploy reads only from the first manifest, volcano/auth.yaml",
				"Move them into volcano/auth.yaml, or pass volcano/volcano-config.yaml first",
			},
		},
		{
			name: "frontend variables in a later manifest",
			setup: func(t *testing.T) {
				t.Helper()
				writeDeployProjectFile(t, "volcano/app.yaml", "version: 1\nrealtime:\n  enabled: true\n")
				writeDeployProjectFile(t, "volcano/web.yaml", "version: 1\nfrontends:\n  - name: web\n    variable_scope: scoped\n    variables: [API_URL]\n")
			},
			args: []string{"-c", "volcano/app.yaml", "-c", "volcano/web.yaml"},
			want: []string{`volcano/web.yaml declares the variables of frontend "web"`},
		},
		{
			name: "frontend without package.json",
			setup: func(t *testing.T) {
				t.Helper()
				writeDeployProjectFile(t, ".volcano/source-layout.json", `{"version": 1, "frontends": [{"name": "web"}]}`)
			},
			want: []string{`.volcano/source-layout.json: frontend "web" has no package.json in frontends/web`},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setDeployCommandTestHome(t)
			if tc.setup != nil {
				tc.setup(t)
			}
			fake := newFakeVolcano(t)
			server := fake.start()

			_, err := executeDeployCommand(t, New(fake.cloudDeps(server)), tc.args...)
			require.Error(t, err)
			for _, want := range tc.want {
				require.ErrorContains(t, err, want)
			}
			assert.Empty(t, fake.recorded())
		})
	}
}

// A project whose code deploys from Git refuses direct code deploys with a
// 409, so the run has to stop before it applies variables or migrations.
func TestDeployRefusesProjectsThatDeployFromGit(t *testing.T) {
	for _, mode := range []string{"git", "git_pending", "git_exporting"} {
		t.Run(mode, func(t *testing.T) {
			setDeployCommandTestHome(t)
			writeFullProject(t)
			fake := newFakeVolcano(t)
			fake.sourceMode = mode
			server := fake.start()

			_, err := executeDeployCommand(t, New(fake.cloudDeps(server)))
			require.ErrorContains(t, err, `project "Beta" deploys its code from its connected Git repository (source mode "`+mode+`")`)
			// A bare "variables deploy" reads volcano/volcano.env, which may
			// hold the local server's values; name the file this run vetted.
			require.ErrorContains(t, err, "deploy variables and configuration with:\n"+
				"  volcano cloud variables deploy -f volcano/volcano.env\n"+
				"  volcano cloud config deploy -f volcano/volcano-config.yaml")
			assert.Empty(t, fake.recorded())
		})
	}
}

// Each step's failure stops the run, names what already ran, and lists the
// commands that finish from the failed step on.
func TestDeployStopsAtTheFailingStep(t *testing.T) {
	const (
		variablesCommand = "  volcano cloud variables deploy -f volcano/volcano.env"
		functionsCommand = "  volcano cloud functions deploy --all"
		durableCommand   = "  volcano cloud durable deploy --all"
		frontendCommand  = "  volcano cloud frontends deploy --name web --path . --app-root web --variable-scope scoped --variable API_URL"
		configCommand    = "  volcano cloud config deploy -f volcano/volcano-config.yaml"
	)
	for _, tc := range []struct {
		step     string
		failPath string
		recorded []string
		done     string
		rest     []string
	}{
		{
			step:     "variables",
			failPath: "/variables",
			recorded: []string{"failed /variables"},
			rest:     []string{variablesCommand, functionsCommand, durableCommand, frontendCommand, configCommand},
		},
		{
			step:     "functions",
			failPath: "/functions/batch",
			recorded: []string{"variable API_URL", "failed /functions/batch"},
			done:     "variables",
			rest:     []string{functionsCommand, durableCommand, frontendCommand, configCommand},
		},
		{
			step:     "durable",
			failPath: "/durable-functions",
			recorded: []string{"variable API_URL", "function hello", "failed /durable-functions"},
			done:     "variables, functions",
			rest:     []string{durableCommand, frontendCommand, configCommand},
		},
		{
			step:     "frontends",
			failPath: "/frontends",
			recorded: []string{"variable API_URL", "function hello", "durable order-pipeline", "failed /frontends"},
			done:     "variables, functions, durable",
			rest:     []string{frontendCommand, configCommand},
		},
		{
			step:     "config",
			failPath: "/config",
			recorded: []string{"variable API_URL", "function hello", "durable order-pipeline", "frontend web", "failed /config"},
			done:     "variables, functions, durable, frontends",
			rest:     []string{configCommand},
		},
	} {
		t.Run(tc.step, func(t *testing.T) {
			setDeployCommandTestHome(t)
			writeFullProject(t)
			fake := newFakeVolcano(t)
			fake.failPath = tc.failPath
			server := fake.start()

			out, err := executeDeployCommand(t, New(fake.cloudDeps(server)))
			require.Error(t, err)

			assert.Equal(t, tc.recorded, fake.recorded())
			require.ErrorContains(t, err, tc.step+": ")
			hint := "Fix the cause and run the command again, or finish by hand:\n" + strings.Join(tc.rest, "\n")
			if tc.done != "" {
				hint = "Already deployed: " + tc.done + ".\n" + hint
			}
			require.ErrorContains(t, err, hint)
			assert.NotContains(t, out, "Waiting for")
		})
	}
}

// The standalone function commands sort kinds by the default manifest, so
// with another first manifest they could create a durable function as a
// standard one; the hint offers only the re-run.
func TestDeployResumeHintOnlyRerunsWhenTheFirstManifestIsNotTheDefault(t *testing.T) {
	setDeployCommandTestHome(t)
	writeDeployProjectFile(t, "volcano/functions/hello.js", `exports.handler = async () => ({});`)
	writeDeployProjectFile(t, "volcano/functions/order-pipeline.js", `exports.handler = async () => ({});`)
	writeDeployProjectFile(t, "volcano/prod.yaml", "version: 1\nfunctions:\n  - name: order-pipeline\n    kind: durable\n")
	fake := newFakeVolcano(t)
	fake.failPath = "/functions/batch"
	server := fake.start()

	_, err := executeDeployCommand(t, New(fake.cloudDeps(server)), "-c", "volcano/prod.yaml")
	require.Error(t, err)

	assert.True(t, strings.HasSuffix(err.Error(), "\n\nFix the cause and run the command again."), err.Error())
	assert.NotContains(t, err.Error(), "functions deploy --all")
}

// Without functions to deploy, kinds don't matter, so a default manifest the
// run doesn't read can't refuse it.
func TestDeployWithoutFunctionsIgnoresTheDefaultManifest(t *testing.T) {
	setDeployCommandTestHome(t)
	writeDeployProjectFile(t, "volcano/volcano-config.yaml", "version: [\n")
	writeDeployProjectFile(t, "volcano/auth.yaml", "version: 1\nauth:\n  email:\n    enabled: true\n")
	fake := newFakeVolcano(t)
	server := fake.start()

	out, err := executeDeployCommand(t, New(fake.cloudDeps(server)), "-c", "volcano/auth.yaml", "--no-wait")
	require.NoError(t, err, out)

	assert.Equal(t, []string{"config"}, fake.recorded())
}

// After the migrations ran, the hint says to leave them out of the next run.
func TestDeployResumeHintLeavesOutMigrationsThatRan(t *testing.T) {
	hint := resumeHint([]string{stepVariables, stepMigrations}, []step{
		{name: stepFunctions, manual: []string{"volcano functions deploy --all"}},
		{name: stepDurable, skipped: true, manual: []string{"volcano durable deploy --all"}},
	})

	assert.Equal(t, "Already deployed: variables, migrations.\n"+
		"Fix the cause and run the command again without --with-migrations and -d, since the migrations already ran, or finish by hand:\n"+
		"  volcano functions deploy --all", hint)
}

func TestDeployWaitsForEveryDeployment(t *testing.T) {
	setDeployCommandTestHome(t)
	writeFullProject(t)
	fake := newFakeVolcano(t)
	fake.statuses = map[string][]string{"web": {"queued", "provisioning", "active"}}
	fake.unlisted = map[string]bool{"hello": true}
	server := fake.start()
	deps := fake.cloudDeps(server)
	deps.NewTicker = func(time.Duration) cliruntime.Ticker { return tickNow{} }

	out, err := executeDeployCommand(t, New(deps))
	require.NoError(t, err, out)

	assert.Equal(t, 3, fake.polls)
	assert.Contains(t, out, "Waiting for 3 deployment(s)...")
	assert.Contains(t, out, "- hello           unchanged")
	assert.Contains(t, out, "✓ web             active")
}

// The server re-syncs variables changed during a build after the deployment
// completes, and the durable function refuses starts until that sync lands.
func TestDeployWaitsForTheDurableFunctionToAcceptExecutions(t *testing.T) {
	setDeployCommandTestHome(t)
	writeFullProject(t)
	fake := newFakeVolcano(t)
	fake.durableStatuses = map[string][]string{"order-pipeline": {"provisioning", "provisioning", "active"}}
	server := fake.start()
	deps := fake.cloudDeps(server)
	deps.NewTicker = func(time.Duration) cliruntime.Ticker { return tickNow{} }

	out, err := executeDeployCommand(t, New(deps))
	require.NoError(t, err, out)

	assert.Equal(t, 3, fake.durableReads["order-pipeline"])
	assert.Equal(t, 3, fake.polls)
	assert.Contains(t, out, "✓ order-pipeline  active")
}

func TestDeployReportsAFailedDeployment(t *testing.T) {
	setDeployCommandTestHome(t)
	writeFullProject(t)
	fake := newFakeVolcano(t)
	fake.statuses = map[string][]string{"web": {"failed"}}
	server := fake.start()

	out, err := executeDeployCommand(t, New(fake.cloudDeps(server)))
	require.EqualError(t, err, "1 deployment(s) failed: frontend web")

	assert.Contains(t, out, "✗ web             failed: next build exited with code 1")
	assert.Contains(t, out, "volcano cloud frontends logs web --type build")
	assert.NotContains(t, fake.reads, "frontend web")
}

// The config step supersedes deployments it redeploys, as on a region change.
// The newer deployment is the one that serves, so its failure fails the run.
func TestDeployWaitsForTheDeploymentThatSupersedesOne(t *testing.T) {
	setDeployCommandTestHome(t)
	writeFullProject(t)
	fake := newFakeVolcano(t)
	fake.superseding = map[string][]string{"web": {"provisioning", "failed"}}
	server := fake.start()
	deps := fake.cloudDeps(server)
	deps.NewTicker = func(time.Duration) cliruntime.Ticker { return tickNow{} }

	out, err := executeDeployCommand(t, New(deps))
	require.EqualError(t, err, "1 deployment(s) failed: frontend web")

	assert.Contains(t, out, "✗ web             failed: next build exited with code 1")
	assert.NotContains(t, out, "superseded")
}

// With no newer deployment in sight there is nothing to wait for, but nothing
// shows this run's deployment succeeded either.
func TestDeployDoesNotCountASupersededDeploymentAsDeployed(t *testing.T) {
	setDeployCommandTestHome(t)
	writeFullProject(t)
	fake := newFakeVolcano(t)
	fake.statuses = map[string][]string{"web": {"superseded"}}
	server := fake.start()

	out, err := executeDeployCommand(t, New(fake.cloudDeps(server)))
	require.NoError(t, err, out)

	assert.Contains(t, out, "- web             superseded by a newer deployment")
	assert.NotContains(t, out, "✓ web")
	assert.NotContains(t, fake.reads, "frontend web")
}

// The server supersedes only queued deployments. One already provisioning when
// the config step redeploys its resource stays, and the redeploy queues behind
// it; that redeploy is what serves, so the run waits for it too.
func TestDeployWaitsForARedeployQueuedBehindItsDeployment(t *testing.T) {
	setDeployCommandTestHome(t)
	writeFullProject(t)
	fake := newFakeVolcano(t)
	fake.queuedBehind = map[string][]string{"web": {"queued", "provisioning", "failed"}}
	server := fake.start()
	deps := fake.cloudDeps(server)
	deps.NewTicker = func(time.Duration) cliruntime.Ticker { return tickNow{} }

	out, err := executeDeployCommand(t, New(deps))
	require.EqualError(t, err, "1 deployment(s) failed: frontend web")

	assert.Equal(t, 3, fake.polls)
	assert.Contains(t, out, "✗ web             failed: next build exited with code 1")
	assert.NotContains(t, out, "✓ web")
}

func TestDeployNoWaitReturnsOnceStarted(t *testing.T) {
	setDeployCommandTestHome(t)
	writeFullProject(t)
	fake := newFakeVolcano(t)
	server := fake.start()

	out, err := executeDeployCommand(t, New(fake.cloudDeps(server)), "--no-wait")
	require.NoError(t, err, out)

	assert.NotContains(t, fake.reads, "deployments")
	assert.Contains(t, out, "Deployments started; not waiting for them to finish")
}

// Local mode deploys functions one by one, has no deploy source to check, and
// migrates the app database unless told otherwise.
func TestLocalDeployTargetsTheLocalServer(t *testing.T) {
	setDeployCommandTestHome(t)
	writeFullProject(t)
	fake := newFakeVolcano(t)
	server := fake.start()

	out, err := executeDeployCommand(t, NewLocal(fake.localDeps(server)), "--no-wait")
	require.NoError(t, err, out)

	assert.Equal(t, []string{
		"variable API_URL",
		"function hello",
		"durable order-pipeline",
		"frontend web",
		"config",
	}, fake.recorded())
	assert.NotContains(t, fake.reads, "source-export")
	assert.Contains(t, out, `Deploying to local project "Beta"`)
}

// The starters' volcano/volcano.env is written for the local server, so a
// local deploy sends it as it is.
func TestLocalDeployDeploysTheLocalServerValues(t *testing.T) {
	setDeployCommandTestHome(t)
	writeDeployProjectFile(t, "volcano/volcano.env", "VOLCANO_API_URL=http://localhost:8000\n"+
		"VOLCANO_ANON_KEY=ak-0000000000000000000000000000000000000000\n")
	fake := newFakeVolcano(t)
	server := fake.start()

	out, err := executeDeployCommand(t, NewLocal(fake.localDeps(server)))
	require.NoError(t, err, out)

	assert.ElementsMatch(t, []string{"variable VOLCANO_ANON_KEY", "variable VOLCANO_API_URL"}, fake.recorded())
}

func TestLocalDeployMigratesTheAppDatabase(t *testing.T) {
	setDeployCommandTestHome(t)
	writeDeployProjectFile(t, "volcano/migrations/001_init.sql", "create table notes (id int);")
	writeDeployProjectFile(t, "volcano/functions/hello.js", `exports.handler = async () => ({});`)
	fake := newFakeVolcano(t)
	server := fake.start()

	out, err := executeDeployCommand(t, NewLocal(fake.localDeps(server)), "--with-migrations")
	require.Error(t, err)

	assert.Contains(t, out, "2. migrations  volcano/migrations -> database app")
	assert.Empty(t, fake.recorded())
	require.ErrorContains(t, err, `migrations: database "app" is not active (status: provisioning)`)
	assert.ErrorContains(t, err, `Fix the cause and run the command again, or finish by hand:
  volcano migrations deploy --all -d app
  volcano functions deploy --all`)
}

func TestFrontendSelectionComesFromTheFirstManifest(t *testing.T) {
	manifest, err := projectconfig.Parse([]byte(`version: 1
frontends:
  - name: scoped
    variable_scope: scoped
    variables: [API_URL]
  - name: none-selected
    variable_scope: scoped
    variables: []
  - name: everything
    variable_scope: all
  - name: shared
    variable_scope: shared
  - name: kept
    variable_scope: scoped
  - name: list-only
    variables: [API_URL]
  - name: routes-only
    function_routes: []
`), func(string) (string, bool) { return "", false })
	require.NoError(t, err)
	b := planBuilder{manifests: []manifestFile{{path: "volcano-config.yaml", manifest: manifest}}}

	for _, tc := range []struct {
		name      string
		scope     string
		variables []string
		deferred  bool
	}{
		{name: "scoped", scope: "scoped", variables: []string{"API_URL"}},
		{name: "none-selected", scope: "scoped", variables: []string{}},
		{name: "everything", scope: "all"},
		{name: "shared", deferred: true},
		{name: "kept", deferred: true},
		{name: "list-only", deferred: true},
		{name: "routes-only"},
		{name: "undeclared"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scope, variables, deferred := b.frontendSelection(tc.name)
			assert.Equal(t, tc.scope, scope)
			assert.Equal(t, tc.variables, variables)
			assert.Equal(t, tc.deferred, deferred)
		})
	}
}
