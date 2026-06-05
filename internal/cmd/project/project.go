// Package project wires the volcano project subcommands.
package project

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/confirm"
	"github.com/Kong/volcano-cli/internal/output"
	cliproject "github.com/Kong/volcano-cli/internal/project"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type listOptions struct {
	deps  cliruntime.Deps
	page  int
	limit int
	out   io.Writer
}

type createOptions struct {
	deps cliruntime.Deps
	name string
	out  io.Writer
}

type getOptions struct {
	deps      cliruntime.Deps
	projectID string
	out       io.Writer
}

type deleteOptions struct {
	deps      cliruntime.Deps
	projectID string
	yes       bool
	in        io.Reader
	out       io.Writer
}

type useOptions struct {
	deps       cliruntime.Deps
	identifier string
	out        io.Writer
}

// NewProjects returns the projects management command.
func NewProjects(deps cliruntime.Deps) *cobra.Command {
	var page int
	var limit int
	cmd := &cobra.Command{
		Use:   "projects",
		Short: "Manage projects",
		Long:  "Create, list, delete, and select Volcano projects.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runList(cmd.Context(), listOptions{
				deps:  deps,
				page:  page,
				limit: limit,
				out:   cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().IntVar(&page, "page", api.DefaultPage, "Page number to fetch")
	cmd.Flags().IntVar(&limit, "limit", api.DefaultLimit, "Number of projects per page")
	cmd.AddCommand(newList(deps))
	cmd.AddCommand(newCreate(deps))
	cmd.AddCommand(newGet(deps))
	cmd.AddCommand(newDelete(deps))
	cmd.AddCommand(newUse(deps))
	return cmd
}

func newList(deps cliruntime.Deps) *cobra.Command {
	var page int
	var limit int
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List projects",
		Long:  "List all projects for the authenticated user.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runList(cmd.Context(), listOptions{
				deps:  deps,
				page:  page,
				limit: limit,
				out:   cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().IntVar(&page, "page", api.DefaultPage, "Page number to fetch")
	cmd.Flags().IntVar(&limit, "limit", api.DefaultLimit, "Number of projects per page")
	return cmd
}

func runList(ctx context.Context, opts listOptions) error {
	cfg, projects, err := cliproject.NewService(opts.deps).List(ctx, opts.page, opts.limit)
	if err != nil {
		return err
	}

	output.Projects(opts.out, cfg, projects)
	return nil
}

func newCreate(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "create <name>",
		Short: "Create a project",
		Long:  "Create a Volcano project for the authenticated user.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCreate(cmd.Context(), createOptions{
				deps: deps,
				name: strings.TrimSpace(args[0]),
				out:  cmd.OutOrStdout(),
			})
		},
	}
}

func runCreate(ctx context.Context, opts createOptions) error {
	project, err := cliproject.NewService(opts.deps).Create(ctx, opts.name)
	if err != nil {
		return err
	}

	output.Success(opts.out, "Project created: %s (%s)", project.Name, project.Id.String())
	return nil
}

func newGet(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "get [project-id]",
		Short: "Get project details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGet(cmd.Context(), getOptions{
				deps:      deps,
				projectID: strings.TrimSpace(args[0]),
				out:       cmd.OutOrStdout(),
			})
		},
	}
}

func runGet(ctx context.Context, opts getOptions) error {
	project, err := cliproject.NewService(opts.deps).Get(ctx, opts.projectID)
	if err != nil {
		return err
	}

	output.Project(opts.out, project)
	return nil
}

func newDelete(deps cliruntime.Deps) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete [project-id]",
		Short: "Delete a project",
		Long: `Start asynchronous deletion for a project. The project remains visible with status "deleting" until cleanup finishes, then it disappears.

By default this command prompts for confirmation.
Use --yes to skip the prompt.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDelete(cmd.Context(), deleteOptions{
				deps:      deps,
				projectID: strings.TrimSpace(args[0]),
				yes:       yes,
				in:        cmd.InOrStdin(),
				out:       cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}

func runDelete(ctx context.Context, opts deleteOptions) error {
	if !opts.yes {
		confirmed, err := confirm.Delete(opts.in, opts.out, "project", opts.projectID)
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	}

	if err := cliproject.NewService(opts.deps).Delete(ctx, opts.projectID); err != nil {
		return err
	}

	output.Success(opts.out, "Project deletion started: %s", opts.projectID)
	fmt.Fprintln(opts.out, "Status will be \"deleting\" until cleanup finishes; afterwards the project will no longer exist.")
	return nil
}

func newUse(deps cliruntime.Deps) *cobra.Command {
	cmd := NewUse(deps)
	cmd.Use = "use [project-name-or-id]"
	return cmd
}

// NewUse returns the current-project selection command.
func NewUse(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "use [project-name-or-id]",
		Short: "Set the active project",
		Long: `Set the active project for subsequent commands.

You can specify the project by name or ID.

Example:
  volcano use "My Project"
  volcano use eac37d5a-5f6f-42d8-acf6-0f2ae9c7a550`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUse(cmd.Context(), useOptions{
				deps:       deps,
				identifier: args[0],
				out:        cmd.OutOrStdout(),
			})
		},
	}
}

func runUse(ctx context.Context, opts useOptions) error {
	selected, err := cliproject.NewService(opts.deps).Use(ctx, opts.identifier)
	if err != nil {
		return err
	}

	output.Success(opts.out, "Now using project: %s (%s)", selected.Name, selected.Id.String())
	output.Success(opts.out, "Saved to ~/.volcano/config.json")
	return nil
}
