package sandboxes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/sandbox"
)

type createRequest struct {
	TemplateID      string `json:"template_id"`
	TemplateVersion string `json:"template_version"`
	Baseline        string `json:"baseline"`
	DC              string `json:"dc"`
	LifetimeMs      int64  `json:"lifetime_ms"`
}

func createFlags(cmd *cobra.Command, request *createRequest, lifetime *time.Duration) {
	cmd.Flags().StringVar(&request.TemplateID, "template", "", "Template/preset ID")
	cmd.Flags().StringVar(&request.TemplateVersion, "template-version", "", "Immutable template version")
	cmd.Flags().StringVar(&request.Baseline, "size", "1gib", "Baseline size: 1gib or 2gib")
	cmd.Flags().StringVar(&request.DC, "dc", "", "Verified Volcano DC (see capabilities)")
	cmd.Flags().DurationVar(lifetime, "ttl", 10*time.Minute, "Absolute sandbox lifetime (server cleanup continues after disconnect)")
}

func (r *createRequest) validate(ttl time.Duration) error {
	if !sandbox.ValidID(r.TemplateID) || !validVersion(r.TemplateVersion) || r.DC == "" || (r.Baseline != "1gib" && r.Baseline != "2gib") || ttl < time.Millisecond || ttl > time.Hour {
		return errors.New("create requires --template, --template-version, --dc, --size 1gib|2gib and --ttl between 1ms and 1h")
	}
	r.LifetimeMs = ttl.Milliseconds()
	return nil
}

func (o *options) create() *cobra.Command {
	var request createRequest
	var ttl time.Duration
	cmd := &cobra.Command{Use: "create", Short: "Request a sandbox; returns a durable operation, not a ready handle", Args: cobra.NoArgs, Example: "  volcano cloud sandboxes create --template node --template-version 1 --dc us-east-1 --json"}
	createFlags(cmd, &request, &ttl)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := request.validate(ttl); err != nil {
			return err
		}
		return o.run(cmd, func(ctx context.Context, client *sandbox.Client, _ string) error {
			response, err := o.request(cmd, ctx, client, sandbox.Request{Method: http.MethodPost, Path: "/sandboxes", Body: request})
			if err != nil {
				return err
			}
			return o.write(cmd, response.Body)
		})
	}
	return cmd
}

// ExitError preserves the remote command's known process status. The entry point
// recognizes ExitCode without treating remote stderr as a platform failure.
type ExitError struct{ Code int }

func (e *ExitError) Error() string {
	return fmt.Sprintf("sandbox command exited with status %d", e.Code)
}

// ExitCode returns the shell-compatible process exit code.
func (e *ExitError) ExitCode() int { return e.Code }

func (o *options) exec(oneShot bool) *cobra.Command {
	var command, cwd string
	var duration, wait, ttl time.Duration
	var async bool
	var environment map[string]string
	var create createRequest
	use, count, path := "exec <sandbox-id>", 1, ""
	if oneShot {
		use, count, path = "run", 0, "/one-shot"
	}
	cmd := &cobra.Command{Use: use, Short: "Execute a command once; never automatically replay an unknown result", Args: cobra.ExactArgs(count), Example: "  volcano cloud sandboxes exec sb_123 --generation 1 --command 'node /workspace/main.js'"}
	cmd.Flags().StringVar(&command, "command", "", "Command string to execute inside the sandbox")
	cmd.Flags().StringVar(&cwd, "workdir", ".", "Workspace-relative working directory")
	cmd.Flags().StringToStringVar(&environment, "env", nil, "Command environment KEY=value (repeatable)")
	cmd.Flags().DurationVar(&duration, "command-timeout", time.Minute, "Server-side command deadline")
	cmd.Flags().DurationVar(&wait, "wait", 10*time.Second, "Synchronous server wait, at most 60s")
	cmd.Flags().BoolVar(&async, "async", false, "Return accepted operation without waiting; exit 0 means accepted, not completed")
	if oneShot {
		createFlags(cmd, &create, &ttl)
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if command == "" || len(command) > 16384 || duration < time.Millisecond || duration > 10*time.Minute || wait < 0 || wait > time.Minute || !workspacePath(cwd) {
			return errors.New("--command is required (at most 16 KiB); command-timeout must be 1ms..10m, wait 0..60s and workdir workspace-relative")
		}
		if !oneShot {
			if !sandbox.ValidID(args[0]) {
				return errors.New("invalid sandbox ID")
			}
			if err := o.requireGeneration(); err != nil {
				return err
			}
			path = "/sandboxes/" + args[0] + "/executions"
		} else if err := create.validate(ttl); err != nil {
			return err
		}
		if async {
			wait = 0
		}
		if environment == nil {
			environment = map[string]string{}
		}
		body := map[string]any{"command": map[string]any{"command": command, "working_directory": cwd, "environment": environment, "stdin": "", "timeout_ms": duration.Milliseconds()}, "wait_ms": wait.Milliseconds()}
		if oneShot {
			body["sandbox"] = create
		}
		return o.run(cmd, func(ctx context.Context, client *sandbox.Client, _ string) error {
			response, err := o.request(cmd, ctx, client, sandbox.Request{Method: http.MethodPost, Path: path, Generation: o.generation, Body: body})
			if err != nil {
				return err
			}
			return o.executionResult(cmd, response, async)
		})
	}
	return cmd
}

func (o *options) executionResult(cmd *cobra.Command, response sandbox.Response, async bool) error {
	var reply struct {
		Result *struct {
			Stdout          string `json:"stdout"`
			Stderr          string `json:"stderr"`
			State           string `json:"state"`
			ExitCode        *int   `json:"exit_code"`
			OperationID     string `json:"operation_id"`
			StdoutTruncated bool   `json:"stdout_truncated"`
			StderrTruncated bool   `json:"stderr_truncated"`
		} `json:"result"`
		Operation *struct {
			ID string `json:"id"`
		} `json:"operation"`
	}
	if json.Unmarshal(response.Body, &reply) != nil || (reply.Result == nil) == (reply.Operation == nil) {
		return errors.New("invalid execution reply; outcome unknown, do not resubmit")
	}
	if reply.Operation != nil {
		if response.Status != http.StatusAccepted || !sandbox.ValidID(reply.Operation.ID) {
			return errors.New("invalid accepted execution reply")
		}
		if err := o.write(cmd, response.Body); err != nil {
			return err
		}
		if async {
			return nil
		}
		return fmt.Errorf("command outcome pending; inspect operations get %s; do not resubmit", reply.Operation.ID)
	}
	result := reply.Result
	if response.Status != http.StatusOK || !sandbox.ValidID(result.OperationID) {
		return errors.New("invalid command result")
	}
	if o.json {
		if err := o.write(cmd, response.Body); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprint(cmd.OutOrStdout(), result.Stdout); err != nil {
			return err
		}
		if _, err := fmt.Fprint(cmd.ErrOrStderr(), result.Stderr); err != nil {
			return err
		}
		if result.StdoutTruncated || result.StderrTruncated {
			fmt.Fprintln(cmd.ErrOrStderr(), "Sandbox command output was truncated")
		}
	}
	if result.State != "completed" || result.ExitCode == nil || *result.ExitCode < 0 || *result.ExitCode > 255 {
		return fmt.Errorf("command outcome %s; operation %s has no known shell exit status", result.State, result.OperationID)
	}
	if *result.ExitCode != 0 {
		return &ExitError{Code: *result.ExitCode}
	}
	return nil
}
