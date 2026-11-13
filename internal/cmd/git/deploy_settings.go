package git

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/gitconnect"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// deploySetting is one deploy-settings subcommand: the setting it changes, how
// it changes it, and how the outcome reads.
type deploySetting struct {
	// subcommand is the command's own name, whose next step the output leaves
	// out.
	subcommand string
	change     func(context.Context, gitconnect.Service) (*apiclient.ProjectGitDeploySettings, error)
	render     func(io.Writer, output.GitBinding)
}

// runDeploySetting changes one deploy setting on a project with a repository
// connected, then says what a push to the production branch deploys.
func runDeploySetting(ctx context.Context, deps cliruntime.Deps, out io.Writer, setting deploySetting) error {
	service := gitconnect.NewService(deps)

	project, err := service.Project()
	if err != nil {
		return err
	}
	connection, err := connectedBinding(ctx, deps, service)
	if err != nil {
		return err
	}

	settings, err := setting.change(ctx, service)
	if err != nil {
		return guide(deps, err)
	}
	setting.render(out, output.GitBinding{
		Connection: connection,
		Settings:   settings,
		Project:    project.Label(),
		GitCommand: cliruntime.CommandPath(deps, "git"),
		Changed:    setting.subcommand,
	})
	return nil
}

// connectedBinding reads the project's binding and refuses a project with none.
// The deploy settings could be saved without one, but saving them makes them
// the project's own, so connecting afterwards would no longer turn on
// auto-deploy and functions for it: the setting would be saved and nothing
// would deploy.
func connectedBinding(
	ctx context.Context, deps cliruntime.Deps, service gitconnect.Service,
) (*apiclient.ProjectGitConnection, error) {
	connection, err := service.Status(ctx)
	if err == nil {
		return connection, nil
	}
	// "Connect one" is only advice where a repository can be connected.
	if errors.Is(err, gitconnect.ErrNotConnected) {
		if cannot := service.CanConnect(ctx); cannot != nil {
			err = cannot
		}
	}
	return nil, guide(deps, err)
}

// onOffArgument reads the one argument of a setting that is either on or off.
func onOffArgument(args []string, setting string) (bool, error) {
	if len(args) != 1 {
		return false, fmt.Errorf("say whether %s should be on or off, such as: on", setting)
	}
	switch args[0] {
	case "on":
		return true, nil
	case "off":
		return false, nil
	}
	return false, fmt.Errorf("%s is either on or off, not %q", setting, args[0])
}
