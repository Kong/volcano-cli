package durable

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/archive"
	clidurable "github.com/Kong/volcano-cli/internal/durable"
	clifunction "github.com/Kong/volcano-cli/internal/function"
	"github.com/Kong/volcano-cli/internal/output"
	"github.com/Kong/volcano-cli/internal/projectconfig"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type deployOptions struct {
	deps    cliruntime.Deps
	file    string
	all     bool
	public  bool
	private bool
	out     io.Writer
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
one by name or path, whether or not the manifest mentions it.

Deploying over an existing durable function redeploys it. Executions already
running continue on the version they started on.`,
			cliruntime.CommandPath(deps, "durable deploy --all"),
			cliruntime.CommandPath(deps, "durable deploy -f order-pipeline")),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.deps = deps
			opts.file = strings.TrimSpace(opts.file)
			opts.out = cmd.OutOrStdout()
			return runDeploy(cmd.Context(), opts)
		},
	}
	cmd.Flags().StringVarP(&opts.file, "file", "f", "", "Deploy a specific durable function by name or path")
	cmd.Flags().BoolVarP(&opts.all, "all", "a", false, "Deploy every function volcano-config.yaml declares durable")
	cmd.Flags().BoolVar(&opts.public, "public", false, "Let anon keys start executions of this function")
	cmd.Flags().BoolVar(&opts.private, "private", false, "Stop anon keys from starting executions of this function")
	return cmd
}

func runDeploy(ctx context.Context, opts deployOptions) error {
	if opts.public && opts.private {
		return errors.New("cannot use --public and --private together")
	}
	visibility := deployVisibility(opts)
	targets, err := deployTargets(opts)
	if err != nil {
		return err
	}

	service := clidurable.NewService(opts.deps)
	sources, baseDir, err := durableSources(ctx, opts, targets)
	if err != nil {
		return err
	}

	for i, source := range sources {
		fmt.Fprintf(opts.out, "\n[%d/%d] Deploying %s...\n", i+1, len(sources), source.Name)
		if err := deployOne(ctx, opts.out, service, baseDir, source, visibility); err != nil {
			return err
		}
	}
	fmt.Fprintln(opts.out)
	output.Success(opts.out, "%d/%d durable function(s) deployment started", len(sources), len(sources))
	fmt.Fprintf(opts.out, "Follow the rollout with %s\n", cliruntime.CommandPath(opts.deps, "durable get "+sources[0].Name))
	return nil
}

func deployOne(
	ctx context.Context,
	out io.Writer,
	service clidurable.Service,
	baseDir string,
	source clifunction.SourceInfo,
	visibility *bool,
) error {
	fmt.Fprintf(out, "  Runtime: %s\n", source.Runtime.Name)
	fmt.Fprintf(out, "  Function code: %s\n", source.Path)
	pkg, err := clifunction.PackageSource(source, baseDir)
	if err != nil {
		return fmt.Errorf("failed to package durable function %s: %w", source.Name, err)
	}
	fmt.Fprintf(out, "  Archive size: %s\n", archive.FormatSize(pkg.Size))

	deployed, err := service.Deploy(ctx, *pkg, visibility)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "  Deployed %s (%s)\n", deployed.Name, string(deployed.Status))
	return nil
}

// deployVisibility turns the two flags into the field the API takes, where an
// absent value keeps the deployed function's current visibility. A durable
// function has no update endpoint, so redeploying is the only way to change it.
func deployVisibility(opts deployOptions) *bool {
	switch {
	case opts.public:
		public := true
		return &public
	case opts.private:
		private := false
		return &private
	default:
		return nil
	}
}

// deployTargets resolves which functions to deploy: the one named by --file, or
// every function the local manifest declares durable.
func deployTargets(opts deployOptions) ([]string, error) {
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
	opts deployOptions,
	targets []string,
) ([]clifunction.SourceInfo, string, error) {
	baseDir, err := os.Getwd()
	if err != nil {
		return nil, "", fmt.Errorf("failed to get current directory: %w", err)
	}

	runtimeCatalog, err := clifunction.NewService(opts.deps).RuntimeCatalog(ctx)
	if err != nil {
		return nil, "", err
	}
	fmt.Fprintln(opts.out, "\nScanning volcano/functions/...")
	scanned, err := clifunction.ScanSources(baseDir, runtimeCatalog)
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
		sources = append(sources, source)
	}
	return sources, baseDir, nil
}

func matchSource(sources []clifunction.SourceInfo, target, baseDir string) (clifunction.SourceInfo, bool) {
	for _, source := range sources {
		if clifunction.MatchesTarget(source, target, baseDir) {
			return source, true
		}
	}
	return clifunction.SourceInfo{}, false
}
