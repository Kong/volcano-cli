// Package policy provides storage policy commands.
package policy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clistorage "github.com/Kong/volcano-cli/internal/storage"
)

type createOptions struct {
	deps       cliruntime.Deps
	bucket     string
	name       string
	operation  string
	definition string
	out        io.Writer
}

func newCreate(deps cliruntime.Deps) *cobra.Command {
	var name string
	var operation string
	var definition string
	cmd := &cobra.Command{
		Use:   "create <bucket>",
		Short: "Attach a policy to a bucket",
		Long: `Attach a row-level security policy to a bucket.

The --operation flag accepts SELECT, INSERT, UPDATE, or DELETE.
The --definition flag accepts a policy expression evaluated at request time
(e.g. "auth.uid() = owner_id").`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCreate(cmd.Context(), createOptions{
				deps:       deps,
				bucket:     strings.TrimSpace(args[0]),
				name:       name,
				operation:  operation,
				definition: definition,
				out:        cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Policy name (unique within the bucket)")
	cmd.Flags().StringVar(&operation, "operation", "", "Operation the policy applies to (SELECT, INSERT, UPDATE, DELETE)")
	cmd.Flags().StringVar(&definition, "definition", "", "Policy expression")
	for _, flag := range []string{"name", "operation", "definition"} {
		if err := cmd.MarkFlagRequired(flag); err != nil {
			panic(err)
		}
	}
	return cmd
}

func runCreate(ctx context.Context, opts createOptions) error {
	if opts.bucket == "" {
		return errors.New("bucket name cannot be empty")
	}
	operation, err := parsePolicyOperation(opts.operation)
	if err != nil {
		return err
	}

	policy, err := clistorage.NewService(opts.deps).CreatePolicy(ctx, opts.bucket, api.StoragePolicyCreateInput{
		Name:       opts.name,
		Definition: opts.definition,
		Operation:  operation,
	})
	if err != nil {
		return err
	}
	output.Success(opts.out, "Policy '%s' created on bucket '%s'", policy.Name, opts.bucket)
	output.StoragePolicy(opts.out, opts.bucket, policy)
	return nil
}

func parsePolicyOperation(value string) (apiclient.CreateStoragePolicyRequestOperation, error) {
	normalized := apiclient.CreateStoragePolicyRequestOperation(strings.ToUpper(strings.TrimSpace(value)))
	if !normalized.Valid() {
		return "", fmt.Errorf("invalid operation %q (expected one of: SELECT, INSERT, UPDATE, DELETE)", value)
	}
	return normalized, nil
}
