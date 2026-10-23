package git

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/gitconnect"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type rootDirectoryOptions struct {
	deps cliruntime.Deps
	// rootDirectory is the directory to build from, "" for the repository
	// root.
	rootDirectory string
	out           io.Writer
}

func newRootDirectory(deps cliruntime.Deps) *cobra.Command {
	var reset bool
	cmd := &cobra.Command{
		Use:   "root-directory [path]",
		Short: "Change the subdirectory the connected repository builds from",
		Long: fmt.Sprintf(`Change the subdirectory of the connected repository that the project builds
from. The repository stays connected as it is.

The path is relative to the repository root, such as apps/web. Pass --reset to
build from the repository root again.

This works with a project access token, so CI or an agent can move a project
within its repository. Connecting a different repository needs a person's
account token: see '%s'.

Nothing is redeployed. The next push to the production branch builds from the
new directory.`, cliruntime.CommandPath(deps, "git connect")),
		Example: "  volcano git root-directory apps/web\n  volcano git root-directory --reset",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rootDirectory, err := rootDirectoryArgument(args, reset)
			if err != nil {
				return err
			}
			return runRootDirectory(cmd.Context(), rootDirectoryOptions{
				deps:          deps,
				rootDirectory: rootDirectory,
				out:           cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().BoolVar(&reset, "reset", false, "Build from the repository root")
	return cmd
}

// rootDirectoryArgument is the directory asked for. Resetting to the
// repository root takes --reset rather than an empty argument, which is what an
// unset variable in a script looks like.
func rootDirectoryArgument(args []string, reset bool) (string, error) {
	switch {
	case reset && len(args) == 1:
		return "", errors.New("pass a directory or --reset, not both")
	case reset:
		return "", nil
	case len(args) == 0:
		return "", errors.New("name the directory the project builds from, such as apps/web, " +
			"or pass --reset to build from the repository root")
	}
	rootDirectory := strings.TrimSpace(args[0])
	if rootDirectory == "" {
		return "", errors.New("the directory is empty; pass --reset to build from the repository root")
	}
	if err := validateRootDirectory("the root directory", rootDirectory); err != nil {
		return "", err
	}
	return rootDirectory, nil
}

func runRootDirectory(ctx context.Context, opts rootDirectoryOptions) error {
	service := gitconnect.NewService(opts.deps)
	webURL, _ := service.WebURL()

	project, err := service.Project()
	if err != nil {
		return err
	}

	// Nothing is read first: the write names only the root directory, so it
	// cannot replace a repository someone else connected meanwhile, and it
	// changes nothing when the directory is already the one asked for.
	connection, err := service.SetRootDirectory(ctx, opts.rootDirectory, nil)
	if err != nil {
		// "Connect one" is only advice where a repository can be connected.
		if errors.Is(err, gitconnect.ErrNotConnected) {
			if cannot := service.CanConnect(ctx); cannot != nil {
				err = cannot
			}
		}
		return guide(opts.deps, webURL, err)
	}

	output.GitRootDirectorySet(opts.out, output.GitBinding{Connection: connection, Project: project.Label()})
	return nil
}
