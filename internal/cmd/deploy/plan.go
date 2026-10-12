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

	"github.com/Kong/volcano-cli/internal/apiclient"
	configcmd "github.com/Kong/volcano-cli/internal/cmd/config"
	migrationcmd "github.com/Kong/volcano-cli/internal/cmd/databases/migration"
	durablecmd "github.com/Kong/volcano-cli/internal/cmd/durable"
	frontendscmd "github.com/Kong/volcano-cli/internal/cmd/frontends"
	functionscmd "github.com/Kong/volcano-cli/internal/cmd/functions"
	variablescmd "github.com/Kong/volcano-cli/internal/cmd/variables"
	"github.com/Kong/volcano-cli/internal/envfile"
	clifrontend "github.com/Kong/volcano-cli/internal/frontend"
	"github.com/Kong/volcano-cli/internal/project"
	"github.com/Kong/volcano-cli/internal/projectconfig"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	"github.com/Kong/volcano-cli/internal/theme"
)

const (
	functionsDir         = "volcano/functions"
	migrationsDir        = "volcano/migrations"
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
	local  bool
	target project.Target
	steps  []step
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
	envFile, err := resolveEnvFile(opts.envFile)
	if err != nil {
		return nil, err
	}
	if err := checkVariableSources(envFile, opts.envFile != "", manifests); err != nil {
		return nil, err
	}
	if !opts.deps.LocalMode && envFile != "" {
		if err := refuseLocalValues(envFile); err != nil {
			return nil, err
		}
	}
	hasFunctions, err := isDir(functionsDir)
	if err != nil {
		return nil, err
	}
	var durable []string
	if len(manifests) > 0 {
		durable = manifests[0].manifest.DurableFunctionNames()
	}
	frontends, layoutFound, err := readFrontends(opts.noFrontends)
	if err != nil {
		return nil, err
	}
	database, err := migrationDatabase(opts, manifests)
	if err != nil {
		return nil, err
	}

	projects := project.NewService(opts.deps)
	target, err := projects.CurrentTarget(ctx)
	if err != nil {
		return nil, err
	}
	deploysCode := hasFunctions || len(durable) > 0 || len(frontends) > 0
	if !opts.deps.LocalMode && deploysCode {
		mode, err := projects.SourceMode(ctx)
		if err != nil {
			return nil, err
		}
		if mode != apiclient.ProjectSourceExportStateModePlatform {
			return nil, fmt.Errorf("%s deploys its code from its connected Git repository (source mode %q), so functions and frontends can't be deployed from here.\n"+
				"Push to the repository to deploy code, and use %q and %q for variables and configuration",
				targetLabel(target), mode,
				cliruntime.CommandPath(opts.deps, "variables deploy"), cliruntime.CommandPath(opts.deps, "config deploy"))
		}
	}

	primaryPath := ""
	if len(manifests) > 0 {
		primaryPath = manifests[0].path
	}
	b := planBuilder{deps: opts.deps, manifests: manifests}
	return &plan{
		local:  opts.deps.LocalMode,
		target: target,
		steps: []step{
			b.variables(envFile),
			b.migrations(opts.withMigrations, database),
			b.functions(hasFunctions, primaryPath, len(durable) > 0),
			b.durable(durable, primaryPath),
			b.frontends(frontends, layoutFound, opts.noFrontends),
			b.config(),
		},
	}, nil
}

func (p *plan) print(w io.Writer) {
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

// The local development server's fixed keys, which the starters'
// volcano/volcano.env holds.
var localServerKeys = []string{
	"ak-0000000000000000000000000000000000000000",
	"sk-1111111111111111111111111111111111111111",
}

// refuseLocalValues keeps the local server's values out of a cloud project: its
// keys, or an API URL on this machine, which a cloud frontend would compile in.
func refuseLocalValues(path string) error {
	file, err := envfile.Load(path)
	if err != nil {
		return err
	}
	var local []string
	for name, value := range file.Variables {
		if slices.Contains(localServerKeys, value) || (strings.HasSuffix(name, "VOLCANO_API_URL") && isLoopbackURL(value)) {
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

func resolveEnvFile(flag string) (string, error) {
	path, err := envfile.ResolvePath(flag)
	if errors.Is(err, envfile.ErrNotFound) && flag == "" {
		return "", nil
	}
	return path, err
}

// checkVariableSources refuses an env file next to a manifest variables
// section. The section is fully synced, so the config step would delete every
// variable only the env file sets.
func checkVariableSources(envFile string, named bool, manifests []manifestFile) error {
	if envFile == "" {
		return nil
	}
	for _, m := range manifests {
		if !m.manifest.DeclaresVariables() {
			continue
		}
		fix := "rename " + envFile
		if named {
			fix = "leave out --env-file"
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

// migrationDatabase picks the database --with-migrations applies to. Local
// projects have one named app; a cloud project's comes from -d, or from the
// manifests when they declare exactly one.
func migrationDatabase(opts options, manifests []manifestFile) (string, error) {
	if !opts.withMigrations && opts.database != "" {
		return "", fmt.Errorf("--database only picks where migrations go; add --with-migrations to apply them to %q", opts.database)
	}
	if !opts.withMigrations || opts.database != "" {
		return opts.database, nil
	}
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

func (b planBuilder) migrations(enabled bool, database string) step {
	s := step{name: stepMigrations}
	if !enabled {
		s.skipped = true
		s.detail = "pass --with-migrations to apply " + migrationsDir
		return s
	}
	s.detail = migrationsDir + " -> database " + database
	if b.deps.LocalMode {
		s.manual = []string{b.command("migrations deploy --all -d " + database)}
	} else {
		s.manual = []string{b.command("databases migration up --all -d " + database)}
	}
	s.run = func(ctx context.Context, r *runner) error {
		return migrationcmd.ApplyAll(ctx, r.opts.deps, database, r.opts.out)
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
	s.manual = []string{b.command("functions deploy --all")}
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
	s.manual = []string{b.command("durable deploy --all")}
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

func (b planBuilder) frontends(frontends []clifrontend.LayoutFrontend, layoutFound, skip bool) step {
	s := step{name: stepFrontends}
	switch {
	case skip:
		s.skipped = true
		s.detail = "--no-frontends"
		return s
	case !layoutFound:
		s.skipped = true
		s.detail = "no " + clifrontend.SourceLayoutPath
		return s
	case len(frontends) == 0:
		s.skipped = true
		s.detail = clifrontend.SourceLayoutPath + " lists no frontends"
		return s
	}
	names := make([]string, 0, len(frontends))
	deploys := make([]frontendscmd.DeployOptions, 0, len(frontends))
	for _, fe := range frontends {
		names = append(names, fe.Name)
		opts := frontendscmd.DeployOptions{
			Name:    fe.Name,
			Path:    frontendPath(fe),
			AppRoot: fe.AppRoot,
		}
		opts.VariableScope, opts.Variables = b.frontendSelection(fe.Name)
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
	return s
}

// frontendSelection is the variable selection a frontend is built with, from
// its entry in the first manifest. A deploy can only set all or scoped; a
// shared frontend, or an entry that leaves either field out, keeps what the
// frontend has until the config step applies the entry.
func (b planBuilder) frontendSelection(name string) (string, []string) {
	if len(b.manifests) == 0 || b.manifests[0].manifest.Frontends == nil {
		return "", nil
	}
	for _, fe := range *b.manifests[0].manifest.Frontends {
		if fe.Name != name || fe.VariableScope == nil {
			continue
		}
		switch *fe.VariableScope {
		case "all":
			return "all", nil
		case "scoped":
			if fe.Variables == nil {
				return "", nil
			}
			return "scoped", *fe.Variables
		}
	}
	return "", nil
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
