package policy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/confirm"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clistorage "github.com/Kong/volcano-cli/internal/storage"
)

type deleteOptions struct {
	deps       cliruntime.Deps
	bucket     string
	identifier string
	yes        bool
	in         io.Reader
	out        io.Writer
}

func newDelete(deps cliruntime.Deps) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <bucket> <name-or-id>",
		Short: "Delete a policy from a bucket",
		Long: `Delete a policy by name or UUID from a bucket.

By default this command prompts for confirmation.
Use --yes to skip the prompt.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDelete(cmd.Context(), deleteOptions{
				deps:       deps,
				bucket:     strings.TrimSpace(args[0]),
				identifier: strings.TrimSpace(args[1]),
				yes:        yes,
				in:         cmd.InOrStdin(),
				out:        cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}

func runDelete(ctx context.Context, opts deleteOptions) error {
	if opts.bucket == "" {
		return errors.New("bucket name cannot be empty")
	}
	if opts.identifier == "" {
		return errors.New("policy identifier cannot be empty")
	}
	if !opts.yes {
		confirmed, err := confirm.Delete(opts.in, opts.out, "storage policy", fmt.Sprintf("%s on bucket %s", opts.identifier, opts.bucket))
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	}

	policy, err := clistorage.NewService(opts.deps).DeletePolicy(ctx, opts.bucket, opts.identifier)
	if err != nil {
		return err
	}
	output.Success(opts.out, "Policy '%s' deleted from bucket '%s'", policy.Name, opts.bucket)
	return nil
}
