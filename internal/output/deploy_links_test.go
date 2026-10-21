package output

import (
	"bytes"
	"testing"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

func outputDeployLink() *apiclient.DeployLink {
	commit := "0123456789abcdef0123456789abcdef01234567"
	return &apiclient.DeployLink{
		Id:                  "k3Vn8TqPz0aX",
		RepoFullName:        "acme/notes-app",
		Branch:              "main",
		Path:                "apps/web",
		SuggestedName:       "notes-app",
		Status:              apiclient.DeployLinkStatusActive,
		LastValidatedCommit: &commit,
		CreatedAt:           time.Now().Add(-time.Hour),
		UpdatedAt:           time.Now(),
	}
}

func TestDeployLinkSnippets(t *testing.T) {
	assert.Equal(t, "https://volcano.dev/deploy/k3Vn8TqPz0aX", DeployLinkURL("k3Vn8TqPz0aX"))
	assert.Equal(t,
		"[![Deploy to Volcano](https://volcano.dev/deploy-button.svg)](https://volcano.dev/deploy/k3Vn8TqPz0aX)",
		DeployLinkMarkdownSnippet("k3Vn8TqPz0aX"))
	assert.Equal(t,
		`<a href="https://volcano.dev/deploy/k3Vn8TqPz0aX"><img src="https://volcano.dev/deploy-button.svg" alt="Deploy to Volcano"></a>`,
		DeployLinkHTMLSnippet("k3Vn8TqPz0aX"))
}

// An id is a path segment, so one with markup or a slash cannot reshape the
// address or the snippets.
func TestDeployLinkURLEscapesTheID(t *testing.T) {
	assert.Equal(t, "https://volcano.dev/deploy/a%2Fb%22%3E", DeployLinkURL(`a/b">`))
}

func TestDeployLinkCreatedShowsAddressAndSnippets(t *testing.T) {
	var out bytes.Buffer
	DeployLinkCreated(&out, outputDeployLink())

	for _, want := range []string{
		"Created deploy link k3Vn8TqPz0aX",
		"URL: https://volcano.dev/deploy/k3Vn8TqPz0aX",
		"Repository: acme/notes-app", "Path: apps/web", "Suggested name: notes-app",
		"Markdown", "[![Deploy to Volcano]", "HTML", `<a href="https://volcano.dev/deploy/k3Vn8TqPz0aX">`,
	} {
		assert.Contains(t, out.String(), want)
	}
}

func TestDeployLinkShowsWhyADisabledLinkIsOff(t *testing.T) {
	link := outputDeployLink()
	link.Status = apiclient.DeployLinkStatusDisabled
	reason := apiclient.RepositoryPrivate
	link.DisabledReason = &reason

	var out bytes.Buffer
	DeployLink(&out, link)

	assert.Contains(t, out.String(), "Disabled reason: repository_private")
}

func TestDeployLinksListsRowsOrSaysThereAreNone(t *testing.T) {
	var out bytes.Buffer
	DeployLinks(&out, nil)
	assert.Equal(t, "No deploy links found\n", out.String())

	out.Reset()
	DeployLinks(&out, []apiclient.DeployLink{*outputDeployLink()})
	for _, want := range []string{"k3Vn8TqPz0aX", "acme/notes-app", "main", "active", "Showing 1 deploy link(s)"} {
		assert.Contains(t, out.String(), want)
	}
}

func TestDeployLinkStatsShowsTotalsAndDays(t *testing.T) {
	day := func(value string) openapi_types.Date {
		parsed, err := time.Parse("2006-01-02", value)
		require.NoError(t, err)
		return openapi_types.Date{Time: parsed}
	}
	stats := &apiclient.DeployLinkStats{
		Since: time.Date(2026, 9, 19, 21, 20, 0, 0, time.UTC),
		Totals: []apiclient.DeployLinkKindCount{
			{Kind: apiclient.DeployLinkKindCountKindReady, Events: 8},
			{Kind: apiclient.DeployLinkKindCountKindClick, Events: 42, Visitors: 31},
		},
		Daily: []apiclient.DeployLinkDailyCount{
			{Day: day("2026-10-17"), Kind: apiclient.DeployLinkDailyCountKindClick, Events: 3},
			{Day: day("2026-10-18"), Kind: apiclient.DeployLinkDailyCountKindClick, Events: 17},
			{Day: day("2026-10-18"), Kind: apiclient.DeployLinkDailyCountKindReady, Events: 2},
		},
	}

	var out bytes.Buffer
	DeployLinkStats(&out, "k3Vn8TqPz0aX", stats)
	text := out.String()

	assert.Contains(t, text, "Link: k3Vn8TqPz0aX")
	assert.Less(t, indexOf(text, "click   "), indexOf(text, "ready   "), "totals follow the visitor's path, not the API's order")
	assert.Contains(t, text, "42        31")
	assert.Less(t, indexOf(text, "2026-10-18"), indexOf(text, "2026-10-17"), "days are newest first")
}

func TestDeployLinkStatsSaysWhenThereIsNoActivity(t *testing.T) {
	var out bytes.Buffer
	DeployLinkStats(&out, "k3Vn8TqPz0aX", &apiclient.DeployLinkStats{})
	assert.Contains(t, out.String(), "No activity in this window")
}

func indexOf(text, substring string) int {
	return bytes.Index([]byte(text), []byte(substring))
}
