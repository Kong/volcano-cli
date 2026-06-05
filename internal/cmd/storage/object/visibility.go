package object

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clistorage "github.com/Kong/volcano-cli/internal/storage"
)

type visibilityOptions struct {
	deps           cliruntime.Deps
	serviceOptions []clistorage.Option
	bucket         string
	remotePath     string
	makePublic     bool
	out            io.Writer
}

func newVisibility(deps cliruntime.Deps, serviceOptions ...clistorage.Option) *cobra.Command {
	var makePublic bool
	var makePrivate bool
	cmd := &cobra.Command{
		Use:   "visibility <bucket> <remote-path>",
		Short: "Set an object's public/private visibility",
		Long:  "Use --public to make an object publicly downloadable or --private to revoke public access.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVisibility(cmd.Context(), visibilityOptions{
				deps:           deps,
				serviceOptions: serviceOptions,
				bucket:         strings.TrimSpace(args[0]),
				remotePath:     strings.TrimSpace(args[1]),
				makePublic:     makePublic,
				out:            cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().BoolVar(&makePublic, "public", false, "Make the object publicly downloadable")
	cmd.Flags().BoolVar(&makePrivate, "private", false, "Revoke public access")
	cmd.MarkFlagsMutuallyExclusive("public", "private")
	cmd.MarkFlagsOneRequired("public", "private")
	return cmd
}

func runVisibility(ctx context.Context, opts visibilityOptions) error {
	if opts.bucket == "" {
		return errors.New("bucket name cannot be empty")
	}
	if opts.remotePath == "" {
		return errors.New("remote path cannot be empty")
	}
	object, err := clistorage.NewService(opts.deps, opts.serviceOptions...).SetObjectVisibility(ctx, opts.bucket, opts.remotePath, opts.makePublic)
	if err != nil {
		return err
	}
	visibility := "private"
	if opts.makePublic {
		visibility = "public"
	}
	output.Success(opts.out, "Object '%s' in bucket '%s' is now %s", opts.remotePath, opts.bucket, visibility)
	output.StorageObject(opts.out, opts.bucket, object)
	return nil
}
