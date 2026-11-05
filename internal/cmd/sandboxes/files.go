package sandboxes

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/archive"
	"github.com/Kong/volcano-cli/internal/output"
	"github.com/Kong/volcano-cli/internal/sandbox"
)

func (c *commands) files() *cobra.Command {
	cmd := &cobra.Command{Use: "files", Short: "Read or write session files"}
	cmd.AddCommand(&cobra.Command{Use: "read <session-id> <path>", Short: "Write file bytes to stdout", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		id, err := uuid.Parse(args[0])
		if err != nil {
			return err
		}
		project, err := c.project()
		if err != nil {
			return err
		}
		value, err := project.API.ReadSandboxFile(cmd.Context(), id, apiclient.SandboxFileReadRequest{Path: args[1]})
		if err != nil {
			return err
		}
		if c.json {
			return c.write(cmd, value)
		}
		_, err = cmd.OutOrStdout().Write(value.Data)
		return err
	}})
	cmd.AddCommand(&cobra.Command{Use: "write <session-id> <path>", Short: "Read file bytes from stdin", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		id, err := uuid.Parse(args[0])
		if err != nil {
			return err
		}
		const limit = 8 << 20
		data, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), limit+1))
		if err != nil {
			return err
		}
		if len(data) > limit {
			return errors.New("files are limited to 8 MiB")
		}
		project, err := c.project()
		if err != nil {
			return err
		}
		if err := project.API.WriteSandboxFile(cmd.Context(), id, apiclient.SandboxFileWriteRequest{Path: args[1], Data: data}); err != nil {
			return err
		}
		return c.write(cmd, struct {
			Path string `json:"path"`
		}{args[1]})
	}})
	cmd.AddCommand(c.upload())
	return cmd
}

// uploadTotals is what the calls of one upload wrote.
type uploadTotals struct {
	Files int64 `json:"files"`
	Bytes int64 `json:"bytes"`
	Calls int   `json:"calls"`
}

func (c *commands) upload() *cobra.Command {
	var destination string
	cmd := &cobra.Command{
		Use:   "upload <session-id> <local-dir> --path <dir>",
		Short: "Copy a local directory into the session's workspace",
		Long: "Copy a local directory's files and subdirectories into a directory of the session's workspace. " +
			"Symbolic links and special files are refused before anything is sent. " +
			"Large trees go in several calls, each written whole or not at all. " +
			"A file too large for one call, up to 1 GiB, is sent on its own in parts and lands with mode 0600.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := uuid.Parse(args[0])
			if err != nil {
				return err
			}
			upload, err := sandbox.OpenUpload(args[1], sandbox.ArchiveLimits)
			if err != nil {
				return err
			}
			defer func() { _ = upload.Close() }()
			project, err := c.project()
			if err != nil {
				return err
			}
			u := &uploader{api: project.API, project: project.ProjectID, session: id, destination: destination}
			if err := u.run(cmd.Context(), upload); err != nil {
				return u.stopped(err)
			}
			if c.json {
				return c.write(cmd, u.total)
			}
			output.Success(cmd.OutOrStdout(), "Uploaded %d %s (%s) to %s in %d %s",
				u.total.Files, plural(u.total.Files, "file", "files"), archive.FormatSize(u.total.Bytes),
				workspacePath(destination), u.total.Calls, plural(int64(u.total.Calls), "call", "calls"))
			return nil
		},
	}
	cmd.Flags().StringVar(&destination, "path", "", "Workspace directory to copy into, relative to /workspace; created when missing")
	_ = cmd.MarkFlagRequired("path")
	return cmd
}

// uploader sends one upload: its archives first, which create every
// directory, then each file too large for an archive, in parts.
type uploader struct {
	api         *api.Client
	project     uuid.UUID
	session     uuid.UUID
	destination string
	total       uploadTotals
}

func (u *uploader) run(ctx context.Context, upload *sandbox.Upload) error {
	err := upload.Each(func(body []byte) error {
		result, err := u.api.CreateSandboxFileArchive(ctx, api.SandboxFileArchiveInput{
			Project: u.project, Session: u.session, Path: u.destination, Archive: body,
		})
		if err != nil {
			return u.refusal(err, "")
		}
		u.total.Calls++
		u.total.Files += result.Files
		u.total.Bytes += result.Bytes
		return nil
	})
	if err != nil {
		return err
	}
	for _, file := range upload.LargeFiles() {
		if err := u.largeFile(ctx, file); err != nil {
			return err
		}
	}
	return nil
}

// largeFile sends file in parts. Nothing appears in the session until the
// last part arrives and the session has checked the whole file's SHA-256.
func (u *uploader) largeFile(ctx context.Context, file sandbox.LargeFile) error {
	target := path.Join(u.destination, file.Name)
	err := file.Parts(func(part sandbox.FilePart) error {
		request := apiclient.SandboxFilePartRequest{
			ProjectId: u.project, SessionId: u.session, Path: target,
			Offset: part.Offset, Size: part.Size, Sha256: part.SHA256, Data: part.Data,
		}
		_, err := u.api.CreateSandboxFilePart(ctx, request)
		if worthResending(ctx, err) {
			// The session takes a part again at an offset it has reached.
			_, err = u.api.CreateSandboxFilePart(ctx, request)
		}
		if err != nil {
			return u.refusal(err, file.Path)
		}
		u.total.Calls++
		return nil
	})
	if err != nil {
		return err
	}
	u.total.Files++
	u.total.Bytes += file.Size
	return nil
}

// worthResending reports whether a failed part may succeed if sent again: the
// request may not have arrived, or the server failed. A refusal would repeat.
func worthResending(ctx context.Context, err error) bool {
	if err == nil || ctx.Err() != nil {
		return false
	}
	status := api.Status(err)
	return status == 0 || status >= http.StatusInternalServerError
}

// uploadRefusal is an API refusal in words that say what to do. It keeps the
// API error as its cause, so the exit code still reflects its status.
type uploadRefusal struct {
	reason string
	cause  error
}

func (e *uploadRefusal) Error() string { return e.reason }

func (e *uploadRefusal) Unwrap() error { return e.cause }

// refusal words a refused call. localPath names the file whose part was
// sending.
func (u *uploader) refusal(err error, localPath string) error {
	var reason string
	switch {
	case api.RouteMissing(err):
		reason = "this server does not support Sandbox uploads yet"
	case api.Status(err) == http.StatusNotFound && api.Code(err) == "":
		reason = fmt.Sprintf("no session %s in the selected project", u.session)
	case api.Code(err) == "sandbox_path_conflict":
		reason = "the upload conflicts with the session's workspace: --path or a directory in the upload " +
			"is a file or symbolic link there, or a file in the upload is a directory there"
	case api.Code(err) == "sandbox_workspace_full":
		reason = "the session's disk has no room for the upload"
	case api.Code(err) == "sandbox_image_outdated":
		reason = "this session's image predates uploads; start a new session from a current preset, or redeploy its template"
	case api.Code(err) == "sandbox_file_part_mismatch":
		reason = localPath + " changed while it was being uploaded; upload again"
	case api.Code(err) == "sandbox_file_part_out_of_order":
		reason = "the session no longer has the parts of " + localPath + " sent so far; upload again"
	case api.Code(err) == "sandbox_archive_invalid":
		reason = "the session refused the upload as invalid; a path may be too long (4,096 bytes with --path, 255 bytes per name)"
	case api.Code(err) == "sandbox_archive_too_large":
		reason = "the session refused an archive over its per-call limits; upgrade the CLI with `volcano upgrade`"
	default:
		return err
	}
	return &uploadRefusal{reason: reason, cause: err}
}

// stopped says what the calls before a failure already wrote, since each call
// lands on its own. Before any call succeeds, an error other than a worded
// refusal keeps its own message.
func (u *uploader) stopped(err error) error {
	if u.total.Calls == 0 {
		if _, ok := errors.AsType[*uploadRefusal](err); !ok {
			return err
		}
		return fmt.Errorf("upload failed: %w. Nothing was written", err)
	}
	return fmt.Errorf("upload stopped: %w. Earlier calls already wrote %d %s (%s) to %s",
		err, u.total.Files, plural(u.total.Files, "file", "files"), archive.FormatSize(u.total.Bytes),
		workspacePath(u.destination))
}

// workspacePath is a --path value as the absolute path the session sees.
func workspacePath(dir string) string {
	return "/workspace/" + strings.TrimPrefix(dir, "/workspace/")
}

func plural(n int64, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
