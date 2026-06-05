package output

import (
	"io"
	"net/url"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// LogsFetcher fetches one page of log events. An empty nextToken requests the first page.
type LogsFetcher func(nextToken string) (*apiclient.GetLogsResponse, error)

// PrintLogs renders paginated log events from fetch until the response signals
// no more pages or the next-token cursor cannot be advanced.
func PrintLogs(w io.Writer, fetch LogsFetcher) error {
	nextToken := ""
	for {
		resp, err := fetch(nextToken)
		if err != nil {
			return err
		}
		if resp == nil {
			return nil
		}
		LogEvents(w, resp.Data)
		LogPartialWarning(w, resp)
		if !resp.HasMore || resp.Next == nil {
			return nil
		}
		nextToken = extractNextToken(*resp.Next)
		if nextToken == "" {
			return nil
		}
	}
}

func extractNextToken(nextURL string) string {
	// url.Parse accepts any well-formed cursor URL the server emits, so the
	// previous manual next_token= scan was unreachable in practice.
	parsed, err := url.Parse(nextURL)
	if err != nil {
		return ""
	}
	return parsed.Query().Get("next_token")
}
