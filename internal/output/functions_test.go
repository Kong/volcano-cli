package output

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// Regression coverage for the body/message field rename: the API's LogEvent
// body is untyped JSON (string, object, array, number, or bool), not a plain
// "message" string, so these decode the same wire shape the server sends.

func TestLogEventsRendersStringBody(t *testing.T) {
	var events []apiclient.LogEvent
	raw := `[{"timestamp":"2026-07-30T10:11:20-04:00","body":"next build failed: some error"}]`
	require.NoError(t, json.Unmarshal([]byte(raw), &events))

	var out bytes.Buffer
	LogEvents(&out, events)
	assert.Contains(t, out.String(), "next build failed: some error")
}

func TestLogEventsRendersNonStringBodyAsJSON(t *testing.T) {
	var events []apiclient.LogEvent
	raw := `[{"timestamp":"2026-07-30T10:11:20-04:00","body":{"phase":"BUILD","status":"FAILED"}}]`
	require.NoError(t, json.Unmarshal([]byte(raw), &events))

	var out bytes.Buffer
	LogEvents(&out, events)
	assert.Contains(t, out.String(), `{"phase":"BUILD","status":"FAILED"}`)
}

func TestLogSearchEventsRendersStringBody(t *testing.T) {
	var events []apiclient.LogSearchEvent
	raw := `[{"id":"evt-1","timestamp":"2026-07-30T10:11:20-04:00","resource":{"type":"frontend","id":"` + outputProjectID + `"},"body":"hello from build"}]`
	require.NoError(t, json.Unmarshal([]byte(raw), &events))

	var out bytes.Buffer
	LogSearchEvents(&out, events)
	assert.Contains(t, out.String(), "hello from build")
}

func TestLogEventBodyTextHandlesNil(t *testing.T) {
	assert.Empty(t, logEventBodyText(nil))
	assert.Empty(t, logSearchEventBodyText(nil))
}

func TestLogEventsRendersEveryBodyVariant(t *testing.T) {
	cases := []struct {
		name     string
		bodyJSON string
		want     string
	}{
		{name: "array", bodyJSON: `["a","b"]`, want: `["a","b"]`},
		{name: "number", bodyJSON: `42`, want: "42"},
		{name: "boolean", bodyJSON: `true`, want: "true"},
		{name: "null", bodyJSON: `null`, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var events []apiclient.LogEvent
			raw := `[{"timestamp":"2026-07-30T10:11:20-04:00","body":` + tc.bodyJSON + `}]`
			require.NoError(t, json.Unmarshal([]byte(raw), &events))

			var out bytes.Buffer
			LogEvents(&out, events)
			if tc.want == "" {
				assert.NotContains(t, out.String(), "null")
				return
			}
			assert.Contains(t, out.String(), tc.want)
		})
	}
}

// A server that predates levels sends only is_public, and its false let
// signed-in users in, which is the authenticated level now.
func TestFunctionVisibilityFallsBackToIsPublic(t *testing.T) {
	for _, tc := range []struct {
		visibility apiclient.FunctionVisibility
		isPublic   bool
		want       string
	}{
		{visibility: apiclient.FunctionVisibilityPrivate, want: "private"},
		{visibility: apiclient.FunctionVisibilityAuthenticated, want: "authenticated"},
		{visibility: apiclient.FunctionVisibilityPublic, isPublic: true, want: "public"},
		{isPublic: true, want: "public"},
		{isPublic: false, want: "authenticated"},
	} {
		assert.Equal(t, tc.want, FunctionVisibility(tc.visibility, tc.isPublic))
	}
}

func TestFunctionShowsTheRoutesThatReachIt(t *testing.T) {
	fn := apiclient.Function{Name: "session", Visibility: apiclient.FunctionVisibilityPublic, IsPublic: true}

	var routed bytes.Buffer
	Function(&routed, &fn, []FunctionRouteSource{
		{Frontend: "admin", PathPrefix: "/api"},
		{Frontend: "web", PathPrefix: "/api/session"},
	})
	assert.Contains(t, routed.String(), "Visibility: public")
	assert.Contains(t, routed.String(), "Routed from: admin /api, web /api/session")

	var unrouted bytes.Buffer
	Function(&unrouted, &fn, nil)
	assert.NotContains(t, unrouted.String(), "Routed from")
}
