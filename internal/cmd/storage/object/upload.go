package object

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clistorage "github.com/Kong/volcano-cli/internal/storage"
)

type uploadOptions struct {
	deps           cliruntime.Deps
	serviceOptions []clistorage.Option
	bucket         string
	localPath      string
	remotePath     string
	contentType    string
	out            io.Writer
}

func newUpload(deps cliruntime.Deps, serviceOptions ...clistorage.Option) *cobra.Command {
	var contentType string
	cmd := &cobra.Command{
		Use:   "upload <bucket> <local-path> <remote-path>",
		Short: "Upload a local file to a bucket",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpload(cmd.Context(), uploadOptions{
				deps:           deps,
				serviceOptions: serviceOptions,
				bucket:         strings.TrimSpace(args[0]),
				localPath:      args[1],
				remotePath:     strings.TrimSpace(args[2]),
				contentType:    contentType,
				out:            cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().StringVar(&contentType, "content-type", "", "Override MIME type (auto-detected from extension if empty)")
	return cmd
}

func runUpload(ctx context.Context, opts uploadOptions) error {
	if opts.bucket == "" {
		return errors.New("bucket name cannot be empty")
	}
	if opts.remotePath == "" {
		return errors.New("remote path cannot be empty")
	}
	localPath := strings.TrimSpace(opts.localPath)
	if localPath == "" {
		return errors.New("local file path is required")
	}
	file, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("failed to open file %q: %w", localPath, err)
	}
	defer func() { _ = file.Close() }()
	contentType := strings.TrimSpace(opts.contentType)
	if contentType == "" {
		contentType = mime.TypeByExtension(filepath.Ext(localPath))
	}

	object, err := clistorage.NewService(opts.deps, opts.serviceOptions...).UploadObject(ctx, opts.bucket, opts.remotePath, contentType, file)
	if err != nil {
		return err
	}
	output.Success(opts.out, "Uploaded '%s' to bucket '%s' as '%s'", localPath, opts.bucket, object.Name)
	output.StorageObject(opts.out, opts.bucket, object)
	return nil
}
