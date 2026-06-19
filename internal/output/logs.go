package output

import (
	"io"
	"strings"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// LogsFetcher fetches one page of log events. An empty cursor requests the first page.
type LogsFetcher func(cursor string) (*apiclient.ListLogsResponse, error)

// SearchLogsFetcher fetches one page of searched log events. An empty cursor requests the first page.
type SearchLogsFetcher func(cursor string) (*apiclient.LogSearchResponse, error)

// PrintLogs renders paginated log events from fetch until the response signals
// no more pages or the cursor cannot be advanced.
func PrintLogs(w io.Writer, fetch LogsFetcher) error {
	cursor := ""
	for {
		resp, err := fetch(cursor)
		if err != nil {
			return err
		}
		if resp == nil {
			return nil
		}
		LogEvents(w, resp.Data)
		if !resp.HasMore || resp.NextCursor == nil {
			return nil
		}
		cursor = strings.TrimSpace(*resp.NextCursor)
		if cursor == "" {
			return nil
		}
	}
}

// PrintSearchLogs renders paginated searched log events from fetch until the
// response signals no more pages or the cursor cannot be advanced.
func PrintSearchLogs(w io.Writer, fetch SearchLogsFetcher) error {
	cursor := ""
	for {
		resp, err := fetch(cursor)
		if err != nil {
			return err
		}
		if resp == nil {
			return nil
		}
		LogSearchEvents(w, resp.Data)
		if !resp.HasMore || resp.NextCursor == nil {
			return nil
		}
		cursor = strings.TrimSpace(*resp.NextCursor)
		if cursor == "" {
			return nil
		}
	}
}
