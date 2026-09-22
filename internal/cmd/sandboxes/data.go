package sandboxes

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/sandbox"
)

func (o *options) files() *cobra.Command {
	cmd := &cobra.Command{Use: "files", Short: "Read and write bounded workspace-relative files"}
	for _, action := range []string{"read", "write", "list", "delete"} {
		cmd.AddCommand(o.file(action))
	}
	return cmd
}

func (o *options) file(action string) *cobra.Command {
	var cursor, digest, source string
	var offset int64
	var limit int
	cmd := &cobra.Command{Use: action + " <sandbox-id> <workspace-path>", Short: action + " a workspace path", Args: cobra.ExactArgs(2), Example: "  volcano cloud sandboxes files " + action + " sb_123 main.js --generation 1"}
	cmd.Flags().StringVar(&cursor, "cursor", "", "Opaque list cursor")
	cmd.Flags().Int64Var(&offset, "offset", 0, "Read offset in bytes")
	defaultLimit := 100
	if action == "read" {
		defaultLimit = 8 << 20
	}
	cmd.Flags().IntVar(&limit, "limit", defaultLimit, "Maximum bytes (read) or entries (list)")
	if action == "write" {
		cmd.Flags().StringVar(&source, "source", "", "Local file to upload (at most 8 MiB)")
		cmd.Flags().StringVar(&digest, "expected-digest", "", "Require the existing file digest to match")
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if !sandbox.ValidID(args[0]) {
			return errors.New("invalid sandbox ID")
		}
		if err := o.requireGeneration(); err != nil {
			return err
		}
		if !workspacePath(args[1]) {
			return errors.New("path must be workspace-relative, without traversal")
		}
		maxLimit := 100
		if action == "read" {
			maxLimit = 8 << 20
		}
		if offset < 0 || limit < 1 || limit > maxLimit {
			return errors.New("invalid file offset or limit")
		}
		request := sandbox.Request{
			Method: http.MethodGet, Path: "/sandboxes/" + args[0] + "/files", Generation: o.generation,
			Query: url.Values{"path": {args[1]}, "offset": {strconv.FormatInt(offset, 10)}, "limit": {strconv.Itoa(limit)}, "cursor": {cursor}},
		}
		switch action {
		case "list":
			request.Path += "/list"
		case "delete":
			request.Method = http.MethodDelete
		case "write":
			f, err := os.Open(source)
			if err != nil {
				return err
			}
			defer func() { _ = f.Close() }()
			data, err := io.ReadAll(io.LimitReader(f, (8<<20)+1))
			if err != nil {
				return err
			}
			if len(data) > 8<<20 {
				return errors.New("file exceeds 8 MiB")
			}
			request.Method, request.Query = http.MethodPut, nil
			request.Body = struct {
				Path           string `json:"path"`
				Content        []byte `json:"content"`
				ExpectedDigest string `json:"expected_digest,omitempty"`
			}{args[1], data, digest}
		}
		return o.run(cmd, func(ctx context.Context, client *sandbox.Client, _ string) error {
			response, err := o.request(cmd, ctx, client, request)
			if err != nil {
				return err
			}
			// Read returns base64 content and next_offset/eof together. A partial
			// read must not masquerade as a complete local-file download.
			return o.write(cmd, response.Body)
		})
	}
	return cmd
}

func workspacePath(value string) bool {
	if value == "" || len(value) > 4096 || path.Clean(value) != value || strings.HasPrefix(value, "/") || strings.ContainsAny(value, "\\\x00\r\n") {
		return false
	}
	return !slices.Contains(strings.Split(value, "/"), "..")
}

func (o *options) logs() *cobra.Command {
	var cursor string
	var follow bool
	var limit int
	cmd := &cobra.Command{Use: "logs <sandbox-id>", Short: "Stream bounded NDJSON logs; reconnect explicitly with the last cursor", Args: cobra.ExactArgs(1), Example: "  volcano cloud sandboxes logs sb_123 --generation 1 --follow --json"}
	cmd.Flags().StringVar(&cursor, "cursor", "", "Resume cursor (fresh authorization on every connection)")
	cmd.Flags().BoolVar(&follow, "follow", false, "Follow until timeout or cancellation; disconnect is a nonzero exit")
	cmd.Flags().IntVar(&limit, "limit", 100, "Maximum entries per request (1..100)")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if !sandbox.ValidID(args[0]) || limit < 1 || limit > 100 {
			return errors.New("invalid sandbox ID or limit")
		}
		if err := o.requireGeneration(); err != nil {
			return err
		}
		return o.run(cmd, func(ctx context.Context, client *sandbox.Client, _ string) error {
			request := sandbox.Request{
				Method: http.MethodGet, Path: "/sandboxes/" + args[0] + "/logs", Generation: o.generation,
				Query: url.Values{"cursor": {cursor}, "limit": {strconv.Itoa(limit)}, "follow": {strconv.FormatBool(follow)}},
			}
			return client.Logs(ctx, request, follow, func(frame json.RawMessage) error {
				// Always one JSON frame per line, including cursors/truncation.
				return json.NewEncoder(cmd.OutOrStdout()).Encode(frame)
			})
		})
	}
	return cmd
}

func (o *options) usage() *cobra.Command {
	var from, to, id, cursor string
	var limit int
	cmd := &cobra.Command{Use: "usage", Short: "Read raw usage ledger events without losing decimal precision", Args: cobra.NoArgs, Example: "  volcano cloud sandboxes usage --from 2026-09-01T00:00:00Z --to 2026-09-02T00:00:00Z --json"}
	cmd.Flags().StringVar(&from, "from", "", "Inclusive RFC3339 start time")
	cmd.Flags().StringVar(&to, "to", "", "RFC3339 end time")
	cmd.Flags().StringVar(&id, "sandbox", "", "Optional sandbox ID filter")
	cmd.Flags().StringVar(&cursor, "cursor", "", "Opaque next_cursor")
	cmd.Flags().IntVar(&limit, "limit", 100, "Page size (1..100)")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		start, err := time.Parse(time.RFC3339, from)
		if err != nil {
			return errors.New("--from requires RFC3339")
		}
		end, err := time.Parse(time.RFC3339, to)
		if err != nil || !end.After(start) || limit < 1 || limit > 100 || (id != "" && !sandbox.ValidID(id)) {
			return errors.New("invalid usage interval, limit or sandbox ID")
		}
		return o.run(cmd, func(ctx context.Context, client *sandbox.Client, _ string) error {
			response, err := client.Do(ctx, sandbox.Request{Method: http.MethodGet, Path: "/usage", Query: url.Values{"from": {start.UTC().Format(time.RFC3339Nano)}, "to": {end.UTC().Format(time.RFC3339Nano)}, "sandbox_id": {id}, "cursor": {cursor}, "limit": {strconv.Itoa(limit)}}})
			if err != nil {
				return err
			}
			return o.write(cmd, response.Body)
		})
	}
	return cmd
}
