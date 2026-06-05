package object

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clistorage "github.com/Kong/volcano-cli/internal/storage"
)

type downloadOptions struct {
	deps           cliruntime.Deps
	serviceOptions []clistorage.Option
	bucket         string
	remotePath     string
	localPath      string
	out            io.Writer
}

func newDownload(deps cliruntime.Deps, serviceOptions ...clistorage.Option) *cobra.Command {
	return &cobra.Command{
		Use:   "download <bucket> <remote-path> <local-path>",
		Short: "Download an object to a local file (use '-' for stdout)",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDownload(cmd.Context(), downloadOptions{
				deps:           deps,
				serviceOptions: serviceOptions,
				bucket:         strings.TrimSpace(args[0]),
				remotePath:     strings.TrimSpace(args[1]),
				localPath:      args[2],
				out:            cmd.OutOrStdout(),
			})
		},
	}
}

func runDownload(ctx context.Context, opts downloadOptions) error {
	if opts.bucket == "" {
		return errors.New("bucket name cannot be empty")
	}
	if opts.remotePath == "" {
		return errors.New("remote path cannot be empty")
	}

	service := clistorage.NewService(opts.deps, opts.serviceOptions...)
	target := opts.localPath
	if target == "-" {
		_, err := service.DownloadObject(ctx, opts.bucket, opts.remotePath, opts.out)
		return err
	}

	written, err := downloadObjectToFile(ctx, service, opts.bucket, opts.remotePath, target)
	if err != nil {
		return err
	}
	output.Success(opts.out, "Downloaded '%s' from bucket '%s' to '%s' (%d bytes)", opts.remotePath, opts.bucket, target, written)
	return nil
}

// downloadObjectToFile streams the object body into a sibling ".part" file and
// renames it onto target only after a clean Close, so an interrupted download
// never destroys an existing file or leaves a half-written one in its place.
func downloadObjectToFile(ctx context.Context, service clistorage.Service, bucket, remotePath, target string) (written int64, err error) {
	tempPath := target + ".part"
	f, err := os.Create(tempPath)
	if err != nil {
		return 0, fmt.Errorf("failed to create file %q: %w", tempPath, err)
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			_ = os.Remove(tempPath)
			return
		}
		if rerr := os.Rename(tempPath, target); rerr != nil {
			_ = os.Remove(tempPath)
			err = fmt.Errorf("failed to finalize download to %q: %w", target, rerr)
		}
	}()

	return service.DownloadObject(ctx, bucket, remotePath, f)
}
