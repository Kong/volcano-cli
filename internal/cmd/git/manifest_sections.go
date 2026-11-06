package git

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/gitconnect"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type manifestSectionsOptions struct {
	deps cliruntime.Deps
	// sections is every section a push applies from now on, empty for none.
	sections   []string
	yes        bool
	jsonOutput bool
	in         io.Reader
	out        io.Writer
	errOut     io.Writer
}

func newManifestSections(deps cliruntime.Deps) *cobra.Command {
	opts := manifestSectionsOptions{deps: deps}
	cmd := &cobra.Command{
		Use:   "manifest-sections",
		Short: "Show which volcano-config.yaml sections a push applies",
		Long: fmt.Sprintf(`Show which volcano-config.yaml sections a push to the connected repository's
production branch applies once its deploys succeed. A project applies none
until a person chooses some with '%s'.

Inside a chosen section a push only adds: it never deletes, replaces, or widens
exposure. Those changes are left for '%s'.

This works with a project access token.`,
			cliruntime.CommandPath(deps, "git manifest-sections set"),
			cliruntime.CommandPath(deps, "cloud config deploy")),
		Example: "  volcano git manifest-sections\n  volcano git manifest-sections --json",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.out = cmd.OutOrStdout()
			return runManifestSections(cmd.Context(), opts)
		},
	}
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit the project's Git deploy settings as machine-readable JSON")
	cmd.AddCommand(newManifestSectionsSet(deps))
	return cmd
}

func newManifestSectionsSet(deps cliruntime.Deps) *cobra.Command {
	opts := manifestSectionsOptions{deps: deps}
	var none bool
	cmd := &cobra.Command{
		Use:   "set [section...]",
		Short: "Choose which volcano-config.yaml sections a push applies",
		Long: fmt.Sprintf(`Choose every volcano-config.yaml section a push to the production branch
applies once its deploys succeed, replacing the current choice. Pass --none to
apply none. The section a push can apply today is frontends.

Choosing a section lets anyone who can push to the production branch change
it, so this asks first unless you pass --yes. Inside the section a push only
adds: it never deletes, replaces, or widens exposure. A push that sets a secret
or leaves a ${...} unresolved in a chosen section fails: it deploys nothing and
applies none of its config.

This is a person's decision: sign in with '%s'. A project access token is
refused. Nothing is applied now; the next push applies the sections.`,
			cliruntime.CommandPath(deps, "login")),
		Example: "  volcano git manifest-sections set frontends\n  volcano git manifest-sections set --none",
		Args:    cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sections, err := manifestSectionsArguments(args, none)
			if err != nil {
				return err
			}
			opts.sections = sections
			opts.in, opts.out, opts.errOut = cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr()
			return runManifestSectionsSet(cmd.Context(), opts)
		},
	}
	cmd.Flags().BoolVar(&none, "none", false, "Apply no volcano-config.yaml sections on push")
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit the project's Git deploy settings as machine-readable JSON")
	return cmd
}

// manifestSectionsArguments is the sections asked for. Choosing none takes
// --none rather than no arguments, which is what an unset variable in a script
// looks like. Which sections exist is the API's to say.
func manifestSectionsArguments(args []string, none bool) ([]string, error) {
	switch {
	case none && len(args) > 0:
		return nil, errors.New("pass sections or --none, not both")
	case none:
		return []string{}, nil
	case len(args) == 0:
		return nil, errors.New("name the sections a push applies, such as frontends, or pass --none to apply none")
	}
	sections := make([]string, 0, len(args))
	for _, arg := range args {
		section := strings.TrimSpace(arg)
		if section == "" {
			return nil, errors.New("a section is empty; pass --none to apply none")
		}
		sections = append(sections, section)
	}
	return sections, nil
}

func runManifestSections(ctx context.Context, opts manifestSectionsOptions) error {
	service := gitconnect.NewService(opts.deps)

	project, err := service.Project()
	if err != nil {
		return err
	}
	connection, err := manifestBinding(ctx, opts.deps, service)
	if err != nil {
		return err
	}
	settings, err := manifestSettings(ctx, opts.deps, service)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return output.JSON(opts.out, settings)
	}
	output.GitManifestSections(opts.out, manifestReport(opts.deps, project, connection, settings))
	return nil
}

func runManifestSectionsSet(ctx context.Context, opts manifestSectionsOptions) error {
	service := gitconnect.NewService(opts.deps)

	project, err := service.Project()
	if err != nil {
		return err
	}
	// Said before anything is read or asked: no answer changes it.
	if err := service.RequirePerson(); err != nil {
		return err
	}
	connection, err := manifestBinding(ctx, opts.deps, service)
	if err != nil {
		return err
	}
	current, err := manifestSettings(ctx, opts.deps, service)
	if err != nil {
		return err
	}

	// Only a section being added widens what a push can change. Removing one
	// takes nothing from anyone, so it is not asked about.
	if added := addedSections(current.ManifestSections, opts.sections); len(added) > 0 {
		confirmed, err := ask(opts.in, promptWriter(opts.jsonOutput, opts.out, opts.errOut), opts.yes,
			manifestSectionsWarning(connection, added),
			fmt.Sprintf("Apply %s on push?", strings.Join(added, ", ")))
		if err != nil || !confirmed {
			return err
		}
	}

	settings, err := service.SetManifestSections(ctx, opts.sections)
	if err != nil {
		return guide(opts.deps, err)
	}
	if opts.jsonOutput {
		return output.JSON(opts.out, settings)
	}
	output.GitManifestSectionsSet(opts.out, manifestReport(opts.deps, project, connection, settings))
	return nil
}

// manifestBinding reads the project's binding, nil when nothing is connected.
// The sections can be chosen before a repository is, but not where none can
// be connected at all, such as local mode: no push arrives there to apply
// them.
func manifestBinding(
	ctx context.Context, deps cliruntime.Deps, service gitconnect.Service,
) (*apiclient.ProjectGitConnection, error) {
	connection, err := service.Status(ctx)
	switch {
	case errors.Is(err, gitconnect.ErrNotConnected):
		if cannot := service.CanConnect(ctx); cannot != nil {
			return nil, guide(deps, cannot)
		}
	case err != nil:
		return nil, guide(deps, err)
	}
	// Nil when nothing is connected, which is a state here rather than a
	// failure.
	return connection, nil
}

// manifestSettings reads the deploy settings, refusing an API that leaves the
// sections out: it predates applying volcano-config.yaml on push, so "none"
// would be a guess.
func manifestSettings(
	ctx context.Context, deps cliruntime.Deps, service gitconnect.Service,
) (*apiclient.ProjectGitDeploySettings, error) {
	settings, err := service.DeploySettings(ctx)
	if err != nil {
		return nil, guide(deps, err)
	}
	if settings.ManifestSections == nil {
		return nil, gitconnect.ErrManifestSectionsUnavailable
	}
	return settings, nil
}

func manifestReport(
	deps cliruntime.Deps, project gitconnect.ProjectRef,
	connection *apiclient.ProjectGitConnection, settings *apiclient.ProjectGitDeploySettings,
) output.GitManifest {
	return output.GitManifest{
		Connection:          connection,
		Settings:            settings,
		Project:             project.Label(),
		GitCommand:          cliruntime.CommandPath(deps, "git"),
		ConfigDeployCommand: cliruntime.CommandPath(deps, "cloud config deploy"),
	}
}

// addedSections are the sections asked for that a push does not apply yet.
func addedSections(current []apiclient.ProjectGitManifestSection, asked []string) []string {
	var added []string
	for _, section := range asked {
		if !slices.Contains(current, apiclient.ProjectGitManifestSection(section)) && !slices.Contains(added, section) {
			added = append(added, section)
		}
	}
	return added
}

// manifestSectionsWarning says who gains control of the sections being added.
func manifestSectionsWarning(connection *apiclient.ProjectGitConnection, added []string) string {
	pushers := "anyone who can push to the production branch of the repository you connect"
	if connection != nil {
		pushers = fmt.Sprintf("anyone who can push to %s on %s", connection.ProductionBranch, connection.RepoFullName)
	}
	return fmt.Sprintf("This lets %s change the project's %s through volcano-config.yaml. "+
		"A push only adds: it never deletes, replaces, or widens exposure.", pushers, strings.Join(added, ", "))
}
