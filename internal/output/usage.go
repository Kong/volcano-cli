package output

import (
	"fmt"
	"io"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/theme"
)

// ProjectUsage renders aggregate current-month and all-time totals. The API's
// hourly and daily series are intentionally not rendered until the command has
// an explicit detailed mode.
func ProjectUsage(w io.Writer, usage *apiclient.ProjectUsageResponse) {
	if usage == nil {
		return
	}

	on := theme.On(w)
	kv(w, on, "Month", "%s", usage.Month)
	if len(usage.Metrics) == 0 {
		fmt.Fprintln(w, "No usage metrics recorded")
		return
	}

	tableHead(w, on, true, 72, "%-36s  %-16s  %-16s", "Metric", "Current month", "All time")
	for _, metric := range usage.Metrics {
		fmt.Fprintf(w, "%-36s  %-16d  %-16d\n", Truncate(metric.Metric, 36), metric.Total, metric.AllTime)
	}
}
