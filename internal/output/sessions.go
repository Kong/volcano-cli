package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/theme"
)

// AccountSessionsPage is one page of the account's sign-in sessions, as the
// sessions list renders it.
type AccountSessionsPage struct {
	Sessions   []apiclient.AccountSession
	Page       int
	Limit      int
	Total      int
	TotalPages int
	// NextCommand lists the next page; empty on the last one.
	NextCommand string
}

// AccountSessions renders the account's sign-in sessions, browser and CLI
// alike, marking the one this CLI is logged in with.
func AccountSessions(w io.Writer, page AccountSessionsPage) {
	on := theme.On(w)
	if len(page.Sessions) == 0 {
		if page.Total == 0 {
			fmt.Fprintln(w, "No sessions")
		} else {
			fmt.Fprintf(w, "No sessions on page %d\n", page.Page)
		}
		summary(w, on, "Showing 0 of %d session(s) (page %d, limit %d)", page.Total, page.Page, page.Limit)
		return
	}

	tableHead(w, on, true, 112, "%-36s  %-8s  %-30s  %-15s  %-14s  %s",
		"ID", "Kind", "Device", "IP address", "Last active", "")
	for _, session := range page.Sessions {
		current := ""
		if session.IsCurrent {
			current = theme.Command("this CLI", on)
		}
		fmt.Fprintf(w, "%-36s  %-8s  %-30s  %-15s  %-14s  %s\n",
			session.Id.String(),
			sessionKind(session.Kind),
			Truncate(sessionDevice(session.UserAgent), 30),
			Truncate(sessionIP(session), 15),
			sessionLastActive(session),
			current,
		)
	}
	summary(w, on, "Showing %d of %d session(s) (page %d, limit %d)",
		len(page.Sessions), page.Total, page.Page, page.Limit)
	if page.NextCommand != "" {
		nextPage(w, on, page.NextCommand)
	}
}

// sessionKind tells a CLI login from a browser sign-in.
func sessionKind(kind apiclient.AccountSessionKind) string {
	if kind == apiclient.AccountSessionKindCli {
		return "CLI"
	}
	return "Browser"
}

// sessionDevice is the user agent the signing-in client sent. Any client can
// send any text, so control characters are dropped before it reaches the
// terminal, where they could rewrite or hide rows.
func sessionDevice(userAgent *string) string {
	if userAgent == nil {
		return "unknown"
	}
	if device := strings.TrimSpace(theme.StripControl(*userAgent)); device != "" {
		return device
	}
	return "unknown"
}

func sessionIP(session apiclient.AccountSession) string {
	for _, ip := range []*string{session.LastIpAddress, session.IpAddress} {
		if ip == nil {
			continue
		}
		if address := strings.TrimSpace(theme.StripControl(*ip)); address != "" {
			return address
		}
	}
	return "unknown"
}

func sessionLastActive(session apiclient.AccountSession) string {
	switch {
	case session.LastActivityAt != nil:
		return FormatTimeAgo(*session.LastActivityAt)
	case session.SessionStartedAt != nil:
		return FormatTimeAgo(*session.SessionStartedAt)
	default:
		return "unknown"
	}
}
