package sandboxes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// Codes of the file listing refusals a user can act on.
const (
	codeImageOutdated = "sandbox_image_outdated"
	codeNotDirectory  = "sandbox_path_not_directory"
)

func (c *commands) files() *cobra.Command {
	cmd := &cobra.Command{Use: "files", Short: "List, read, or write session files"}
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
	cmd.AddCommand(c.listFiles())
	return cmd
}

func (c *commands) listFiles() *cobra.Command {
	var withSHA256, recursive bool
	var limit int
	cmd := &cobra.Command{
		Use:   "list <session-id> [path]",
		Short: "List workspace entries",
		Long: `List a directory in the session's workspace: each entry's type, permission
bits, size, and path relative to the workspace root, sorted by path. Name the
directory as src or /workspace/src; omit it to list the whole workspace.

The listing includes every entry below the directory unless --recursive=false.
It reports a symlink as a symlink and never follows it. --sha256 adds each
regular file's SHA-256 digest, or why the file has none: budget (over
256 MiB), changing (it changed while hashed; list it again later), or
unreadable (more than one hard link, another owner, or no owner read). The
command reads every page before it prints; --json prints the entries as one
JSON array.`,
		Example: fmt.Sprintf("  %s\n  %s\n  %s",
			cliruntime.CommandPath(c.deps, "sandboxes files list SESSION_ID"),
			cliruntime.CommandPath(c.deps, "sandboxes files list SESSION_ID src --sha256"),
			cliruntime.CommandPath(c.deps, "sandboxes files list SESSION_ID /workspace/src --recursive=false --json")),
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := uuid.Parse(args[0])
			if err != nil {
				return err
			}
			// Send only what the user chose; the API owns the defaults.
			params := apiclient.ListSandboxSessionFilesParams{SessionId: id}
			if len(args) == 2 {
				params.Path = &args[1]
			}
			if cmd.Flags().Changed("recursive") {
				params.Recursive = &recursive
			}
			if withSHA256 {
				params.IncludeSha256 = &withSHA256
			}
			if limit != 0 {
				params.Limit = &limit
			}
			project, err := c.project()
			if err != nil {
				return err
			}
			// The listing names the session's own project, which need not be
			// the selected one; other session commands accept any session too.
			session, err := project.API.SandboxSession(cmd.Context(), id)
			if err != nil {
				return err
			}
			params.ProjectId = session.ProjectId
			entries, err := listAllFiles(cmd.Context(), project.API, params)
			if err != nil {
				return c.listRefusal(err, params.Path)
			}
			if c.json {
				return writeListing(cmd.OutOrStdout(), entries)
			}
			output.SandboxFiles(cmd.OutOrStdout(), entries, withSHA256)
			return nil
		},
	}
	cmd.Flags().BoolVar(&withSHA256, "sha256", false, "Add each regular file's SHA-256 digest")
	cmd.Flags().BoolVar(&recursive, "recursive", true, "List every entry below the directory; --recursive=false lists only its direct entries")
	cmd.Flags().IntVar(&limit, "limit", 0, "Most entries per request, 1 to 10000 (server default if 0)")
	return cmd
}

// listAllFiles reads every page of a listing. Each request repeats the first
// one's path and options with the previous page's cursor, as the API requires.
func listAllFiles(ctx context.Context, client *api.Client, params apiclient.ListSandboxSessionFilesParams) ([]apiclient.SandboxFileEntry, error) {
	entries := []apiclient.SandboxFileEntry{}
	for {
		page, err := client.ListSandboxFiles(ctx, &params)
		if err != nil {
			return nil, err
		}
		entries = append(entries, page.Entries...)
		if page.NextCursor == nil || *page.NextCursor == "" {
			return entries, nil
		}
		// Following a cursor that does not move would never end.
		if params.Cursor != nil && *params.Cursor == *page.NextCursor {
			return nil, errors.New("the Volcano API returned the same file listing page twice")
		}
		params.Cursor = page.NextCursor
	}
}

// listRefusal says what to do about a listing refusal the user can act on. It
// keeps the refusal, so the exit code still reports it.
func (c *commands) listRefusal(err error, path *string) error {
	switch {
	case api.Code(err) == codeImageOutdated:
		return api.WithCause(fmt.Errorf("this session's image predates file listing: start a new session with %s; for a custom template, redeploy it with %s first",
			cliruntime.CommandPath(c.deps, "sandboxes run"), cliruntime.CommandPath(c.deps, "sandboxes templates deploy")), err)
	case api.Code(err) == codeNotDirectory:
		name := "the workspace root"
		if path != nil {
			name = strconv.Quote(*path)
		}
		return api.WithCause(fmt.Errorf("%s is not a directory in the session's workspace: it is missing, is a file, or passes through a symlink", name), err)
	case api.RouteMissing(err):
		return api.WithCause(errors.New("this Volcano API cannot list Sandbox files yet"), err)
	}
	return err
}

// writeListing prints entries as one compact JSON array. It escapes DEL and
// the C1 controls, which encoding/json passes through, like the C0 controls:
// a program in the Sandbox names its files, and the array can reach a
// terminal. These runes only appear inside JSON strings, where the escape
// means the same.
func writeListing(w io.Writer, entries []apiclient.SandboxFileEntry) error {
	data, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	var b strings.Builder
	for _, r := range string(data) {
		if r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			fmt.Fprintf(&b, "\\u%04x", r)
			continue
		}
		b.WriteRune(r)
	}
	b.WriteByte('\n')
	_, err = io.WriteString(w, b.String())
	return err
}
