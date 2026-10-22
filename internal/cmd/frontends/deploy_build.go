package frontends

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/api"
	clifrontend "github.com/Kong/volcano-cli/internal/frontend"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// sourceFlags describe the source a deploy uploads, which --from replaces.
var sourceFlags = []string{"path", "name", "framework", "app-root", "variable-scope", "variable"}

// deployArgs checks whether --from was given rather than its value, so an
// empty --from (an unset shell variable) is refused instead of deploying the
// current directory.
func deployArgs(cmd *cobra.Command, deps cliruntime.Deps, from string, args []string) error {
	if !cmd.Flags().Changed("from") {
		if len(args) == 0 {
			return nil
		}
		if deps.LocalMode {
			return fmt.Errorf("unexpected argument %q: name the frontend with --name", args[0])
		}
		return fmt.Errorf("unexpected argument %q: name the frontend with --name to deploy source, or pass --from to deploy another frontend's build", args[0])
	}
	if deps.LocalMode {
		target := "<frontend>"
		if len(args) == 1 {
			target = args[0]
		}
		return fmt.Errorf("--from is a cloud option: local development builds each frontend from its own source, "+
			"so run '%s' in the frontend's directory, or 'volcano cloud frontends deploy %s --from %s' to deploy the build in the cloud project",
			cliruntime.CommandPath(deps, "frontends deploy --name "+target), target, from)
	}
	if strings.TrimSpace(from) == "" {
		return errors.New("--from needs the frontend whose build to deploy, as <frontend>[@<deployment-id>]")
	}
	if len(args) != 1 {
		return fmt.Errorf("--from needs exactly one frontend to deploy to, as in '%s'",
			cliruntime.CommandPath(deps, "frontends deploy web-eu --from "+from))
	}
	return nil
}

func runDeployBuild(cmd *cobra.Command, deps cliruntime.Deps, target, from string) error {
	var combined []string
	for _, name := range sourceFlags {
		if cmd.Flags().Changed(name) {
			combined = append(combined, "--"+name)
		}
	}
	if len(combined) > 0 {
		return fmt.Errorf("--from deploys an existing build and cannot be combined with %s", strings.Join(combined, ", "))
	}
	source, err := clifrontend.ParseBuildSource(from)
	if err != nil {
		return err
	}

	deployed, err := clifrontend.NewService(deps).DeployBuild(cmd.Context(), target, source)
	if api.Code(err) == "build_variables_differ" {
		return fmt.Errorf("%w\nRedeploy '%s' if those variables changed since its build, or give '%s' the same variable selection",
			err, source.Frontend, target)
	}
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	output.Success(out, "Frontend '%s' deployment started with the build of '%s'", deployed.Frontend.Name, deployed.Source.Name)
	fmt.Fprintf(out, "Source deployment: %s\n", deployed.SourceDeployment.Id)
	switch {
	case deployed.Frontend.PendingDeploymentId != nil:
		fmt.Fprintf(out, "Pending Deployment: %s (starts when the current deployment finishes)\n", deployed.Frontend.PendingDeploymentId)
	case deployed.Frontend.CurrentDeploymentId != nil:
		fmt.Fprintf(out, "Deployment: %s\n", deployed.Frontend.CurrentDeploymentId)
	}
	return nil
}
