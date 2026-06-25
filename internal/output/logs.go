package output

import (
	"io"
	"strings"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// SearchLogsFetcher fetches one page of searched log events. An empty cursor requests the first page.
type SearchLogsFetcher func(cursor string) (*apiclient.LogSearchResponse, error)

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
