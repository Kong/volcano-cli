package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/theme"
)

// ServiceKeys renders one service-key page, including key values returned by a
// successful API response. The API wrapper deliberately strips response bodies
// from errors, so this renderer only receives a secret on the success path.
func ServiceKeys(w io.Writer, page *apiclient.PaginatedServiceKeys, projectID string, commandPrefix ...string) {
	if page == nil {
		page = &apiclient.PaginatedServiceKeys{}
	}

	if len(page.Data) == 0 {
		if page.Total == 0 {
			fmt.Fprintln(w, "No service keys created")
		} else {
			fmt.Fprintf(w, "No service keys found on page %d\n", page.Page)
		}
		return
	}

	for i := range page.Data {
		if i > 0 {
			fmt.Fprintln(w)
		}
		ServiceKey(w, &page.Data[i])
	}
	summary(w, theme.On(w), "Showing %d of %d service key(s) (page %d, limit %d)", len(page.Data), page.Total, page.Page, page.Limit)
	if page.HasMore {
		command := commandPathPrefix(commandPrefix) + " projects service-keys list"
		if projectID != "" {
			command += " " + projectID
		}
		nextPage(w, theme.On(w), fmt.Sprintf("%s --page %d --limit %d", command, page.Page+1, page.Limit))
	}
}

// ServiceKey renders service-key metadata and the key value when the API
// returned it. A nil key value is omitted because list/detail responses from a
// future server may intentionally redact it.
func ServiceKey(w io.Writer, key *apiclient.ServiceKey) {
	on := theme.On(w)
	kv(w, on, "ID", "%s", key.Id.String())
	kv(w, on, "Name", "%s", key.Name)
	kv(w, on, "Prefix", "%s", key.KeyPrefix)
	kv(w, on, "Permissions", "%s", strings.Join(key.Permissions, ", "))
	if key.KeyValue != nil {
		kv(w, on, "Key value", "%s", *key.KeyValue)
	}
	if key.CreatedAt != nil {
		kv(w, on, "Created", "%s", FormatTimestamp(*key.CreatedAt))
	}
	if key.UpdatedAt != nil {
		kv(w, on, "Updated", "%s", FormatTimestamp(*key.UpdatedAt))
	}
}
