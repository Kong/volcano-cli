package sandboxes

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

type launchOptions struct {
	preset, template, region, key string
	memory, duration, timeout     int
}

func (o *launchOptions) flags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&o.preset, "preset", "", "Preset from sandboxes presets")
	cmd.Flags().StringVar(&o.template, "template", "", "Saved Sandbox template ID")
	cmd.Flags().StringVar(&o.region, "region", "aws-us-east-1", "Sandbox region")
	cmd.Flags().StringVar(&o.key, "request-id", "", "UUID idempotency key; reuse when retrying the same request")
	cmd.Flags().IntVar(&o.memory, "memory", 0, "Memory in MB (1024 or 2048; defaults to the preset or template)")
}

func (o launchOptions) request() (apiclient.CreateSandboxSessionRequest, uuid.UUID, error) {
	result := apiclient.CreateSandboxSessionRequest{Region: o.region}
	if (o.preset == "") == (o.template == "") {
		return result, uuid.Nil, errors.New("choose exactly one --preset or --template")
	}
	if o.memory != 0 && o.memory != 1024 && o.memory != 2048 {
		return result, uuid.Nil, errors.New("--memory must be 1024 or 2048")
	}
	if o.memory != 0 {
		memory := apiclient.CreateSandboxSessionRequestMemoryMb(o.memory)
		result.MemoryMb = &memory
	}
	if o.preset != "" {
		preset := apiclient.CreateSandboxSessionRequestPreset(o.preset)
		result.Preset = &preset
	}
	if o.template != "" {
		id, err := uuid.Parse(o.template)
		if err != nil {
			return result, uuid.Nil, err
		}
		result.SandboxId = &id
	}
	key, err := requestID(o.key)
	return result, key, err
}

func requestID(value string) (uuid.UUID, error) {
	if value == "" {
		return uuid.NewRandom()
	}
	return uuid.Parse(value)
}

func (c *commands) execute() *cobra.Command {
	var opts launchOptions
	cmd := &cobra.Command{Use: "exec [session-id] -- <command> [args...]", Short: "Execute once, or inside an existing session", RunE: func(cmd *cobra.Command, args []string) error {
		split := cmd.ArgsLenAtDash()
		if split < 0 || split > 1 || split >= len(args) {
			return errors.New("use exec [session-id] -- command [args...]")
		}
		command := shellCommand(args[split:])
		project, err := c.project()
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), time.Duration(opts.timeout)*time.Second+3*time.Minute)
		defer cancel()
		if split == 1 {
			if opts.preset != "" || opts.template != "" || cmd.Flags().Changed("memory") || cmd.Flags().Changed("region") {
				return errors.New("preset, template, memory, and region only apply when creating a sandbox")
			}
			id, err := uuid.Parse(args[0])
			if err != nil {
				return err
			}
			key, err := requestID(opts.key)
			if err != nil {
				return err
			}
			value, err := project.API.ExecSandboxSession(ctx, id, key, apiclient.SandboxCommandRequest{Command: command, TimeoutSeconds: &opts.timeout})
			if err != nil {
				return err
			}
			return c.outputCommand(cmd, value.Stdout, value.Stderr, value.ExitCode, value)
		}
		request, key, err := opts.request()
		if err != nil {
			return err
		}
		body := apiclient.SandboxExecutionRequest{Command: command, Region: request.Region, SandboxId: request.SandboxId, TimeoutSeconds: &opts.timeout}
		if request.Preset != nil {
			preset := apiclient.SandboxExecutionRequestPreset(*request.Preset)
			body.Preset = &preset
		}
		if request.MemoryMb != nil {
			memory := apiclient.SandboxExecutionRequestMemoryMb(*request.MemoryMb)
			body.MemoryMb = &memory
		}
		value, err := project.API.ExecSandbox(ctx, project.ProjectID, key, body)
		if err != nil {
			return err
		}
		return c.outputCommand(cmd, value.Stdout, value.Stderr, value.ExitCode, value)
	}}
	opts.flags(cmd)
	cmd.Flags().IntVar(&opts.timeout, "timeout", 60, "Command timeout in seconds")
	return cmd
}

func shellCommand(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = "'" + strings.ReplaceAll(arg, "'", "'\"'\"'") + "'"
	}
	return strings.Join(quoted, " ")
}

func (c *commands) outputCommand(cmd *cobra.Command, stdout, stderr string, code int, value any) error {
	if c.json {
		if err := c.write(cmd, value); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprint(cmd.OutOrStdout(), stdout); err != nil {
			return err
		}
		if _, err := fmt.Fprint(cmd.ErrOrStderr(), stderr); err != nil {
			return err
		}
	}
	if code != 0 {
		return &ExitError{Code: code}
	}
	return nil
}

func (c *commands) run() *cobra.Command {
	var opts launchOptions
	cmd := &cobra.Command{Use: "run", Short: "Start a persistent session", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		request, key, err := opts.request()
		if err != nil {
			return err
		}
		request.MaxDurationSeconds = &opts.duration
		project, err := c.project()
		if err != nil {
			return err
		}
		value, err := project.API.StartSandbox(cmd.Context(), project.ProjectID, key, request)
		if err != nil {
			return err
		}
		return c.write(cmd, value)
	}}
	opts.flags(cmd)
	cmd.Flags().IntVar(&opts.duration, "duration", 3600, "Maximum session lifetime in seconds")
	return cmd
}
