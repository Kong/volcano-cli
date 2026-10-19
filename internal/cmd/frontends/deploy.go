package frontends

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/archive"
	clifrontend "github.com/Kong/volcano-cli/internal/frontend"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const defaultFrontendFramework = "nextjs"

type deployOptions struct {
	deps          cliruntime.Deps
	path          string
	name          string
	framework     string
	appRoot       string
	variableScope string
	variables     []string
	excludeFiles  []string
	from          string
	out           io.Writer
}

func newDeploy(deps cliruntime.Deps) *cobra.Command {
	opts := deployOptions{path: "."}
	cmd := &cobra.Command{
		Use:   "deploy [<frontend> --from <frontend>[@<deployment-id>]]",
		Short: "Deploy a frontend",
		Long: fmt.Sprintf(`Deploy a frontend from a local project directory.

The CLI packages the directory into a tar.gz archive and uploads it to Volcano,
which builds and deploys the frontend. If the project participates in a JS
workspace (pnpm-workspace.yaml or package.json "workspaces") and declares
"workspace:" deps, packaging is promoted to the workspace root automatically.
Pass --app-root to opt out of auto-promotion and take explicit control of the
archive layout.

With --from, deploy the build another frontend of the project already serves
to an existing frontend, without uploading or compiling again. Name a
deployment after @ to use that one instead of the one it serves. Both
frontends must build with the same variable values.

Usage:
  %s
  %s
  %s`,
			cliruntime.CommandPath(deps, "frontends deploy --name web --path ./apps/web"),
			cliruntime.CommandPath(deps, "frontends deploy --name web --path . --app-root apps/web"),
			cliruntime.CommandPath(deps, "frontends deploy web-eu --from web")),
		Args: func(_ *cobra.Command, args []string) error {
			return deployArgs(deps, opts.from, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.from != "" {
				return runDeployBuild(cmd, deps, args[0], opts.from)
			}
			local := opts
			local.deps = deps
			local.out = cmd.OutOrStdout()
			// "volcano frontends deploy" once deployed to the cloud; scripts
			// written then are told where this one went.
			if deps.LocalMode {
				output.Note(cmd.ErrOrStderr(), "Deploying to local development. To deploy to the cloud project, run 'volcano cloud frontends deploy'.")
			}
			return runDeploy(cmd.Context(), local)
		},
	}
	cmd.Flags().StringVar(&opts.path, "path", ".", "Path to the frontend project directory")
	cmd.Flags().StringVar(&opts.name, "name", "", "Frontend name (defaults to the lowercased directory name)")
	cmd.Flags().StringVar(&opts.framework, "framework", defaultFrontendFramework, "Frontend framework (nextjs)")
	cmd.Flags().StringVar(&opts.appRoot, "app-root", "", "Relative path inside the archive to the app to build (for monorepos)")
	cmd.Flags().StringVar(&opts.variableScope, "variable-scope", "", "Project variable selection: all or scoped")
	cmd.Flags().StringArrayVar(&opts.variables, "variable", nil, "Project variable name to include when --variable-scope is scoped (repeatable)")
	cmd.Flags().StringVar(&opts.from, "from", "", "Deploy the build of another frontend, as <frontend>[@<deployment-id>], instead of uploading source")
	return cmd
}

// DeployOptions configures Deploy. The fields up to Variables mirror the flags
// of "frontends deploy".
type DeployOptions struct {
	Path          string
	Name          string
	AppRoot       string
	VariableScope string
	Variables     []string
	// ExcludeFiles are files to leave out of the upload, such as an env file
	// the caller deployed from inside the frontend's directory.
	ExcludeFiles []string
	// Out is required.
	Out io.Writer
}

// Deploy packages and uploads one frontend, as "frontends deploy" does, and
// returns the frontend whose deployment started.
func Deploy(ctx context.Context, deps cliruntime.Deps, opts DeployOptions) (*apiclient.Frontend, error) {
	return deploy(ctx, deployOptions{
		deps:          deps,
		path:          opts.Path,
		name:          opts.Name,
		appRoot:       opts.AppRoot,
		variableScope: opts.VariableScope,
		variables:     opts.Variables,
		excludeFiles:  opts.ExcludeFiles,
		out:           opts.Out,
	})
}

func runDeploy(ctx context.Context, opts deployOptions) error {
	deployed, err := deploy(ctx, opts)
	if err != nil {
		return err
	}
	if opts.deps.LocalMode {
		printLocalSiteURL(opts, deployed)
	}
	return nil
}

// printLocalSiteURL names where the local frontend serves. A frontend's URL
// exists only once its first build finishes, so a new one points at "get".
func printLocalSiteURL(opts deployOptions, deployed *apiclient.Frontend) {
	if deployed.SiteUrl != nil && *deployed.SiteUrl != "" {
		fmt.Fprintf(opts.out, "Site URL: %s (serves the new build once it is active)\n", *deployed.SiteUrl)
		return
	}
	fmt.Fprintf(opts.out, "Run '%s' for the site URL once the build finishes.\n",
		cliruntime.CommandPath(opts.deps, "frontends get "+deployed.Name))
}

func deploy(ctx context.Context, opts deployOptions) (*apiclient.Frontend, error) {
	rawPath := strings.TrimSpace(opts.path)
	if rawPath == "" {
		rawPath = "."
	}
	absPath, err := filepath.Abs(rawPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve frontend path: %w", err)
	}

	name := strings.TrimSpace(opts.name)
	if name == "" {
		name = strings.ToLower(filepath.Base(absPath))
	}
	if name == "" {
		return nil, errors.New("frontend name cannot be empty")
	}

	framework := strings.TrimSpace(opts.framework)
	if framework == "" {
		framework = defaultFrontendFramework
	}
	if framework != defaultFrontendFramework {
		return nil, fmt.Errorf("unsupported framework %q (only %s is currently supported)", framework, defaultFrontendFramework)
	}

	variableScope := strings.TrimSpace(opts.variableScope)
	if variableScope != "" && variableScope != "all" && variableScope != "scoped" {
		return nil, errors.New("--variable-scope must be all or scoped")
	}
	if len(opts.variables) > 0 && variableScope != "scoped" {
		return nil, errors.New("--variable requires --variable-scope scoped")
	}

	appRoot, err := clifrontend.NormalizeAppRoot(opts.appRoot)
	if err != nil {
		return nil, err
	}

	if appRoot != "" {
		if err := clifrontend.ValidateAppRootExists(absPath, appRoot); err != nil {
			return nil, err
		}
	}

	appCheckRoot := absPath
	if appRoot != "" {
		appCheckRoot = filepath.Join(absPath, filepath.FromSlash(appRoot))
	}
	found, err := clifrontend.HasBuildPackage(absPath, appCheckRoot)
	if err != nil {
		return nil, err
	}
	if !found {
		if appRoot != "" {
			return nil, fmt.Errorf("no package.json at --path/--app-root (%s); make sure --app-root points at the app directory", appCheckRoot)
		}
		return nil, fmt.Errorf("no package.json at --path (%s); pass --app-root or run from the app directory", appCheckRoot)
	}

	fmt.Fprintf(opts.out, "Packaging frontend directory: %s\n", absPath)
	pkg, err := clifrontend.PackageDirectory(absPath, clifrontend.PackageOptions{
		DisableWorkspacePromotion: appRoot != "",
		AppRoot:                   appRoot,
		ExcludeFiles:              opts.excludeFiles,
	})
	if err != nil {
		return nil, err
	}
	if pkg.PackagingRoot != absPath {
		fmt.Fprintf(opts.out, "Packaging workspace root: %s\n", pkg.PackagingRoot)
		if pkg.AppPath != "" {
			fmt.Fprintf(opts.out, "App path: %s\n", pkg.AppPath)
		}
	}
	for _, link := range pkg.SkippedSymlinks {
		fmt.Fprintf(opts.out, "Warning: skipped symlink %s (symlinks are not archived)\n", link)
	}
	fmt.Fprintf(opts.out, "Archive size: %s\n", archive.FormatSize(pkg.Size))
	fmt.Fprintln(opts.out, "Uploading archive...")

	deployed, err := clifrontend.NewService(opts.deps).Deploy(ctx, api.FrontendDeployInput{
		Name:          name,
		Framework:     framework,
		AppRoot:       appRoot,
		VariableScope: variableScope,
		Variables:     opts.variables,
		Archive:       pkg.Archive,
	})
	if err != nil {
		return nil, err
	}

	output.Success(opts.out, "Frontend '%s' deployment started", deployed.Name)
	return deployed, nil
}
