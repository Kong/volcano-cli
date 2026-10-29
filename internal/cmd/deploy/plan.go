package deploy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	configcmd "github.com/Kong/volcano-cli/internal/cmd/config"
	migrationcmd "github.com/Kong/volcano-cli/internal/cmd/databases/migration"
	durablecmd "github.com/Kong/volcano-cli/internal/cmd/durable"
	frontendscmd "github.com/Kong/volcano-cli/internal/cmd/frontends"
	functionscmd "github.com/Kong/volcano-cli/internal/cmd/functions"
	variablescmd "github.com/Kong/volcano-cli/internal/cmd/variables"
	clidatabase "github.com/Kong/volcano-cli/internal/database"
	"github.com/Kong/volcano-cli/internal/envfile"
	clifrontend "github.com/Kong/volcano-cli/internal/frontend"
	"github.com/Kong/volcano-cli/internal/output"
	"github.com/Kong/volcano-cli/internal/project"
	"github.com/Kong/volcano-cli/internal/projectconfig"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	"github.com/Kong/volcano-cli/internal/session"
	"github.com/Kong/volcano-cli/internal/theme"
)

const (
	functionsDir         = "volcano/functions"
	migrationsDir        = clidatabase.MigrationsDir
	defaultLocalDatabase = "app"
)

const (
	stepVariables  = "variables"
	stepMigrations = "migrations"
	stepFunctions  = "functions"
	stepDurable    = "durable"
	stepFrontends  = "frontends"
	stepConfig     = "config"
)

type manifestFile struct {
	path     string
	manifest *projectconfig.Manifest
}

type step struct {
	name string
	// detail names what the step deploys, or why it is skipped.
	detail  string
	skipped bool
	// manual lists the commands that do the step by hand.
	manual []string
	run    func(ctx context.Context, r *runner) error
}

type plan struct {
	local    bool
	target   project.Target
	steps    []step
	warnings []string
}

// prepare reads and checks every input before anything is deployed, so a
// refusal leaves the project untouched.
func prepare(ctx context.Context, opts options) (*plan, error) {
	manifests, err := loadManifests(opts.configs)
	if err != nil {
		return nil, err
	}
	if err := checkLaterManifests(manifests); err != nil {
		return nil, err
	}
	hasFunctions, err := isDir(functionsDir)
	if err != nil {
		return nil, err
	}
	if hasFunctions {
		if err := checkDefaultManifest(manifests); err != nil {
			return nil, err
		}
	}
	envFile, err := resolveEnvFile(opts.envFile, opts.noEnvFile)
	if err != nil {
		return nil, err
	}
	if err := checkVariableSources(envFile, opts.envFile != "", manifests); err != nil {
		return nil, err
	}
	if !opts.deps.LocalMode && envFile != "" {
		if err := refuseLocalValues(envFile, deployAPIURL(opts.deps)); err != nil {
			return nil, err
		}
	}
	var durable []string
	if len(manifests) > 0 {
		durable = manifests[0].manifest.DurableFunctionNames()
	}
	frontends, layoutFound, err := readFrontends(opts.noFrontends)
	if err != nil {
		return nil, err
	}
	migrations, err := migrationTargets(opts, manifests)
	if err != nil {
		return nil, err
	}

	projects := project.NewService(opts.deps)
	target, err := projects.CurrentTarget(ctx)
	if err != nil {
		return nil, err
	}
	if hasMigrationFiles(migrations) {
		if err := requireMigrationRuns(ctx, opts.deps, migrations); err != nil {
			return nil, err
		}
	}
	deploysCode := hasFunctions || len(durable) > 0 || len(frontends) > 0
	if !opts.deps.LocalMode && deploysCode {
		mode, err := projects.SourceMode(ctx)
		if err != nil {
			return nil, err
		}
		if mode != apiclient.ProjectSourceExportStateModePlatform {
			return nil, gitSourceError(opts.deps, target, mode, envFile, manifests)
		}
	}

	primaryPath := ""
	if len(manifests) > 0 {
		primaryPath = manifests[0].path
	}
	b := planBuilder{deps: opts.deps, manifests: manifests, defaultKinds: readsDefaultManifest(manifests)}
	frontendsStep, warnings := b.frontends(frontends, layoutFound, opts.noFrontends, envFile)
	return &plan{
		local:  opts.deps.LocalMode,
		target: target,
		steps: []step{
			b.variables(envFile),
			b.migrations(opts.withMigrations, migrations),
			b.functions(hasFunctions, primaryPath, len(durable) > 0),
			b.durable(durable, primaryPath),
			frontendsStep,
			b.config(),
		},
		warnings: warnings,
	}, nil
}

// gitSourceError refuses to deploy code to a project that builds from its Git
// repository, and names the commands that still apply.
func gitSourceError(deps cliruntime.Deps, target project.Target, mode apiclient.ProjectSourceExportStateMode, envFile string, manifests []manifestFile) error {
	var commands []string
	if envFile != "" {
		commands = append(commands, cliruntime.CommandPath(deps, "variables deploy -f "+envFile))
	}
	for _, m := range manifests {
		commands = append(commands, cliruntime.CommandPath(deps, "config deploy -f "+m.path))
	}
	refusal := fmt.Sprintf("%s deploys its code from its connected Git repository (source mode %q), so functions and frontends can't be deployed from here.\n",
		targetLabel(target), mode)
	if len(commands) == 0 {
		return errors.New(refusal + "Push to the repository to deploy code")
	}
	return fmt.Errorf("%sPush to the repository to deploy code, and deploy variables and configuration with:\n  %s",
		refusal, strings.Join(commands, "\n  "))
}

func (p *plan) print(w, errOut io.Writer) {
	on := theme.On(w)
	where := "cloud"
	if p.local {
		where = "local"
	}
	fmt.Fprintf(w, "Deploying to %s %s\n", where, targetLabel(p.target))
	for i, s := range p.steps {
		detail := s.detail
		if s.skipped {
			detail = theme.Dim("skipped: "+detail, on)
		}
		fmt.Fprintf(w, "  %d. %-11s %s\n", i+1, s.name, detail)
	}
	for _, warning := range p.warnings {
		output.Warning(errOut, "%s", warning)
	}
}

func targetLabel(target project.Target) string {
	if target.Name == "" {
		return "project " + target.ID.String()
	}
	return fmt.Sprintf("project %q", target.Name)
}

// loadManifests loads the manifests named by -c, or the default one. Every
// named file must exist; a missing default means there is none.
func loadManifests(paths []string) ([]manifestFile, error) {
	if len(paths) == 0 {
		path, err := projectconfig.ResolveManifestPath("")
		if errors.Is(err, projectconfig.ErrManifestNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		paths = []string{path}
	}
	manifests := make([]manifestFile, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if _, err := projectconfig.ResolveManifestPath(path); err != nil {
			return nil, err
		}
		manifest, _, err := projectconfig.Load(path)
		if err != nil {
			return nil, err
		}
		manifests = append(manifests, manifestFile{path: path, manifest: manifest})
	}
	return manifests, nil
}

// checkLaterManifests refuses durable functions or a frontend variable
// selection in any manifest but the first. The code steps read both from the
// first manifest only, and a function created as the wrong kind stays that
// kind.
func checkLaterManifests(manifests []manifestFile) error {
	for _, m := range manifests[min(1, len(manifests)):] {
		var declared []string
		if names := m.manifest.DurableFunctionNames(); len(names) > 0 {
			declared = append(declared, "durable functions ("+strings.Join(names, ", ")+")")
		}
		if m.manifest.Frontends != nil {
			for _, fe := range *m.manifest.Frontends {
				if fe.VariableScope != nil || fe.Variables != nil {
					declared = append(declared, fmt.Sprintf("the variables of frontend %q", fe.Name))
				}
			}
		}
		if len(declared) == 0 {
			continue
		}
		primary := manifests[0].path
		return fmt.Errorf("%s declares %s, which a deploy reads only from the first manifest, %s.\n"+
			"Move them into %s, or pass %s first",
			m.path, strings.Join(declared, " and "), primary, primary, m.path)
	}
	return nil
}

// checkDefaultManifest refuses a -c list that leaves out the default manifest
// when it declares durable functions the first manifest doesn't. The functions
// step would create them as standard functions.
func checkDefaultManifest(manifests []manifestFile) error {
	if len(manifests) == 0 {
		return nil
	}
	path, err := projectconfig.ResolveManifestPath("")
	if errors.Is(err, projectconfig.ErrManifestNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	primary := manifests[0]
	if samePath(path, primary.path) {
		return nil
	}
	defaultManifest, _, err := projectconfig.Load(path)
	if err != nil {
		return fmt.Errorf("%s is not passed with -c, but its durable functions still decide function kinds: %w", path, err)
	}
	declared := primary.manifest.DurableFunctionNames()
	var missing []string
	for _, name := range defaultManifest.DurableFunctionNames() {
		if !slices.Contains(declared, name) {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("%s declares durable functions (%s) that %s, the first manifest, does not; "+
		"this deploy would create them as standard functions, and a function's kind can't change.\n"+
		"Pass %s first, or declare them durable in %s",
		path, strings.Join(missing, ", "), primary.path, path, primary.path)
}

// readsDefaultManifest reports whether the first manifest is the one
// `functions deploy` and `durable deploy` read on their own, so they sort
// function kinds the way this run does.
func readsDefaultManifest(manifests []manifestFile) bool {
	if len(manifests) == 0 {
		return true
	}
	path, err := projectconfig.ResolveManifestPath("")
	return err == nil && samePath(path, manifests[0].path)
}

func samePath(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	return errA == nil && errB == nil && absA == absB
}

// The local development server's fixed keys, which the starters'
// volcano/volcano.env holds.
var localServerKeys = []string{
	"ak-0000000000000000000000000000000000000000",
	"sk-1111111111111111111111111111111111111111",
}

// refuseLocalValues keeps the local server's values out of a cloud project: its
// keys, or an API URL on this machine, which a cloud frontend would compile in.
// An API URL on this machine that names apiURL, the API this deploy targets, is
// the project's own, as when a cloud server runs on this machine.
func refuseLocalValues(path, apiURL string) error {
	file, err := envfile.Load(path)
	if err != nil {
		return err
	}
	var local []string
	for name, value := range file.Variables {
		if slices.Contains(localServerKeys, value) ||
			(strings.HasSuffix(name, "VOLCANO_API_URL") && isLoopbackURL(value) && !sameOrigin(value, apiURL)) {
			local = append(local, name)
		}
	}
	if len(local) == 0 {
		return nil
	}
	slices.Sort(local)
	return fmt.Errorf("%s holds the local development server's values (%s), which do not work in a cloud project.\n"+
		"Put the cloud project's values in another env file and pass it, for example --env-file volcano/cloud.env",
		path, strings.Join(local, ", "))
}

// deployAPIURL is the API a cloud deploy talks to, or "" when the config can't
// be read; the deploy reports that error when it first reaches the API.
func deployAPIURL(deps cliruntime.Deps) string {
	factory := session.NewFactory(deps)
	cfg, err := factory.Config()
	if err != nil {
		return ""
	}
	return factory.APIURL(cfg)
}

func sameOrigin(a, b string) bool {
	ua, errA := url.Parse(strings.TrimSpace(a))
	ub, errB := url.Parse(strings.TrimSpace(b))
	return errA == nil && errB == nil && ua.Hostname() != "" &&
		strings.EqualFold(ua.Scheme, ub.Scheme) && strings.EqualFold(ua.Hostname(), ub.Hostname()) &&
		effectivePort(ua) == effectivePort(ub)
}

func effectivePort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	switch strings.ToLower(u.Scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	}
	return ""
}

func isLoopbackURL(value string) bool {
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsUnspecified())
}

func resolveEnvFile(flag string, none bool) (string, error) {
	if none {
		if flag != "" {
			return "", errors.New("--env-file and --no-env-file can't be used together")
		}
		return "", nil
	}
	path, err := envfile.ResolvePath(flag)
	if errors.Is(err, envfile.ErrNotFound) && flag == "" {
		return "", nil
	}
	return path, err
}

// variableSections are the manifest sections that replace a whole set when
// applied, so only one source may declare each.
var variableSections = []struct {
	name     string
	declared func(*projectconfig.Manifest) bool
}{
	{"variables", func(m *projectconfig.Manifest) bool { return m.Variables != nil }},
	{"shared_variables", func(m *projectconfig.Manifest) bool { return m.SharedVariables != nil }},
	{"frontend_shared_variables", func(m *projectconfig.Manifest) bool { return m.FrontendSharedVariables != nil }},
}

// checkVariableSources refuses two sources for one set of variables: two
// manifests declaring the same section, or an env file next to a variables
// section. Each section is fully synced, so applying one deletes what the
// other set.
func checkVariableSources(envFile string, named bool, manifests []manifestFile) error {
	for _, section := range variableSections {
		var declaring []string
		for _, m := range manifests {
			if section.declared(m.manifest) {
				declaring = append(declaring, m.path)
			}
		}
		if len(declaring) > 1 {
			return fmt.Errorf("%s each declare %s, and applying one deletes every entry it does not list, so the last one wins.\n"+
				"Declare %s in one manifest", strings.Join(declaring, " and "), section.name, section.name)
		}
	}
	if envFile == "" {
		return nil
	}
	for _, m := range manifests {
		if !m.manifest.DeclaresVariables() {
			continue
		}
		fix := "pass --no-env-file"
		if named {
			fix = "replace --env-file with --no-env-file"
		}
		return fmt.Errorf("both %s and the variables section of %s set project variables, and applying %s deletes every variable it does not list.\n"+
			"Keep variables in one place: %s, or remove the variables section from %s",
			envFile, m.path, m.path, fix, m.path)
	}
	return nil
}

// readFrontends lists the frontends in the layout file and checks that each
// one's app has a package.json.
func readFrontends(skip bool) ([]clifrontend.LayoutFrontend, bool, error) {
	if skip {
		return nil, false, nil
	}
	layout, found, err := clifrontend.ReadSourceLayout(".")
	if err != nil || !found {
		return nil, found, err
	}
	for _, fe := range layout.Frontends {
		app := filepath.Join(frontendPath(fe), filepath.FromSlash(fe.AppRoot))
		found, err := clifrontend.HasBuildPackage(frontendPath(fe), app)
		if err != nil {
			return nil, true, err
		}
		if !found {
			return nil, true, fmt.Errorf("%s: frontend %q has no package.json in %s", clifrontend.SourceLayoutPath, fe.Name, app)
		}
	}
	return layout.Frontends, true, nil
}

func frontendPath(fe clifrontend.LayoutFrontend) string {
	if fe.Dir() == "" {
		return "."
	}
	return filepath.FromSlash(fe.Dir())
}

// migrationTarget is a database --with-migrations applies files to.
type migrationTarget struct {
	database string
	location string
	files    []clidatabase.MigrationFile
}

// migrationTargets picks the databases --with-migrations applies to and their
// files. -d names one. Otherwise, with a directory per database, every
// directory's database; with the files in volcano/migrations itself, a local
// project's database named app, or the one database the manifests declare.
func migrationTargets(opts options, manifests []manifestFile) ([]migrationTarget, error) {
	if !opts.withMigrations && opts.database != "" {
		return nil, fmt.Errorf("--database only picks where migrations go; add --with-migrations to apply them to %q", opts.database)
	}
	if !opts.withMigrations {
		return nil, nil
	}
	migrations, err := clidatabase.ReadMigrations(migrationsDir)
	if err != nil {
		return nil, err
	}
	var databases []string
	switch {
	case opts.database != "":
		databases = []string{opts.database}
	case migrations.Nested():
		databases = migrations.Databases()
	default:
		database, err := defaultMigrationDatabase(opts, manifests)
		if err != nil {
			return nil, err
		}
		databases = []string{database}
	}
	targets := make([]migrationTarget, len(databases))
	for i, database := range databases {
		targets[i] = migrationTarget{database: database, location: migrations.Location(database), files: migrations.For(database)}
	}
	return targets, nil
}

func defaultMigrationDatabase(opts options, manifests []manifestFile) (string, error) {
	if opts.deps.LocalMode {
		return defaultLocalDatabase, nil
	}
	var declared []string
	for _, m := range manifests {
		if m.manifest.Databases == nil {
			continue
		}
		for _, db := range *m.manifest.Databases {
			if !slices.Contains(declared, db.Name) {
				declared = append(declared, db.Name)
			}
		}
	}
	switch len(declared) {
	case 1:
		return declared[0], nil
	case 0:
		return "", errors.New("--with-migrations needs --database: no manifest declares a database to default to")
	default:
		return "", fmt.Errorf("--with-migrations needs --database: the manifests declare %d databases (%s)", len(declared), strings.Join(declared, ", "))
	}
}

// requireMigrationRuns refuses the whole deploy, before anything changes,
// where the API takes no tracked migration runs, and names the commands that
// run the files without tracking instead.
func requireMigrationRuns(ctx context.Context, deps cliruntime.Deps, targets []migrationTarget) error {
	err := clidatabase.NewService(deps).RequireMigrationRuns(ctx)
	if !errors.Is(err, clidatabase.ErrMigrationRunsUnavailable) {
		return err
	}
	var untracked []string
	for _, target := range targets {
		if len(target.files) > 0 {
			untracked = append(untracked, cliruntime.CommandPath(deps, "databases migration up --all -d "+cliruntime.ShellQuote(target.database)))
		}
	}
	return api.WithCause(fmt.Errorf("--with-migrations applies migrations in tracked runs, which aren't available on this Volcano API "+
		"(capability %q). Nothing was deployed.\n"+
		"To run the files without tracking, which runs every file again, use:\n  %s\nthen deploy without --with-migrations",
		clidatabase.MigrationsCapability, strings.Join(untracked, "\n  ")), err)
}

func hasMigrationFiles(targets []migrationTarget) bool {
	return slices.ContainsFunc(targets, func(t migrationTarget) bool { return len(t.files) > 0 })
}

func isDir(path string) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return info.IsDir(), nil
}

type planBuilder struct {
	deps      cliruntime.Deps
	manifests []manifestFile
	// defaultKinds is false when the first manifest isn't the one the
	// standalone function commands read; running them by hand could create a
	// durable function as a standard one, so those steps offer no commands.
	defaultKinds bool
}

func (b planBuilder) command(args string) string {
	return cliruntime.CommandPath(b.deps, args)
}

// variables deploys the env file, then the variable sections of the
// manifests. Variables have to exist before a scoped function or frontend
// that selects them is deployed, and shared membership decides what a
// function receives when it deploys.
func (b planBuilder) variables(envFile string) step {
	type subset struct {
		path     string
		manifest *projectconfig.Manifest
	}
	var subsets []subset
	for _, m := range b.manifests {
		if only, ok := m.manifest.VariablesOnly(); ok {
			subsets = append(subsets, subset{path: m.path, manifest: only})
		}
	}
	s := step{name: stepVariables}
	var sources []string
	if envFile != "" {
		sources = append(sources, envFile)
		s.manual = append(s.manual, b.command("variables deploy -f "+envFile))
	}
	for _, sub := range subsets {
		sources = append(sources, "variable sections of "+sub.path)
		s.manual = append(s.manual, b.command("config deploy -f "+sub.path))
	}
	if len(sources) == 0 {
		s.skipped = true
		s.detail = "no env file, and no manifest declares variables"
		return s
	}
	s.detail = strings.Join(sources, ", ")
	s.run = func(ctx context.Context, r *runner) error {
		if envFile != "" {
			if err := variablescmd.Deploy(ctx, r.opts.deps, envFile, r.opts.out); err != nil {
				return err
			}
		}
		for _, sub := range subsets {
			if err := configcmd.DeployManifest(ctx, r.opts.deps, sub.manifest, sub.path, r.opts.out, r.opts.errOut); err != nil {
				return err
			}
		}
		return nil
	}
	return s
}

func (b planBuilder) migrations(enabled bool, targets []migrationTarget) step {
	s := step{name: stepMigrations}
	if !enabled {
		s.skipped = true
		s.detail = "pass --with-migrations to apply " + migrationsDir
		return s
	}
	var details []string
	var apply []migrationTarget
	for _, target := range targets {
		if len(target.files) > 0 {
			details = append(details, target.location+" -> database "+target.database)
			apply = append(apply, target)
			s.manual = append(s.manual, b.command("databases migration apply -d "+cliruntime.ShellQuote(target.database)))
		}
	}
	if len(apply) == 0 {
		s.skipped = true
		s.detail = "no migration files in " + migrationsDir
		return s
	}
	s.detail = strings.Join(details, ", ")
	s.run = func(ctx context.Context, r *runner) error {
		for _, target := range apply {
			if _, err := migrationcmd.Apply(ctx, r.opts.deps, migrationcmd.ApplyOptions{
				Database: target.database,
				Files:    target.files,
				Location: target.location,
				Wait:     true,
				Out:      r.opts.out,
			}); err != nil {
				return err
			}
		}
		return nil
	}
	return s
}

func (b planBuilder) functions(present bool, manifestPath string, durableFollows bool) step {
	s := step{name: stepFunctions}
	if !present {
		s.skipped = true
		s.detail = "no " + functionsDir + " directory"
		return s
	}
	s.detail = functionsDir
	if b.defaultKinds {
		s.manual = []string{b.command("functions deploy --all")}
	}
	s.run = func(ctx context.Context, r *runner) error {
		started, err := functionscmd.DeployAll(ctx, r.opts.deps, functionscmd.DeployAllOptions{
			ManifestPath:   manifestPath,
			Batch:          !r.opts.deps.LocalMode,
			DurableFollows: durableFollows,
			Out:            r.opts.out,
			ErrOut:         r.opts.errOut,
		})
		for _, fn := range started {
			r.track(kindFunction, fn.Name, fn.Id, fn.CurrentDeploymentId, fn.PendingDeploymentId)
		}
		return err
	}
	return s
}

func (b planBuilder) durable(names []string, manifestPath string) step {
	s := step{name: stepDurable}
	if len(names) == 0 {
		s.skipped = true
		s.detail = "no manifest declares durable functions"
		if manifestPath != "" {
			s.detail = manifestPath + " declares no durable functions"
		}
		return s
	}
	s.detail = strings.Join(names, ", ")
	if b.defaultKinds {
		s.manual = []string{b.command("durable deploy --all")}
	}
	s.run = func(ctx context.Context, r *runner) error {
		started, err := durablecmd.DeployAll(ctx, r.opts.deps, durablecmd.DeployAllOptions{
			ManifestPath: manifestPath,
			Out:          r.opts.out,
			ErrOut:       r.opts.errOut,
		})
		for _, fn := range started {
			r.track(kindDurable, fn.Name, fn.Id, fn.CurrentDeploymentId, fn.PendingDeploymentId)
		}
		return err
	}
	return s
}

// frontends deploys each frontend in the layout. The warnings name manifest
// entries whose variable selection only the config step can apply.
func (b planBuilder) frontends(frontends []clifrontend.LayoutFrontend, layoutFound, skip bool, envFile string) (step, []string) {
	s := step{name: stepFrontends}
	switch {
	case skip:
		s.skipped = true
		s.detail = "--no-frontends"
		return s, nil
	case !layoutFound:
		s.skipped = true
		s.detail = "no " + clifrontend.SourceLayoutPath
		return s, nil
	case len(frontends) == 0:
		s.skipped = true
		s.detail = clifrontend.SourceLayoutPath + " lists no frontends"
		return s, nil
	}
	var exclude []string
	if envFile != "" {
		exclude = []string{envFile}
	}
	names := make([]string, 0, len(frontends))
	deploys := make([]frontendscmd.DeployOptions, 0, len(frontends))
	var warnings []string
	for _, fe := range frontends {
		names = append(names, fe.Name)
		opts := frontendscmd.DeployOptions{
			Name:         fe.Name,
			Path:         frontendPath(fe),
			AppRoot:      fe.AppRoot,
			ExcludeFiles: exclude,
		}
		var deferred bool
		opts.VariableScope, opts.Variables, deferred = b.frontendSelection(fe.Name)
		if deferred {
			warnings = append(warnings, fmt.Sprintf("a deploy can't apply the variable selection %s declares for frontend %q, so it builds with "+
				"the selection the frontend has (none if it is new). The config step applies it afterwards; deploy again to rebuild with it",
				b.manifests[0].path, fe.Name))
		}
		deploys = append(deploys, opts)
		s.manual = append(s.manual, b.frontendCommand(opts))
	}
	s.detail = fmt.Sprintf("%s (%s)", strings.Join(names, ", "), clifrontend.SourceLayoutPath)
	s.run = func(ctx context.Context, r *runner) error {
		for _, opts := range deploys {
			fmt.Fprintf(r.opts.out, "\nDeploying frontend %s\n", opts.Name)
			opts.Out = r.opts.out
			deployed, err := frontendscmd.Deploy(ctx, r.opts.deps, opts)
			if err != nil {
				return fmt.Errorf("frontend %q: %w", opts.Name, err)
			}
			r.track(kindFrontend, deployed.Name, deployed.Id, deployed.CurrentDeploymentId, deployed.PendingDeploymentId)
		}
		return nil
	}
	return s, warnings
}

// frontendSelection is the variable selection a frontend is built with, from
// its entry in the first manifest. A deploy can only set all or scoped; a
// shared frontend, or an entry that leaves either field out, keeps what the
// frontend has until the config step applies the entry, and deferred reports
// that.
func (b planBuilder) frontendSelection(name string) (scope string, variables []string, deferred bool) {
	if len(b.manifests) == 0 || b.manifests[0].manifest.Frontends == nil {
		return "", nil, false
	}
	for _, fe := range *b.manifests[0].manifest.Frontends {
		if fe.Name != name || (fe.VariableScope == nil && fe.Variables == nil) {
			continue
		}
		switch {
		case fe.VariableScope != nil && *fe.VariableScope == "all":
			return "all", nil, false
		case fe.VariableScope != nil && *fe.VariableScope == "scoped" && fe.Variables != nil:
			return "scoped", *fe.Variables, false
		default:
			return "", nil, true
		}
	}
	return "", nil, false
}

func (b planBuilder) frontendCommand(opts frontendscmd.DeployOptions) string {
	args := []string{"frontends deploy --name", opts.Name, "--path", opts.Path}
	if opts.AppRoot != "" {
		args = append(args, "--app-root", opts.AppRoot)
	}
	if opts.VariableScope != "" {
		args = append(args, "--variable-scope", opts.VariableScope)
	}
	for _, name := range opts.Variables {
		args = append(args, "--variable", name)
	}
	return b.command(strings.Join(args, " "))
}

func (b planBuilder) config() step {
	s := step{name: stepConfig}
	if len(b.manifests) == 0 {
		s.skipped = true
		s.detail = "no volcano-config.yaml"
		return s
	}
	paths := make([]string, 0, len(b.manifests))
	for _, m := range b.manifests {
		paths = append(paths, m.path)
		s.manual = append(s.manual, b.command("config deploy -f "+m.path))
	}
	s.detail = strings.Join(paths, ", ")
	manifests := b.manifests
	s.run = func(ctx context.Context, r *runner) error {
		for _, m := range manifests {
			if err := configcmd.DeployManifest(ctx, r.opts.deps, m.manifest, m.path, r.opts.out, r.opts.errOut); err != nil {
				return err
			}
		}
		return nil
	}
	return s
}
