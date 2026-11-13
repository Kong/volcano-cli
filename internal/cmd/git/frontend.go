package git

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/gitconnect"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type frontendOptions struct {
	deps cliruntime.Deps
	// name is the frontend a push deploys, "" for none.
	name string
	// root is the directory the frontend builds from, relative to the
	// project's root directory, "" for that directory itself.
	root string
	out  io.Writer
}

func newFrontend(deps cliruntime.Deps) *cobra.Command {
	var (
		root string
		none bool
	)
	cmd := &cobra.Command{
		Use:   "frontend [name]",
		Short: "Choose the frontend a push to the connected repository deploys",
		Long: fmt.Sprintf(`Choose the frontend that a push to the connected repository's production
branch deploys, alongside its functions. Pass --none to stop deploying a
frontend on push.

Pass --root when the frontend builds from a subdirectory, such as apps/web.
The path is relative to the project's root directory, which is set with
'%s'.

The frontend need not exist yet: the next push deploys it under this name.

This works with a project access token, so CI or an agent can set it. The
project needs a connected repository first: see '%s'.

Nothing is redeployed. The command says what a push to the production branch
deploys from now on.`,
			cliruntime.CommandPath(deps, "git root-directory"),
			cliruntime.CommandPath(deps, "git connect")),
		Example: "  volcano git frontend web --root apps/web\n  volcano git frontend --none",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, frontendRoot, err := frontendArguments(args, root, cmd.Flags().Changed("root"), none)
			if err != nil {
				return err
			}
			return runFrontend(cmd.Context(), frontendOptions{
				deps: deps,
				name: name,
				root: frontendRoot,
				out:  cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().StringVar(&root, "root", "",
		"Directory the frontend builds from, relative to the project's root directory")
	cmd.Flags().BoolVar(&none, "none", false, "Deploy no frontend on push")
	return cmd
}

// frontendArguments is the frontend asked for and the directory it builds
// from. Deploying none takes --none rather than an empty name, which is what
// an unset variable in a script looks like.
func frontendArguments(args []string, root string, rootSet, none bool) (name, frontendRoot string, err error) {
	switch {
	case none && len(args) == 1:
		return "", "", errors.New("pass a frontend name or --none, not both")
	case none && rootSet:
		return "", "", errors.New("--root says where a frontend builds from, so it cannot be combined with --none")
	case none:
		return "", "", nil
	case len(args) == 0:
		return "", "", errors.New("name the frontend a push deploys, such as web, " +
			"or pass --none to deploy no frontend")
	}
	name = strings.TrimSpace(args[0])
	if name == "" {
		return "", "", errors.New("the frontend name is empty; pass --none to deploy no frontend")
	}
	frontendRoot = strings.TrimSpace(root)
	if err := validateRootDirectory("the frontend root", frontendRoot); err != nil {
		return "", "", err
	}
	return name, frontendRoot, nil
}

func runFrontend(ctx context.Context, opts frontendOptions) error {
	return runDeploySetting(ctx, opts.deps, opts.out, deploySetting{
		subcommand: "frontend",
		change: func(ctx context.Context, service gitconnect.Service) (*apiclient.ProjectGitDeploySettings, error) {
			return service.SetFrontend(ctx, opts.name, opts.root)
		},
		render: output.GitFrontendSet,
	})
}
