package project

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/output"
	cliproject "github.com/Kong/volcano-cli/internal/project"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// maxServiceKeyListLimit matches the API's ceiling on a service-key page size.
const maxServiceKeyListLimit = 100

// validateServiceKeyListWindow rejects a page or page size outside what the
// contract allows, so a typo fails naming the flag the user typed rather than
// as whatever the API makes of it. Mirrors accesstoken.ValidateListWindow.
func validateServiceKeyListWindow(page, limit int) error {
	if page < 1 {
		return fmt.Errorf("invalid --page %d: expected 1 or more", page)
	}
	if limit < 1 || limit > maxServiceKeyListLimit {
		return fmt.Errorf("invalid --limit %d: expected 1 to %d", limit, maxServiceKeyListLimit)
	}
	return nil
}

type serviceKeyListOptions struct {
	deps      cliruntime.Deps
	projectID string
	page      int
	limit     int
	out       io.Writer
}

type serviceKeyCreateOptions struct {
	deps        cliruntime.Deps
	projectID   string
	name        string
	permissions []string
	out         io.Writer
}

type serviceKeyGetOptions struct {
	deps      cliruntime.Deps
	projectID string
	keyID     string
	out       io.Writer
}

func newServiceKeys(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "service-keys",
		Aliases: []string{"service-key"},
		Short:   "Manage project service keys",
		Long:    "Create and inspect backend service keys for a Volcano project. Service keys bypass row-level security and must never be exposed in frontend code.",
		Args:    cobra.NoArgs,
	}
	cmd.AddCommand(newServiceKeyList(deps))
	cmd.AddCommand(newServiceKeyCreate(deps))
	cmd.AddCommand(newServiceKeyGet(deps))
	return cmd
}

func newServiceKeyList(deps cliruntime.Deps) *cobra.Command {
	var page int
	var limit int
	cmd := &cobra.Command{
		Use:   "list [project-id]",
		Short: "List project service keys",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var projectID string
			if len(args) == 1 {
				projectID = strings.TrimSpace(args[0])
			}
			return runServiceKeyList(cmd.Context(), serviceKeyListOptions{
				deps:      deps,
				projectID: projectID,
				page:      page,
				limit:     limit,
				out:       cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().IntVar(&page, "page", api.DefaultPage, "Page number to fetch")
	cmd.Flags().IntVar(&limit, "limit", api.DefaultLimit, "Number of service keys per page")
	return cmd
}

func runServiceKeyList(ctx context.Context, opts serviceKeyListOptions) error {
	if err := validateServiceKeyListWindow(opts.page, opts.limit); err != nil {
		return err
	}

	page, err := cliproject.NewService(opts.deps).ListServiceKeys(ctx, opts.projectID, opts.page, opts.limit)
	if err != nil {
		return err
	}
	output.ServiceKeys(opts.out, page, opts.projectID, cliruntime.CommandPath(opts.deps, ""))
	return nil
}

func newServiceKeyCreate(deps cliruntime.Deps) *cobra.Command {
	var permissions []string
	var permissionAliases []string
	cmd := &cobra.Command{
		Use:   "create <name> [project-id]",
		Short: "Create a project service key",
		Long:  "Create a backend service key. If no permissions are given, the server's full-access default is preserved.",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			projectID := ""
			if len(args) == 2 {
				projectID = strings.TrimSpace(args[1])
			}
			if cmd.Flags().Changed("permission") && len(permissions) == 0 ||
				cmd.Flags().Changed("permissions") && len(permissionAliases) == 0 {
				return errors.New("service key permission cannot be empty")
			}
			allPermissions := append([]string(nil), permissions...)
			allPermissions = append(allPermissions, permissionAliases...)
			return runServiceKeyCreate(cmd.Context(), serviceKeyCreateOptions{
				deps:        deps,
				projectID:   projectID,
				name:        strings.TrimSpace(args[0]),
				permissions: allPermissions,
				out:         cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().StringSliceVar(&permissions, "permission", nil, "Permission to grant (repeatable; omitted means full access)")
	cmd.Flags().StringSliceVar(&permissionAliases, "permissions", nil, "Alias for --permission")
	return cmd
}

func runServiceKeyCreate(ctx context.Context, opts serviceKeyCreateOptions) error {
	if strings.TrimSpace(opts.name) == "" {
		return errors.New("service key name cannot be empty")
	}
	for _, permission := range opts.permissions {
		if strings.TrimSpace(permission) == "" {
			return errors.New("service key permission cannot be empty")
		}
	}
	key, err := cliproject.NewService(opts.deps).CreateServiceKey(ctx, opts.projectID, opts.name, opts.permissions)
	if err != nil {
		return err
	}
	output.ServiceKey(opts.out, key)
	return nil
}

func newServiceKeyGet(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "get <key-id> [project-id]",
		Short: "Get a project service key",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			projectID := ""
			if len(args) == 2 {
				projectID = strings.TrimSpace(args[1])
			}
			return runServiceKeyGet(cmd.Context(), serviceKeyGetOptions{
				deps:      deps,
				projectID: projectID,
				keyID:     strings.TrimSpace(args[0]),
				out:       cmd.OutOrStdout(),
			})
		},
	}
}

func runServiceKeyGet(ctx context.Context, opts serviceKeyGetOptions) error {
	key, err := cliproject.NewService(opts.deps).GetServiceKey(ctx, opts.projectID, opts.keyID)
	if err != nil {
		return err
	}
	output.ServiceKey(opts.out, key)
	return nil
}
