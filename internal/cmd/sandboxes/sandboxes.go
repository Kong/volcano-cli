// Package sandboxes provides the scriptable Sandbox public API commands.
package sandboxes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	"github.com/Kong/volcano-cli/internal/sandbox"
	"github.com/Kong/volcano-cli/internal/session"
)

type options struct {
	deps            cliruntime.Deps
	json            bool
	timeout         time.Duration
	key, generation string
}

// New builds cloud commands using the selected Volcano project and credential.
func New(deps cliruntime.Deps) *cobra.Command {
	o := &options{deps: deps}
	cmd := &cobra.Command{Use: "sandboxes", Aliases: []string{"sandbox"}, Short: "Run code in isolated sandboxes (preview)", Long: "Manage versioned templates, sandboxes, executions, files, logs and usage. Requires an enabled Sandbox service; no local or guest endpoint fallback is used."}
	cmd.PersistentFlags().BoolVar(&o.json, "json", false, "Print compact JSON (default: indented JSON)")
	cmd.PersistentFlags().DurationVar(&o.timeout, "timeout", 10*time.Minute, "Overall request or log-stream deadline")
	cmd.PersistentFlags().StringVar(&o.key, "idempotency-key", "", "Mutation identity; reuse only for the exact same request after reconciling its outcome")
	cmd.PersistentFlags().StringVar(&o.generation, "generation", "", "Pinned sandbox generation from get/create (required for sensitive operations)")
	cmd.AddCommand(o.route("capabilities", "Show enabled service capabilities", http.MethodGet, "/capabilities", 0, false, false))
	cmd.AddCommand(o.route("list", "List sandboxes", http.MethodGet, "/sandboxes", 0, true, false))
	cmd.AddCommand(o.route("get <sandbox-id>", "Get a sandbox and its generation", http.MethodGet, "/sandboxes/%s", 1, false, false))
	cmd.AddCommand(o.create(), o.exec(false), o.exec(true), o.logs(), o.usage())
	for _, action := range []string{"suspend", "resume", "terminate"} {
		cmd.AddCommand(o.lifecycle(action))
	}
	cmd.AddCommand(o.templates(), o.deployments(), o.executions(), o.files())
	operations := &cobra.Command{Use: "operations", Short: "Inspect durable operations without replaying work"}
	operations.AddCommand(o.route("get <operation-id>", "Get an operation", http.MethodGet, "/operations/%s", 1, false, false))
	cmd.AddCommand(operations)
	cmd.AddCommand(unavailable("shell <sandbox-id>", "Interactive shells", "public PTY transport is not available; use exec for one-shot commands"))
	return cmd
}

// NewLocal reserves the existing local resource tree; volcano start remains the
// only local runtime entry point. No cloud credential is sent to a local guest.
func NewLocal() *cobra.Command {
	return unavailable("sandboxes", "Local sandboxes", "local_runtime is not integrated; volcano start remains the local entry point; use volcano cloud sandboxes for an enabled cloud deployment")
}

func unavailable(use, short, reason string) *cobra.Command {
	return &cobra.Command{Use: use, Short: short, Long: reason, RunE: func(_ *cobra.Command, _ []string) error {
		return errors.New("capability_disabled: " + reason)
	}}
}

func (o *options) run(cmd *cobra.Command, fn func(context.Context, *sandbox.Client, string) error) error {
	if o.timeout <= 0 || o.timeout > 24*time.Hour {
		return errors.New("--timeout must be positive and at most 24h")
	}
	project, err := session.NewFactory(o.deps).CurrentProject()
	if err != nil {
		return err
	}
	origin := os.Getenv("VOLCANO_SANDBOX_URL")
	if origin == "" {
		switch strings.TrimRight(project.APIURL, "/") {
		case "https://api.volcano.dev":
			origin = "https://sandboxes.volcano.run"
		case "https://api.staging.volcano.dev":
			origin = "https://sandboxes.staging.volcano.run"
		default:
			return errors.New("no Sandbox service configured for this Volcano API environment")
		}
	}
	client, err := sandbox.New(origin, project.Config.Token(), project.ProjectID.String(), o.deps.HTTPClient)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()
	return fn(ctx, client, project.ProjectID.String())
}

func (o *options) request(cmd *cobra.Command, ctx context.Context, client *sandbox.Client, request sandbox.Request) (sandbox.Response, error) {
	if request.Method != http.MethodGet {
		key := o.key
		if key == "" {
			key = uuid.NewString()
		}
		if len(key) > 128 || strings.TrimSpace(key) == "" || strings.ContainsAny(key, "\r\n\x1b") {
			return sandbox.Response{}, errors.New("invalid --idempotency-key")
		}
		request.Key = key
		// Emit before dispatch: a disconnect can happen before an operation ID
		// arrives. Retaining this key never implies that replay is safe.
		fmt.Fprintf(cmd.ErrOrStderr(), "Sandbox request key: %s\n", key)
	}
	return client.Do(ctx, request)
}

func (o *options) write(cmd *cobra.Command, body json.RawMessage) error {
	if len(body) == 0 {
		body = json.RawMessage(`null`)
	}
	enc := json.NewEncoder(cmd.OutOrStdout())
	if !o.json {
		enc.SetIndent("", "  ")
	}
	return enc.Encode(body)
}

func (o *options) route(use, short, method, path string, argc int, paged, body bool) *cobra.Command {
	var data, cursor string
	var limit int
	cmd := &cobra.Command{Use: use, Short: short, Args: cobra.ExactArgs(argc), Example: "  volcano cloud sandboxes " + use + " --json"}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		values := make([]any, len(args))
		for i, arg := range args {
			versionArg := strings.Contains(path, "/versions/") && i == 1 && validVersion(arg)
			if !sandbox.ValidID(arg) && !versionArg {
				return errors.New("invalid resource identifier")
			}
			values[i] = url.PathEscape(arg)
		}
		route := path
		if len(values) != 0 {
			route = fmt.Sprintf(path, values...)
		}
		query := url.Values{}
		if paged {
			if limit < 1 || limit > 100 {
				return errors.New("--limit must be 1..100")
			}
			query.Set("limit", strconv.Itoa(limit))
			query.Set("cursor", cursor)
		}
		var payload any
		if body {
			var err error
			payload, err = loadObject(data)
			if err != nil {
				return err
			}
		}
		return o.run(cmd, func(ctx context.Context, client *sandbox.Client, _ string) error {
			response, err := o.request(cmd, ctx, client, sandbox.Request{Method: method, Path: route, Query: query, Body: payload})
			if err != nil {
				return err
			}
			return o.write(cmd, response.Body)
		})
	}
	if paged {
		cmd.Flags().IntVar(&limit, "limit", 100, "Page size (1..100)")
		cmd.Flags().StringVar(&cursor, "cursor", "", "Opaque next_cursor from the previous page")
	}
	if body {
		cmd.Flags().StringVar(&data, "data", "", "Request JSON object or @file, following the Sandbox public contract")
	}
	return cmd
}

func validVersion(value string) bool {
	return value != "." && value != ".." && len(value) <= 128 && value != "" && !strings.ContainsAny(value, "/\\%?#\r\n")
}

func loadObject(value string) (json.RawMessage, error) {
	var data []byte
	if file, ok := strings.CutPrefix(value, "@"); ok {
		f, err := os.Open(file)
		if err != nil {
			return nil, err
		}
		defer func() { _ = f.Close() }()
		data, err = io.ReadAll(io.LimitReader(f, (12<<20)+1))
		if err != nil {
			return nil, err
		}
	} else {
		data = []byte(value)
	}
	if len(data) > 12<<20 || !json.Valid(data) || !strings.HasPrefix(strings.TrimSpace(string(data)), "{") {
		return nil, errors.New("--data requires a JSON object of at most 12 MiB")
	}
	return data, nil
}

func (o *options) templates() *cobra.Command {
	cmd := &cobra.Command{Use: "templates", Short: "Manage immutable template versions (custom builds require service capability)"}
	cmd.AddCommand(o.route("list", "List templates", http.MethodGet, "/templates", 0, true, false))
	cmd.AddCommand(o.route("get <template-id> <version>", "Get an immutable template version", http.MethodGet, "/templates/%s/versions/%s", 2, false, false))
	cmd.AddCommand(o.route("create", "Create template metadata using --data", http.MethodPost, "/templates", 0, false, true))
	cmd.AddCommand(o.route("delete <template-id> <version>", "Delete a template version asynchronously", http.MethodDelete, "/templates/%s/versions/%s", 2, false, false))
	cmd.AddCommand(o.route("deploy <template-id>", "Deploy a verified source artifact using --data", http.MethodPost, "/templates/%s/deployments", 1, false, true))
	cmd.AddCommand(o.route("prepare-source <template-id>", "Prepare a source upload using --data (output contains a sensitive URL)", http.MethodPost, "/templates/%s/source-upload", 1, false, true))
	return cmd
}

func (o *options) deployments() *cobra.Command {
	cmd := &cobra.Command{Use: "deployments", Short: "Inspect template deployments"}
	cmd.AddCommand(o.route("list", "List deployments", http.MethodGet, "/deployments", 0, true, false))
	cmd.AddCommand(o.route("get <deployment-id>", "Get a deployment", http.MethodGet, "/deployments/%s", 1, false, false))
	cmd.AddCommand(o.route("cancel <deployment-id>", "Cancel a deployment asynchronously", http.MethodPost, "/deployments/%s/cancel", 1, false, false))
	return cmd
}

func (o *options) executions() *cobra.Command {
	cmd := &cobra.Command{Use: "executions", Short: "Inspect or cancel submitted executions"}
	cmd.AddCommand(o.route("list <sandbox-id>", "List executions", http.MethodGet, "/sandboxes/%s/executions", 1, true, false))
	cmd.AddCommand(o.route("get <sandbox-id> <execution-id>", "Get an execution and its command result", http.MethodGet, "/sandboxes/%s/executions/%s", 2, false, false))
	cmd.AddCommand(o.lifecycle("cancel-execution"))
	return cmd
}

func (o *options) lifecycle(action string) *cobra.Command {
	use, count := action+" <sandbox-id>", 1
	if action == "cancel-execution" {
		use, count = "cancel <sandbox-id> <execution-id>", 2
	}
	return &cobra.Command{Use: use, Short: "Request " + action + " for a pinned generation", Args: cobra.ExactArgs(count), Example: "  volcano cloud sandboxes " + use + " --generation 1", RunE: func(cmd *cobra.Command, args []string) error {
		for _, arg := range args {
			if !sandbox.ValidID(arg) {
				return errors.New("invalid resource identifier")
			}
		}
		if err := o.requireGeneration(); err != nil {
			return err
		}
		return o.run(cmd, func(ctx context.Context, client *sandbox.Client, project string) error {
			// Fetch ownership, but never silently upgrade the user-pinned generation.
			response, err := client.Do(ctx, sandbox.Request{Method: http.MethodGet, Path: "/sandboxes/" + args[0]})
			if err != nil {
				return err
			}
			var current struct {
				Ref struct {
					Scope struct {
						ProjectID  string `json:"project_id"`
						OwnerID    string `json:"owner_id"`
						Generation string `json:"project_generation"`
					} `json:"scope"`
					SandboxID  string `json:"sandbox_id"`
					Generation string `json:"generation"`
				} `json:"ref"`
			}
			if json.Unmarshal(response.Body, &current) != nil || current.Ref.Scope.ProjectID != project || current.Ref.SandboxID != args[0] || current.Ref.Generation != o.generation || !sandbox.ValidID(current.Ref.Scope.OwnerID) || !positiveRevision(current.Ref.Scope.Generation) {
				return errors.New("sandbox binding/generation conflict; inspect the resource before retrying")
			}
			path := "/sandboxes/" + args[0] + "/" + action
			if action == "cancel-execution" {
				path = "/sandboxes/" + args[0] + "/executions/" + args[1] + "/cancel"
			}
			response, err = o.request(cmd, ctx, client, sandbox.Request{Method: http.MethodPost, Path: path, Generation: o.generation, Body: current.Ref})
			if err != nil {
				return err
			}
			return o.write(cmd, response.Body)
		})
	}}
}

func positiveRevision(value string) bool {
	n, err := strconv.ParseInt(value, 10, 64)
	return err == nil && n > 0 && strconv.FormatInt(n, 10) == value
}

func (o *options) requireGeneration() error {
	if !positiveRevision(o.generation) {
		return errors.New("--generation requires the positive decimal generation from the sandbox handle")
	}
	return nil
}
