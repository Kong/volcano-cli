package sandboxes

import (
	"errors"
	"slices"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

func (c *commands) usage() *cobra.Command {
	return &cobra.Command{Use: "usage", Short: "Show Sandbox preview usage; no credits are charged", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		project, err := c.project()
		if err != nil {
			return err
		}
		value, err := project.API.SandboxUsage(cmd.Context(), project.ProjectID)
		if err != nil {
			return err
		}
		names := []string{"Sandbox Running (MiB-Seconds)", "Sandbox Suspended (Seconds)", "Sandbox Uncertain (MiB-Seconds)"}
		metrics := make([]apiclient.MetricUsageData, 0, len(names))
		for _, m := range value.Metrics {
			if slices.Contains(names, m.Metric) {
				metrics = append(metrics, m)
			}
		}
		if len(metrics) != len(names) {
			return errors.New("this server does not expose Sandbox preview usage yet")
		}
		value.Metrics = metrics
		value.Frontends = nil
		return c.write(cmd, value)
	}}
}
