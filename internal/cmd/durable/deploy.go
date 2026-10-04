package durable

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/archive"
	"github.com/Kong/volcano-cli/internal/cmd/cmdutil"
	clidurable "github.com/Kong/volcano-cli/internal/durable"
	clifunction "github.com/Kong/volcano-cli/internal/function"
	"github.com/Kong/volcano-cli/internal/output"
	"github.com/Kong/volcano-cli/internal/projectconfig"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type deployOptions struct {
	deps       cliruntime.Deps
	file       string
	all        bool
	visibility cmdutil.VisibilityFlags
	out        io.Writer
	errOut     io.Writer
}

func newDeploy(deps cliruntime.Deps) *cobra.Command {
	opts := deployOptions{}
	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy durable functions",
		Long: fmt.Sprintf(`Deploy durable functions from the volcano/functions directory.

Durable function sources live alongside standard ones; what makes a function
durable is its kind, which volcano-config.yaml declares and which cannot be
changed once the function exists:

  functions:
    - name: order-pipeline
      kind: durable

%s deploys every function the manifest declares durable. %s deploys
one by name or path, including one the manifest does not mention. A name the
manifest declares standard is refused: deploy that one with functions deploy.

Deploying over an existing durable function redeploys it. Executions already
running continue on the version they started on.

Visibility sets who can start executions:
  private        Service keys and schedulers only. New functions start here.
  authenticated  Also your project's signed-in users, including anonymous
                 sign-ins.
  public         Also anon keys with functions.invoke.
Callers a private function refuses get the same 404 as for a missing function.

To change a durable function's visibility without rebuilding it, declare
visibility under it in volcano-config.yaml and run %s. Redeploying with
--visibility also sets it, for the one function -f names; leaving the flag out
keeps what the function has.`,
			cliruntime.CommandPath(deps, "durable deploy --all"),
			cliruntime.CommandPath(deps, "durable deploy -f order-pipeline"),
			cliruntime.CommandPath(deps, "config deploy")),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.deps = deps
			opts.file = strings.TrimSpace(opts.file)
			opts.out = cmd.OutOrStdout()
			opts.errOut = cmd.ErrOrStderr()
			return runDeploy(cmd.Context(), &opts)
		},
	}
	cmd.Flags().StringVarP(&opts.file, "file", "f", "", "Deploy a specific durable function by name or path")
	cmd.Flags().BoolVarP(&opts.all, "all", "a", false, "Deploy every function volcano-config.yaml declares durable")
	opts.visibility.Register(cmd.Flags(),
		"Who can start executions: private, authenticated, or public (not valid with --all)")
	return cmd
}

func runDeploy(ctx context.Context, opts *deployOptions) error {
	visibility, err := deployVisibility(opts)
	if err != nil {
		return err
	}
	// Visibility is one value applied to every function the run deploys, and a
	// durable function has no update endpoint -- undoing a flip is a redeploy of
	// each one. So it is a per-function decision only, the way the flag help
	// describes it, and --all has to be told visibility per function through the
	// manifest rather than for all of them at once.
	if opts.all && opts.visibility.Set() {
		return errors.New("cannot use --visibility or --public with --all: deploy one function at a time to set its visibility")
	}
	targets, err := deployTargets(opts)
	if err != nil {
		return err
	}
	// The same manifest read the standard deploy does. Without it a durable
	// function the manifest scopes is created with every project variable, since
	// the API reads a declaration only from the deploy that sends one.
	manifest, err := projectconfig.ReadFunctionDeployManifest("")
	if err != nil {
		return err
	}
	for _, warning := range manifest.Deprecations {
		output.Warning(opts.errOut, "%s", warning)
	}

	service := clidurable.NewService(opts.deps)
	sources, baseDir, err := durableSources(ctx, opts, targets)
	if err != nil {
		return err
	}
	if opts.file != "" && manifest.StandardNames[sources[0].Name] {
		return fmt.Errorf("%q is declared a standard function in volcano-config.yaml; deploy it with %q",
			sources[0].Name, cliruntime.CommandPath(opts.deps, "functions deploy -f "+sources[0].Name))
	}

	var newPrivate, unreported []string
	for i, source := range sources {
		fmt.Fprintf(opts.out, "\n[%d/%d] Deploying %s...\n", i+1, len(sources), source.Name)
		deployed, created, err := deployOne(
			ctx, opts.out, service, baseDir, source, visibility, manifest.Declarations,
		)
		if err != nil {
			if visibility != nil && api.Status(err) == http.StatusBadRequest {
				return fmt.Errorf("%w\n%s", err, cmdutil.VisibilityLevelsHint(opts.deps))
			}
			return err
		}
		if deployed.Visibility == "" {
			if visibility != nil {
				return fmt.Errorf("%s: '%s' was deployed, but check who can start its executions with %s",
					cmdutil.VisibilityLevelsUnsupported, deployed.Name, cliruntime.CommandPath(opts.deps, "durable get "+deployed.Name))
			}
			unreported = append(unreported, deployed.Name)
		}
		// Only a new function the flag did not set: an existing one keeps the
		// level its owner chose, and --visibility private is a choice too.
		if created && visibility == nil && deployed.Visibility == apiclient.FunctionVisibilityPrivate {
			newPrivate = append(newPrivate, deployed.Name)
		}
	}
	fmt.Fprintln(opts.out)
	output.Success(opts.out, "%d/%d durable function(s) deployment started", len(sources), len(sources))
	fmt.Fprintf(opts.out, "Follow the rollout with %s\n", cliruntime.CommandPath(opts.deps, "durable get "+sources[0].Name))
	if len(unreported) > 0 {
		output.Warning(opts.errOut, "%s, so it reported no visibility for %s",
			cmdutil.VisibilityLevelsUnsupported, strings.Join(unreported, ", "))
	}
	cmdutil.PrivateHint{
		Summary:     "New durable functions are private: only service keys and schedulers can start executions.",
		Instruction: "Or redeploy with --visibility, which rebuilds the function:",
		Open: func(name string) string {
			return cliruntime.CommandPath(opts.deps, "durable deploy -f "+name+" --visibility authenticated")
		},
		ConfigDeploy: cliruntime.CommandPath(opts.deps, "config deploy"),
		ConfigFirst:  true,
		Declared:     manifest.Visibility,
	}.Print(opts.out, newPrivate)
	return nil
}

func deployOne(
	ctx context.Context,
	out io.Writer,
	service clidurable.Service,
	baseDir string,
	source clifunction.SourceInfo,
	visibility *apiclient.FunctionVisibility,
	declarations map[string]projectconfig.FunctionVariableDeclaration,
) (deployed *apiclient.DurableFunction, created bool, err error) {
	fmt.Fprintf(out, "  Runtime: %s\n", source.Runtime.Name)
	fmt.Fprintf(out, "  Function code: %s\n", source.Path)
	pkg, err := clifunction.PackageSource(source, baseDir)
	if err != nil {
		return nil, false, fmt.Errorf("failed to package durable function %s: %w", source.Name, err)
	}
	if declaration, ok := declarations[pkg.Name]; ok {
		pkg.VariableScope = declaration.VariableScope
		pkg.Variables = declaration.Variables
	}
	fmt.Fprintf(out, "  Archive size: %s\n", archive.FormatSize(pkg.Size))

	deployed, created, err = service.Deploy(ctx, *pkg, visibility)
	if err != nil {
		return nil, false, err
	}
	fmt.Fprintf(out, "  Deployed %s (%s)\n", deployed.Name, string(deployed.Status))
	if deployed.Visibility != "" {
		fmt.Fprintf(out, "  Visibility: %s\n", deployed.Visibility)
	}
	return deployed, created, nil
}

// deployVisibility turns the flags into the field the API takes, where nil
// keeps the deployed function's current visibility.
func deployVisibility(opts *deployOptions) (*apiclient.FunctionVisibility, error) {
	visibility, ok, err := opts.visibility.Visibility()
	if err != nil || !ok {
		return nil, err
	}
	return &visibility, nil
}

// deployTargets resolves which functions to deploy: the one named by --file, or
// every function the local manifest declares durable.
func deployTargets(opts *deployOptions) ([]string, error) {
	if opts.all && opts.file != "" {
		return nil, errors.New("cannot use --all and --file together")
	}
	if !opts.all && opts.file == "" {
		return nil, errors.New("specify either --all to deploy every durable function declared in volcano-config.yaml, or --file/-f to deploy a specific one")
	}
	if opts.file != "" {
		return []string{opts.file}, nil
	}

	manifestPath, err := projectconfig.ResolveManifestPath("")
	if errors.Is(err, projectconfig.ErrManifestNotFound) {
		// The shared message ends by offering --file to point at a manifest,
		// which is not what --file means here: on this command it names a
		// function source, and --all and --file exclude each other anyway.
		return nil, errors.New(
			"no volcano-config.yaml file found.\ncreate volcano/volcano-config.yaml or ./volcano-config.yaml, " +
				"or deploy one function with --file/-f <name|path>")
	}
	if err != nil {
		return nil, err
	}
	manifest, _, err := projectconfig.Load(manifestPath)
	if err != nil {
		return nil, err
	}
	names := manifest.DurableFunctionNames()
	if len(names) == 0 {
		return nil, errors.New("volcano-config.yaml declares no durable functions; add 'kind: durable' to a function entry, or deploy one with --file/-f")
	}
	return names, nil
}

// durableSources packages the scanned sources for the requested targets,
// keeping the target order so the progress lines match what the user asked for.
func durableSources(
	ctx context.Context,
	opts *deployOptions,
	targets []string,
) ([]clifunction.SourceInfo, string, error) {
	baseDir, err := os.Getwd()
	if err != nil {
		return nil, "", fmt.Errorf("failed to get current directory: %w", err)
	}

	runtimes, err := clifunction.NewService(opts.deps).ListRuntimes(ctx)
	if err != nil {
		return nil, "", err
	}
	fmt.Fprintln(opts.out, "\nScanning volcano/functions/...")
	scanned, err := clifunction.ScanSources(baseDir, clifunction.RuntimeCatalogFromOptions(runtimes))
	if err != nil {
		return nil, "", fmt.Errorf("failed to scan functions: %w", err)
	}

	sources := make([]clifunction.SourceInfo, 0, len(targets))
	for _, target := range targets {
		source, found := matchSource(scanned, target, baseDir)
		if !found {
			return nil, "", fmt.Errorf("function %q not found in volcano/functions/\navailable functions: %s",
				target, clifunction.FormatSourceNames(scanned))
		}
		source, err = durableRuntimeFor(source, runtimes)
		if err != nil {
			return nil, "", err
		}
		sources = append(sources, source)
	}
	return sources, baseDir, nil
}

// durableRuntimeFor settles which runtime a durable source deploys on, before
// anything is packaged or uploaded.
//
// Detection gives a source its language's default runtime, and a language's
// default is not always one that can run durable code: Python's durable support
// starts above the version a standard function gets. Picking the newest durable
// runtime for the same language is what a user would otherwise have to say by
// hand, and there is nowhere to say it — the durable deploy takes no runtime
// flag, and the manifest's runtime is not read on this path.
//
// A language with no durable runtime at all is still refused here rather than by
// the API, which only answers after the archive has been built and sent, and
// cannot name the file the runtime came from.
func durableRuntimeFor(
	source clifunction.SourceInfo,
	runtimes []apiclient.FunctionRuntimeOption,
) (clifunction.SourceInfo, error) {
	if source.Runtime.DurableCapable {
		return source, nil
	}
	if upgraded, ok := newestDurableRuntime(source.Runtime, runtimes); ok {
		source.Runtime = upgraded
		return source, nil
	}

	var capable []string
	for _, runtime := range runtimes {
		if runtime.DurableCapable {
			capable = append(capable, runtime.Name)
		}
	}
	return source, fmt.Errorf("durable functions cannot run on %s, which is the runtime for %s\ndurable runtimes: %s",
		source.Runtime.Name, source.Path, strings.Join(capable, ", "))
}

// newestDurableRuntime finds the highest durable-capable runtime for the same
// language as the detected one, matched on the source extensions they share so
// a Python file cannot be upgraded onto a Node runtime.
func newestDurableRuntime(
	detected apiclient.FunctionRuntimeOption,
	runtimes []apiclient.FunctionRuntimeOption,
) (apiclient.FunctionRuntimeOption, bool) {
	extensions := make(map[string]struct{}, len(detected.Deployment.FileExtensions))
	for _, extension := range detected.Deployment.FileExtensions {
		extensions[extension] = struct{}{}
	}

	var best apiclient.FunctionRuntimeOption
	found := false
	for _, runtime := range runtimes {
		if !runtime.DurableCapable || !sharesExtension(runtime, extensions) {
			continue
		}
		if !found || runtimeVersionLess(best.Name, runtime.Name) {
			best, found = runtime, true
		}
	}
	return best, found
}

func sharesExtension(runtime apiclient.FunctionRuntimeOption, extensions map[string]struct{}) bool {
	for _, extension := range runtime.Deployment.FileExtensions {
		if _, ok := extensions[extension]; ok {
			return true
		}
	}
	return false
}

// runtimeVersionLess orders two runtime names of the same language by version.
// Compared number by number rather than as text, because the names carry
// multi-digit versions: python3.9 sorts after python3.13 as a string.
func runtimeVersionLess(left, right string) bool {
	leftParts, rightParts := runtimeVersionParts(left), runtimeVersionParts(right)
	for i := 0; i < len(leftParts) && i < len(rightParts); i++ {
		if leftParts[i] != rightParts[i] {
			return leftParts[i] < rightParts[i]
		}
	}
	return len(leftParts) < len(rightParts)
}

func runtimeVersionParts(name string) []int {
	var parts []int
	for _, field := range strings.FieldsFunc(name, func(r rune) bool { return r < '0' || r > '9' }) {
		number, err := strconv.Atoi(field)
		if err != nil {
			continue
		}
		parts = append(parts, number)
	}
	return parts
}

func matchSource(sources []clifunction.SourceInfo, target, baseDir string) (clifunction.SourceInfo, bool) {
	for _, source := range sources {
		if clifunction.MatchesTarget(source, target, baseDir) {
			return source, true
		}
	}
	return clifunction.SourceInfo{}, false
}
