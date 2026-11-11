package cloud

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/config"
	cliproject "github.com/Kong/volcano-cli/internal/project"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// projectFlagName is the persistent `volcano cloud` flag that picks the
// project for one command.
const projectFlagName = "project"

const projectFlagUsage = "Project name or ID to run this command against, instead of the active project. " +
	"Does not change the active project"

// projectFlag is `volcano cloud --project`: the project one cloud command runs
// against. It outranks VOLCANO_PROJECT_ID, the directory's link, and the
// project `volcano use` saved, and changes none of them, so scripts and agents
// can address several projects in a row without touching shared state.
type projectFlag struct {
	identifier string
	// selected is the project identifier resolved to, set before the command
	// runs. Nil when the flag was not given.
	selected *config.ProjectConfig
}

// register adds --project to cmd's persistent flags and resolves it before any
// subcommand runs. Resolving up front reports a project that is not there the
// way `volcano use` does, before the command does anything, and lets a name
// stand for the project's ID in every API call.
//
// It sets cmd.PersistentPreRunE. Cobra runs only the nearest one, so a cloud
// subcommand must not set its own; TestCloudSubcommandsKeepProjectFlagHook
// guards that.
func (f *projectFlag) register(cmd *cobra.Command, deps cliruntime.Deps) {
	cmd.PersistentFlags().StringVar(&f.identifier, projectFlagName, "", projectFlagUsage)
	cmd.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		if !cmd.Flags().Changed(projectFlagName) {
			return nil
		}
		return f.resolve(cmd.Context(), deps)
	}
}

func (f *projectFlag) resolve(ctx context.Context, deps cliruntime.Deps) error {
	identifier := strings.TrimSpace(f.identifier)
	if identifier == "" {
		return fmt.Errorf("invalid argument %q for \"--%s\" flag: give a project name or ID", f.identifier, projectFlagName)
	}
	project, err := cliproject.NewService(deps).Resolve(ctx, identifier)
	if err != nil {
		return err
	}
	f.selected = &config.ProjectConfig{ID: project.Id.String(), Name: project.Name}
	return nil
}

// override is cliruntime.Deps.ProjectOverride for the cloud tree: a copy of the
// project --project chose, or nil.
func (f *projectFlag) override() *config.ProjectConfig {
	if f.selected == nil {
		return nil
	}
	selected := *f.selected
	return &selected
}
