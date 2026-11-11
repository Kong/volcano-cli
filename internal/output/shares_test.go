package output

import (
	"bytes"
	"testing"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

var outputReleaseID = uuid.MustParse("0b6c1f1e-4a55-4d6f-9c3e-5d2f7a1b9e10")

func outputShare() *apiclient.Share {
	return &apiclient.Share{
		Id:            "k3Vn8TqPz0aX",
		SuggestedName: "notes-app",
		Status:        apiclient.ShareStatusActive,
		CreatedAt:     time.Now().Add(-time.Hour),
		UpdatedAt:     time.Now(),
	}
}

func publishedShare() *apiclient.Share {
	link := outputShare()
	link.CurrentRelease = &apiclient.ShareReleaseRef{
		Id: outputReleaseID, PublishedAt: time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC),
	}
	return link
}

func TestShareSnippets(t *testing.T) {
	assert.Equal(t, "https://volcano.dev/deploy/k3Vn8TqPz0aX", ShareURL("k3Vn8TqPz0aX"))
	assert.Equal(t,
		"[![Deploy with Volcano](https://volcano.dev/deploy-button.svg)](https://volcano.dev/deploy/k3Vn8TqPz0aX)",
		ShareMarkdownSnippet("k3Vn8TqPz0aX"))
	assert.Equal(t,
		`<a href="https://volcano.dev/deploy/k3Vn8TqPz0aX"><img src="https://volcano.dev/deploy-button.svg" alt="Deploy with Volcano"></a>`,
		ShareHTMLSnippet("k3Vn8TqPz0aX"))
}

// An id is a path segment, so one with markup or a slash cannot reshape the
// address or the snippets.
func TestShareURLEscapesTheID(t *testing.T) {
	assert.Equal(t, "https://volcano.dev/deploy/a%2Fb%22%3E", ShareURL(`a/b">`))
}

func TestShareSnippetsShowTheButton(t *testing.T) {
	var out bytes.Buffer
	ShareSnippets(&out, "k3Vn8TqPz0aX")
	for _, want := range []string{"Markdown", "[![Deploy with Volcano]", "HTML", `<a href="https://volcano.dev/deploy/k3Vn8TqPz0aX">`} {
		assert.Contains(t, out.String(), want)
	}
}

func TestSharingStateSaysWhyAShareIsOffOrNotServing(t *testing.T) {
	reason := func(value string) *string { return &value }
	for _, tc := range []struct {
		name  string
		edit  func(*apiclient.Share)
		want  string
		avoid string
	}{
		{name: "serving", edit: func(s *apiclient.Share) { s.Serving = true }, want: "on", avoid: "but"},
		{name: "active draft", edit: func(s *apiclient.Share) { s.CurrentRelease = nil }, want: "nothing is published yet"},
		{name: "active, not serving", edit: func(*apiclient.Share) {}, want: "the link is not serving right now"},
		{name: "owner", edit: func(s *apiclient.Share) {
			s.Status = apiclient.ShareStatusDisabled
			s.DisabledReason = reason("owner")
		}, want: "off (you switched it off)"},
		{name: "operator", edit: func(s *apiclient.Share) {
			s.Status = apiclient.ShareStatusDisabled
			s.DisabledReason = reason("operator")
		}, want: "Volcano took this share down, and only Volcano can switch it on again"},
		{name: "unknown reason", edit: func(s *apiclient.Share) {
			s.Status = apiclient.ShareStatusDisabled
			s.DisabledReason = reason("billing\x1b[31m")
		}, want: "off (billing[31m)"},
		{name: "no reason", edit: func(s *apiclient.Share) { s.Status = apiclient.ShareStatusDisabled }, want: "off"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			share := publishedShare()
			tc.edit(share)
			state := SharingState(share)
			assert.Contains(t, state, tc.want)
			if tc.avoid != "" {
				assert.NotContains(t, state, tc.avoid)
			}
		})
	}
}

func TestShareStatusShowsTheStateLinkAndRelease(t *testing.T) {
	share := publishedShare()
	share.Serving = true
	var out bytes.Buffer
	ShareStatus(&out, share, nil)
	for _, want := range []string{
		"Sharing: on", "Link: https://volcano.dev/deploy/k3Vn8TqPz0aX", "Description: none",
		"Copies shareable: no", "Shared release: " + outputReleaseID.String() + " (published ",
	} {
		assert.Contains(t, out.String(), want)
	}
}

func TestShareStatsShowsTotalsAndDays(t *testing.T) {
	day := func(value string) openapi_types.Date {
		parsed, err := time.Parse("2006-01-02", value)
		require.NoError(t, err)
		return openapi_types.Date{Time: parsed}
	}
	stats := &apiclient.ShareStats{
		Since: time.Date(2026, 9, 19, 21, 20, 0, 0, time.UTC),
		Totals: []apiclient.ShareKindCount{
			{Kind: apiclient.ShareKindCountKindReady, Events: 8},
			{Kind: apiclient.ShareKindCountKindClick, Events: 42, Visitors: 31},
		},
		Daily: []apiclient.ShareDailyCount{
			{Day: day("2026-10-17"), Kind: apiclient.ShareDailyCountKindClick, Events: 3},
			{Day: day("2026-10-18"), Kind: apiclient.ShareDailyCountKindClick, Events: 17},
			{Day: day("2026-10-18"), Kind: apiclient.ShareDailyCountKindReady, Events: 2},
		},
	}

	var out bytes.Buffer
	ShareStats(&out, "k3Vn8TqPz0aX", stats)
	text := out.String()

	assert.Contains(t, text, "Link: k3Vn8TqPz0aX")
	assert.Less(t, indexOf(text, "click   "), indexOf(text, "ready   "), "totals follow the visitor's path, not the API's order")
	assert.Contains(t, text, "42        31")
	assert.Less(t, indexOf(text, "2026-10-18"), indexOf(text, "2026-10-17"), "days are newest first")
}

func TestShareStatsSaysWhenThereIsNoActivity(t *testing.T) {
	var out bytes.Buffer
	ShareStats(&out, "k3Vn8TqPz0aX", &apiclient.ShareStats{})
	assert.Contains(t, out.String(), "No activity in this window")
}

func indexOf(text, substring string) int {
	return bytes.Index([]byte(text), []byte(substring))
}

func outputPreview() *apiclient.SharePreview {
	main := "main"
	return &apiclient.SharePreview{
		Id: "k3Vn8TqPz0aX",
		Release: apiclient.SharePreviewRelease{
			Id:                  outputReleaseID,
			PublishedAt:         time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC),
			Sha256:              "9f2c",
			Frontends:           []string{"web"},
			Functions:           []string{"admin", "api"},
			ServiceKeyFunctions: []string{"admin"},
			Variables: []apiclient.ShareVariable{
				{Name: "OPENAI_API_KEY", Required: true, Secret: true},
				{Name: "NEXT_PUBLIC_TITLE"},
			},
			Omissions: []apiclient.ShareReleaseOmission{{Kind: apiclient.ShareReleaseOmissionKindDatabase, Resource: &main}},
		},
	}
}

func TestSharePreviewShowsTheReleaseWhatItAsksForAndTheServiceKeyWarning(t *testing.T) {
	var out bytes.Buffer
	SharePreview(&out, outputPreview())

	for _, want := range []string{
		"Share: https://volcano.dev/deploy/k3Vn8TqPz0aX",
		"Release: " + outputReleaseID.String() + " (published ", "Digest: sha256:9f2c",
		"Frontends: web", "Functions: admin, api",
		"Variables: OPENAI_API_KEY (starts empty, secret), NEXT_PUBLIC_TITLE (starts empty)",
		"database main: the new project starts without it",
		"These functions receive your new project's service key, which bypasses row-level security: admin.",
		"Volcano has not reviewed this code",
	} {
		assert.Contains(t, out.String(), want)
	}
}

func TestSharePreviewWithoutServiceKeyFunctionsHasNoWarning(t *testing.T) {
	preview := outputPreview()
	preview.Release.ServiceKeyFunctions = []string{}
	var out bytes.Buffer
	SharePreview(&out, preview)
	assert.NotContains(t, out.String(), "service key")
}

func TestShareReleaseChangesListsOnlyWhatChanged(t *testing.T) {
	before := outputPreview().Release
	after := outputPreview().Release
	after.Id = uuid.MustParse("1b6c1f1e-4a55-4d6f-9c3e-5d2f7a1b9e10")
	after.Functions = []string{"admin", "api", "worker"}
	after.ServiceKeyFunctions = []string{"admin", "worker"}
	after.Variables = []apiclient.ShareVariable{{Name: "OPENAI_API_KEY", Required: true, Secret: true}}

	var out bytes.Buffer
	ShareReleaseChanges(&out, &before, &after)
	text := out.String()

	assert.Contains(t, text, "was "+outputReleaseID.String())
	assert.Contains(t, text, "Functions: +worker")
	assert.Contains(t, text, "Service key: +worker")
	assert.Contains(t, text, "Variables: -NEXT_PUBLIC_TITLE (starts empty)")
	assert.NotContains(t, text, "Frontends", "unchanged frontends are not listed")
}

func TestShareReleaseDraftShowsEverythingTheOwnerConfirms(t *testing.T) {
	visibility, path := "private", "functions/admin/.env"
	draft := &apiclient.ShareReleaseDraft{
		Functions: []apiclient.ShareReleaseFunction{
			{Name: "admin", Kind: apiclient.ShareReleaseFunctionKindStandard, Visibility: &visibility},
		},
		Frontends:           []string{},
		Variables:           []apiclient.ShareVariable{{Name: "OPENAI_API_KEY", Required: true, Secret: true}},
		ServiceKeyFunctions: []string{"admin"},
		Omissions:           []apiclient.ShareReleaseOmission{{Kind: apiclient.ShareReleaseOmissionKindSecretFile, Path: &path}},
	}
	var out bytes.Buffer
	ShareReleaseDraft(&out, draft)

	for _, want := range []string{
		"Functions: admin (standard, private)", "Frontends: none", "Variables: OPENAI_API_KEY (starts empty, secret)",
		"Service key: admin", "functions/admin/.env: dotenv and key files are never released",
	} {
		assert.Contains(t, out.String(), want)
	}
}

func TestShareReleasesListsRowsOrSaysThereAreNone(t *testing.T) {
	var out bytes.Buffer
	ShareReleases(&out, nil)
	assert.Equal(t, "No releases published yet\n", out.String())

	out.Reset()
	ShareReleases(&out, []apiclient.ShareRelease{{
		Id: outputReleaseID, PublishedAt: time.Now().Add(-time.Hour),
		Functions: []apiclient.ShareReleaseFunction{{Name: "api"}}, Frontends: []string{"web"},
		Variables: []apiclient.ShareVariable{{Name: "OPENAI_API_KEY"}},
	}})
	for _, want := range []string{outputReleaseID.String(), "OPENAI_API_KEY", "Showing 1 release(s)"} {
		assert.Contains(t, out.String(), want)
	}
}

func TestTemplateSourceNamesTheShareAndRelease(t *testing.T) {
	source := &apiclient.ProjectTemplateInstallationSource{
		Kind: apiclient.ProjectTemplateInstallationSourceKindShareRelease, ShareId: "k3Vn8TqPz0aX", ReleaseId: outputReleaseID,
	}
	assert.Equal(t, "share k3Vn8TqPz0aX, release "+outputReleaseID.String(), TemplateSource(source))
}

// The set of failure codes is open: a known one reads in words, an unknown one
// as a generic failure that still names it, and an absent one says so.
func TestInstallationFailureWordsKnownCodesAndFallsBack(t *testing.T) {
	code := func(value string) *string { return &value }
	assert.Contains(t, InstallationFailure(code("source_withdrawn")), "the share was switched off or deleted")
	assert.Contains(t, InstallationFailure(code("source_invalid")), "the release failed Volcano's checks")
	assert.Equal(t, "it failed (brand_new_code)", InstallationFailure(code("brand_new_code")))
	assert.Equal(t, "no reason was recorded", InstallationFailure(nil))
	assert.Equal(t, "no reason was recorded", InstallationFailure(code(" ")))
}
