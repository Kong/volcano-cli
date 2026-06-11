package frontends

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/archive"
	clifrontend "github.com/Kong/volcano-cli/internal/frontend"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const defaultFrontendFramework = "nextjs"

type deployOptions struct {
	deps      cliruntime.Deps
	path      string
	name      string
	framework string
	appRoot   string
	out       io.Writer
}

func newDeploy(deps cliruntime.Deps) *cobra.Command {
	opts := deployOptions{path: "."}
	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy a frontend",
		Long: `Deploy a frontend from a local project directory.

The CLI packages the directory into a tar.gz archive and uploads it to Volcano,
which builds and deploys the frontend. If the project participates in a JS
workspace (pnpm-workspace.yaml or package.json "workspaces") and declares
"workspace:" deps, packaging is promoted to the workspace root automatically.
Pass --app-root to opt out of auto-promotion and take explicit control of the
archive layout.

Usage:
  volcano cloud frontends deploy --name web --path ./apps/web
  volcano cloud frontends deploy --name web --path . --app-root apps/web`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			local := opts
			local.deps = deps
			local.out = cmd.OutOrStdout()
			return runDeploy(cmd.Context(), local)
		},
	}
	cmd.Flags().StringVar(&opts.path, "path", ".", "Path to the frontend project directory")
	cmd.Flags().StringVar(&opts.name, "name", "", "Frontend name (defaults to the lowercased directory name)")
	cmd.Flags().StringVar(&opts.framework, "framework", defaultFrontendFramework, "Frontend framework (nextjs)")
	cmd.Flags().StringVar(&opts.appRoot, "app-root", "", "Relative path inside the archive to the app to build (for monorepos)")
	return cmd
}

func runDeploy(ctx context.Context, opts deployOptions) error {
	rawPath := strings.TrimSpace(opts.path)
	if rawPath == "" {
		rawPath = "."
	}
	absPath, err := filepath.Abs(rawPath)
	if err != nil {
		return fmt.Errorf("failed to resolve frontend path: %w", err)
	}

	name := strings.TrimSpace(opts.name)
	if name == "" {
		name = strings.ToLower(filepath.Base(absPath))
	}
	if name == "" {
		return errors.New("frontend name cannot be empty")
	}

	framework := strings.TrimSpace(opts.framework)
	if framework == "" {
		framework = defaultFrontendFramework
	}
	if framework != defaultFrontendFramework {
		return fmt.Errorf("unsupported framework %q (only %s is currently supported)", framework, defaultFrontendFramework)
	}

	appRoot, err := clifrontend.NormalizeAppRoot(opts.appRoot)
	if err != nil {
		return err
	}

	if appRoot != "" {
		if err := clifrontend.ValidateAppRootExists(absPath, appRoot); err != nil {
			return err
		}
	}

	appCheckRoot := absPath
	if appRoot != "" {
		appCheckRoot = filepath.Join(absPath, filepath.FromSlash(appRoot))
	}
	if _, err := os.Stat(filepath.Join(appCheckRoot, "package.json")); err != nil {
		if os.IsNotExist(err) {
			if appRoot != "" {
				return fmt.Errorf("no package.json at --path/--app-root (%s); make sure --app-root points at the app directory", appCheckRoot)
			}
			return fmt.Errorf("no package.json at --path (%s); pass --app-root or run from the app directory", appCheckRoot)
		}
		return fmt.Errorf("failed to read package.json at %s: %w", appCheckRoot, err)
	}

	fmt.Fprintf(opts.out, "Packaging frontend directory: %s\n", absPath)
	pkg, err := clifrontend.PackageDirectory(absPath, clifrontend.PackageOptions{
		DisableWorkspacePromotion: appRoot != "",
		AppRoot:                   appRoot,
	})
	if err != nil {
		return err
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
		Name:      name,
		Framework: framework,
		AppRoot:   appRoot,
		Archive:   pkg.Archive,
	})
	if err != nil {
		return err
	}

	output.Success(opts.out, "Frontend '%s' deployment started", deployed.Name)
	return nil
}
