// Package context wires the volcano context command tree.
package context

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/config"
	"github.com/Kong/volcano-cli/internal/output"
)

type setOptions struct {
	name           string
	apiURL         string
	deviceClientID string
	out            io.Writer
}

// New returns the context command tree.
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "context",
		Short: "Manage Volcano API contexts",
		Long:  "Manage named Volcano API contexts used by login and top-level commands.",
	}
	cmd.AddCommand(newList())
	cmd.AddCommand(newUse())
	cmd.AddCommand(newSet())
	cmd.AddCommand(newDelete())
	return cmd
}

func newList() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List contexts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			printContexts(cmd.OutOrStdout(), cfg)
			return nil
		},
	}
}

func newUse() *cobra.Command {
	return &cobra.Command{
		Use:   "use <name>",
		Short: "Set the default context",
		Long:  "Set the context used by default when --context and VOLCANO_CONTEXT are not set.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			name := config.NormalizeContextName(args[0])
			cfg.SetDefaultContext(name)
			if err := cfg.Save(); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}
			output.Success(cmd.OutOrStdout(), "Now using context: %s", cfg.DefaultContext)
			return nil
		},
	}
}

func newSet() *cobra.Command {
	var opts setOptions
	cmd := &cobra.Command{
		Use:   "set <name>",
		Short: "Create or update a context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.name = args[0]
			opts.out = cmd.OutOrStdout()
			return runSet(opts)
		},
	}
	cmd.Flags().StringVar(&opts.apiURL, "api-url", "", "API URL for the context")
	cmd.Flags().StringVar(&opts.deviceClientID, "device-client-id", "", "OAuth device client ID for browser login")
	return cmd
}

func runSet(opts setOptions) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	name := config.NormalizeContextName(opts.name)
	if name == "" {
		return fmt.Errorf("context name cannot be empty")
	}
	ctx := cfg.EnsureContext(name)
	if apiURL := strings.TrimSpace(opts.apiURL); apiURL != "" {
		ctx.APIBaseURL = apiURL
	}
	if clientID := strings.TrimSpace(opts.deviceClientID); clientID != "" {
		ctx.DeviceClientID = clientID
	}
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	output.Success(opts.out, "Context saved: %s", name)
	return nil
}

func newDelete() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a custom context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := config.NormalizeContextName(args[0])
			if config.IsBuiltInContext(name) {
				return fmt.Errorf("cannot delete built-in context %q", name)
			}
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			cfg.DeleteContext(name)
			if err := cfg.Save(); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}
			output.Success(cmd.OutOrStdout(), "Context deleted: %s", name)
			return nil
		},
	}
}

func printContexts(w io.Writer, cfg *config.Config) {
	names := map[string]struct{}{}
	for _, name := range config.BuiltInContextNames() {
		names[name] = struct{}{}
	}
	for name := range cfg.Contexts {
		names[config.NormalizeContextName(name)] = struct{}{}
	}
	sorted := make([]string, 0, len(names))
	for name := range names {
		if name != "" {
			sorted = append(sorted, name)
		}
	}
	sort.Strings(sorted)

	active := cfg.ActiveContextName()
	fmt.Fprintf(w, "%-8s  %-7s  %s\n", "CURRENT", "NAME", "API URL")
	fmt.Fprintln(w, strings.Repeat("-", 72))
	for _, name := range sorted {
		marker := ""
		if name == active {
			marker = "*"
		}
		resolved := cfg.ResolvedContext(name)
		fmt.Fprintf(w, "%-8s  %-7s  %s\n", marker, name, resolved.APIBaseURL)
	}
}
