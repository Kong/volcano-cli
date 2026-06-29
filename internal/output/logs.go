package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
)

// SearchLogsFetcher fetches one page of searched log events. An empty cursor requests the first page.
type SearchLogsFetcher func(cursor string) (*apiclient.LogSearchResponse, error)

// PrintSearchLogs renders paginated searched log events from fetch until the
// response signals no more pages or the cursor cannot be advanced.
func PrintSearchLogs(w io.Writer, fetch SearchLogsFetcher) error {
	return PrintSearchLogsSkipping(w, fetch, nil)
}

// PrintSearchLogsSkipping renders paginated searched log events, excluding IDs
// already present in skip.
func PrintSearchLogsSkipping(w io.Writer, fetch SearchLogsFetcher, skip map[string]struct{}) error {
	cursor := ""
	for {
		resp, err := fetch(cursor)
		if err != nil {
			return err
		}
		if resp == nil {
			return nil
		}
		LogSearchEvents(w, filterSkippedLogSearchEvents(resp.Data, skip))
		if !resp.HasMore || resp.NextCursor == nil {
			return nil
		}
		cursor = strings.TrimSpace(*resp.NextCursor)
		if cursor == "" {
			return nil
		}
	}
}

func filterSkippedLogSearchEvents(events []apiclient.LogSearchEvent, skip map[string]struct{}) []apiclient.LogSearchEvent {
	if len(events) == 0 || len(skip) == 0 {
		return events
	}
	filtered := make([]apiclient.LogSearchEvent, 0, len(events))
	for _, event := range events {
		if _, ok := skip[event.Id]; ok && event.Id != "" {
			continue
		}
		filtered = append(filtered, event)
	}
	return filtered
}

// PrintLogStreamEvent renders one parsed project log stream event.
func PrintLogStreamEvent(w io.Writer, event *api.ProjectLogStreamEvent) {
	if event == nil {
		return
	}
	switch {
	case event.Log != nil:
		LogSearchEvents(w, []apiclient.LogSearchEvent{*event.Log})
	case event.Warning != "":
		fmt.Fprintf(w, "Warning: %s\n", event.Warning)
	}
}
