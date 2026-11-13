package output

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/theme"
)

// ProjectLogEvents renders events from a read that can span many resources, so
// each line names its level and the resource it came from.
func ProjectLogEvents(w io.Writer, events []apiclient.LogSearchEvent) {
	on := theme.On(w)
	for _, event := range events {
		printProjectLogEvent(w, on, event)
	}
}

func printProjectLogEvent(w io.Writer, on bool, event apiclient.LogSearchEvent) {
	parts := []string{theme.Dim(FormatTimestamp(event.Timestamp), on)}
	if region := stringPtrValue(event.Region); region != "" {
		parts = append(parts, theme.Dim("["+region+"]", on))
	}
	if event.Level != nil && *event.Level != "" {
		parts = append(parts, string(*event.Level))
	}
	resource := event.Resource.Name
	if resource == nil || *resource == "" {
		id := event.Resource.Id.String()
		resource = &id
	}
	parts = append(parts, *resource+":", strings.TrimSpace(logSearchEventBodyText(event.Body)))
	fmt.Fprintln(w, strings.Join(parts, " "))
}

// ProjectLogEventsJSON writes one JSON object per event, so a script can read
// a page or a live tail the same way.
func ProjectLogEventsJSON(w io.Writer, events []apiclient.LogSearchEvent) error {
	encoder := json.NewEncoder(w)
	for _, event := range events {
		if err := encoder.Encode(event); err != nil {
			return err
		}
	}
	return nil
}

// ProjectLogStreamEvent renders one live-tail event like a search result. A
// stream warning goes to warnings, so JSON output stays one event per line.
func ProjectLogStreamEvent(w, warnings io.Writer, event *api.ProjectLogStreamEvent, jsonOutput bool) error {
	switch {
	case event == nil:
		return nil
	case event.Log != nil && jsonOutput:
		return ProjectLogEventsJSON(w, []apiclient.LogSearchEvent{*event.Log})
	case event.Log != nil:
		ProjectLogEvents(w, []apiclient.LogSearchEvent{*event.Log})
	case event.Warning != "":
		Warning(warnings, "%s", event.Warning)
	}
	return nil
}

// LogActivity renders each bucket's count with its levels, highest level first,
// then the total.
func LogActivity(w io.Writer, activity *apiclient.LogActivityResponse, levels []string) {
	on := theme.On(w)
	for _, bucket := range activity.Data {
		var counts []string
		for _, level := range slices.Backward(levels) {
			if count := bucket.Counts.Levels[level]; count > 0 {
				counts = append(counts, fmt.Sprintf("%s %d", level, count))
			}
		}
		line := fmt.Sprintf("%s  %6d", theme.Dim(FormatTimestamp(bucket.StartTime), on), bucket.Total)
		if len(counts) > 0 {
			line += "  " + theme.Dim(strings.Join(counts, ", "), on)
		}
		fmt.Fprintln(w, line)
	}
	summary(w, on, "%d logs", activity.Total)
}
