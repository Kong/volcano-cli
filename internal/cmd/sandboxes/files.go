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
	codeUnreadable    = "sandbox_path_unreadable"
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
unreadable (the session user doesn't own it, its owner-read bit is clear, or
it has more than one hard link).

A listing can be incomplete. It does not enter a directory the session user
can't list, and marks it unreadable in the Size column. It skips a name that
is not UTF-8 or whose path would pass 4096 bytes, with everything below it,
and marks the directory holding it incomplete. A warning on stderr then says
what is missing.

The command reads every page before it prints. --json prints the listing as
one JSON object: the API's fields, with every page's entries in entries.`,
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
			listing, err := listAllFiles(cmd.Context(), project.API, params)
			if err != nil {
				return c.listRefusal(err, params.Path)
			}
			if c.json {
				return writeListing(cmd.OutOrStdout(), listing)
			}
			output.SandboxFiles(cmd.OutOrStdout(), cmd.ErrOrStderr(), listing, withSHA256)
			return nil
		},
	}
	cmd.Flags().BoolVar(&withSHA256, "sha256", false, "Add each regular file's SHA-256 digest")
	cmd.Flags().BoolVar(&recursive, "recursive", true, "List every entry below the directory; --recursive=false lists only its direct entries")
	cmd.Flags().IntVar(&limit, "limit", 0, "Most entries per request, 1 to 10000 (server default if 0)")
	return cmd
}

// listAllFiles reads every page of a listing into one result with every
// page's entries and no cursor. Each request repeats the first one's path and
// options with the previous page's cursor, as the API requires.
func listAllFiles(ctx context.Context, client *api.Client, params apiclient.ListSandboxSessionFilesParams) (*apiclient.SandboxFileListResult, error) {
	listing := &apiclient.SandboxFileListResult{Entries: []apiclient.SandboxFileEntry{}}
	for {
		page, err := client.ListSandboxFiles(ctx, &params)
		if err != nil {
			return nil, err
		}
		listing.ProjectId, listing.SessionId = page.ProjectId, page.SessionId
		listing.Entries = append(listing.Entries, page.Entries...)
		// Every page counts the listed directory again, and the directory can
		// change between pages. Keep the largest count, so the result never
		// understates what the listing skipped.
		if page.UnsupportedNames != nil && (listing.UnsupportedNames == nil || *page.UnsupportedNames > *listing.UnsupportedNames) {
			listing.UnsupportedNames = page.UnsupportedNames
		}
		if page.NextCursor == nil || *page.NextCursor == "" {
			return listing, nil
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
		return api.WithCause(fmt.Errorf("%s is not a directory in the session's workspace: it is missing, is a file, or passes through a symlink", listedName(path)), err)
	case api.Code(err) == codeUnreadable:
		return api.WithCause(fmt.Errorf("%s can't be listed: the session user lacks read or execute permission on it, or execute permission on a directory above it", listedName(path)), err)
	case api.RouteMissing(err):
		return api.WithCause(errors.New("this Volcano API cannot list Sandbox files yet"), err)
	}
	return err
}

// listedName names the listed directory in a refusal.
func listedName(path *string) string {
	if path == nil {
		return "the workspace root"
	}
	return strconv.Quote(*path)
}

// writeListing prints a listing as one compact JSON object. It escapes DEL and
// the C1 controls, which encoding/json passes through, like the C0 controls:
// a program in the Sandbox names its files, and the object can reach a
// terminal. These runes only appear inside JSON strings, where the escape
// means the same.
func writeListing(w io.Writer, listing *apiclient.SandboxFileListResult) error {
	data, err := json.Marshal(listing)
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
