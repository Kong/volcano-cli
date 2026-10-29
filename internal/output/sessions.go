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
	Sessions   []apiclient.AuthSession
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
			sessionKind(string(session.Provider)),
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

// sessionKind tells a CLI login from a browser sign-in. A CLI login is a
// device grant, whose provider names the device client.
func sessionKind(provider string) string {
	if strings.HasPrefix(provider, "device:") {
		return "CLI"
	}
	return "Browser"
}

func sessionDevice(userAgent *string) string {
	if userAgent == nil || strings.TrimSpace(*userAgent) == "" {
		return "unknown"
	}
	return strings.TrimSpace(*userAgent)
}

func sessionIP(session apiclient.AuthSession) string {
	for _, ip := range []*string{session.LastIpAddress, session.IpAddress} {
		if ip != nil && strings.TrimSpace(*ip) != "" {
			return strings.TrimSpace(*ip)
		}
	}
	return "unknown"
}

func sessionLastActive(session apiclient.AuthSession) string {
	switch {
	case session.LastActivityAt != nil:
		return FormatTimeAgo(*session.LastActivityAt)
	case session.SessionStartedAt != nil:
		return FormatTimeAgo(*session.SessionStartedAt)
	default:
		return "unknown"
	}
}
