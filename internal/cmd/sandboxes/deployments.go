package sandboxes

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/sandbox"
)

func (c *commands) deployTemplate() *cobra.Command {
	var directory, templateID, key string
	var memory int
	var ports []int
	cmd := &cobra.Command{Use: "deploy <name>", Short: "Build and deploy a custom Sandbox template", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if !regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`).MatchString(args[0]) {
			return errors.New("template name must contain 1–63 lowercase letters, digits or hyphens, starting with a letter")
		}
		if memory != 1024 && memory != 2048 {
			return errors.New("--memory must be 1024 or 2048")
		}
		if len(ports) > 16 {
			return errors.New("--ports supports at most 16 ports")
		}
		seen := make(map[int]bool, len(ports))
		for _, port := range ports {
			if port < 1 || port > 65532 || seen[port] {
				return errors.New("--ports must contain unique ports between 1 and 65532")
			}
			seen[port] = true
		}
		if key != "" && templateID == "" {
			return errors.New("--request-id requires --template so retrying targets the same template")
		}
		id := uuid.New()
		if templateID != "" {
			parsed, err := uuid.Parse(templateID)
			if err != nil {
				return err
			}
			id = parsed
		}
		request, err := requestID(key)
		if err != nil {
			return err
		}
		source, err := sandbox.PackageDirectory(directory)
		if err != nil {
			return err
		}
		project, err := c.project()
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "Template ID: %s\nRequest ID: %s\n", id, request)
		deployment, err := project.API.DeploySandbox(cmd.Context(), project.ProjectID, id, request, api.SandboxDeployInput{Name: args[0], MemoryMB: memory, Ports: ports, SourceArchive: source})
		if err != nil {
			return err
		}
		return c.write(cmd, struct {
			TemplateID uuid.UUID                    `json:"template_id"`
			Deployment *apiclient.SandboxDeployment `json:"deployment"`
		}{id, deployment})
	}}
	cmd.Flags().StringVar(&directory, "path", ".", "Build context directory containing Dockerfile")
	cmd.Flags().StringVar(&templateID, "template", "", "Stable template UUID; omit to create a new template")
	cmd.Flags().StringVar(&key, "request-id", "", "UUID idempotency key (requires --template)")
	cmd.Flags().IntVar(&memory, "memory", 1024, "Memory in MB")
	cmd.Flags().IntSliceVar(&ports, "ports", nil, "Comma-separated HTTP ports exposed by the template")
	return cmd
}

func (c *commands) deployments() *cobra.Command {
	cmd := &cobra.Command{Use: "deployments", Short: "Inspect custom template deployments"}
	var cursor string
	var limit int
	list := &cobra.Command{Use: "list <template-id>", Short: "List deployment history", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if limit < 1 || limit > 100 {
			return errors.New("--limit must be between 1 and 100")
		}
		id, err := uuid.Parse(args[0])
		if err != nil {
			return err
		}
		project, err := c.project()
		if err != nil {
			return err
		}
		value, err := project.API.SandboxDeployments(cmd.Context(), project.ProjectID, id, cursor, limit)
		if err != nil {
			return err
		}
		return c.write(cmd, value)
	}}
	list.Flags().StringVar(&cursor, "cursor", "", "Continue from a returned pagination cursor")
	list.Flags().IntVar(&limit, "limit", 10, "Deployment history page size (1–100)")
	cmd.AddCommand(list, c.deploymentRead(false), c.deploymentRead(true), c.deploymentLogs())
	return cmd
}

func (c *commands) deploymentRead(source bool) *cobra.Command {
	use, description := "get", "Read deployment status"
	if source {
		use, description = "source", "Write the original tar.gz build context to stdout"
	}
	return &cobra.Command{Use: use + " <template-id> <deployment-id>", Short: description, Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		template, err := uuid.Parse(args[0])
		if err != nil {
			return err
		}
		deployment, err := uuid.Parse(args[1])
		if err != nil {
			return err
		}
		project, err := c.project()
		if err != nil {
			return err
		}
		if source {
			if c.json {
				return errors.New("source returns tar.gz bytes; omit --json")
			}
			return project.API.SandboxDeploymentSource(cmd.Context(), project.ProjectID, template, deployment, cmd.OutOrStdout())
		}
		value, err := project.API.SandboxDeployment(cmd.Context(), project.ProjectID, template, deployment)
		if err != nil {
			return err
		}
		return c.write(cmd, value)
	}}
}

func (c *commands) deploymentLogs() *cobra.Command {
	var region, cursor string
	var limit int
	cmd := &cobra.Command{Use: "logs <template-id> <deployment-id>", Short: "Read regional build logs", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if region == "" {
			return errors.New("--region is required")
		}
		if limit < 1 || limit > 1000 {
			return errors.New("--limit must be between 1 and 1000")
		}
		template, err := uuid.Parse(args[0])
		if err != nil {
			return err
		}
		deployment, err := uuid.Parse(args[1])
		if err != nil {
			return err
		}
		project, err := c.project()
		if err != nil {
			return err
		}
		params := &apiclient.GetSandboxDeploymentLogsParams{Region: region, Limit: &limit}
		if cursor != "" {
			params.Cursor = &cursor
		}
		value, err := project.API.SandboxDeploymentLogs(cmd.Context(), project.ProjectID, template, deployment, params)
		if err != nil {
			return err
		}
		return c.write(cmd, value)
	}}
	cmd.Flags().StringVar(&region, "region", "", "Deployment region, for example aws-us-east-1 (required)")
	cmd.Flags().StringVar(&cursor, "cursor", "", "Continue from a returned next_cursor")
	cmd.Flags().IntVar(&limit, "limit", 100, "Maximum log events (1–1000)")
	_ = cmd.MarkFlagRequired("region")
	return cmd
}
