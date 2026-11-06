package git

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliapi "github.com/Kong/volcano-cli/internal/api"
	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const personRefusal = "choosing what a push applies is a person's decision: a project access token can't. " +
	"Sign in with 'volcano login' and run this again"

func TestManifestSectionsShowsWhatAPushApplies(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.manifestSections = []string{"frontends"}

	out, err := executeGitCommand(t, api.serve(), nil, "", "manifest-sections")
	require.NoError(t, err)

	assert.Contains(t, out, "Project: Storefront ("+gitProjectID+")")
	assert.Contains(t, out, "Repository: octo/storefront")
	assert.Contains(t, out, "Manifest sections: frontends\n")
	assert.Contains(t, out, "A push to main applies these once its deploys succeed. It only adds: "+
		"deletes, replacements and wider exposure are left for volcano cloud config deploy.")
	assert.Nil(t, api.sentSectionsBody(), "reading changes nothing")
}

func TestManifestSectionsSaysHowToChooseWhenNoneApply(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"

	out, err := executeGitCommand(t, api.serve(), nil, "", "manifest-sections")
	require.NoError(t, err)

	assert.Contains(t, out, "Manifest sections: none\n")
	assert.Contains(t, out, "A push applies none of volcano-config.yaml.")
	assert.Contains(t, out, "To apply its frontends on push, run volcano git manifest-sections set frontends")
}

// The sections are saved without a repository, and say so: no push arrives
// to apply them until one is connected. The same goes for auto-deploy off.
func TestManifestSectionsSaysWhenNoPushAppliesThem(t *testing.T) {
	for _, tc := range []struct {
		name      string
		connected string
		want      []string
	}{
		{name: "not connected", want: []string{
			"No repository is connected, so no push applies them yet.", "Connect one with volcano git connect",
		}},
		{name: "auto-deploy off", connected: "octo/storefront", want: []string{
			"Auto-deploy is off, so a push to main applies nothing.", "Turn it on with volcano git auto-deploy on",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setGitCommandTestHome(t)
			api := newGitAPI(t)
			api.connected = tc.connected
			api.autoDeploy = false
			api.manifestSections = []string{"frontends"}

			out, err := executeGitCommand(t, api.serve(), nil, "", "manifest-sections")
			require.NoError(t, err)
			assert.Contains(t, out, "Manifest sections: frontends\n")
			for _, want := range tc.want {
				assert.Contains(t, out, want)
			}
			assert.NotContains(t, out, "once its deploys succeed")
		})
	}
}

// --json prints the API's deploy settings, sections included, for a script.
func TestManifestSectionsJSON(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.manifestSections = []string{"frontends"}

	out, err := executeGitCommand(t, api.serve(), nil, "", "manifest-sections", "--json")
	require.NoError(t, err)

	var settings map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &settings), out)
	assert.Equal(t, []any{"frontends"}, settings["manifest_sections"])
	assert.Equal(t, true, settings["auto_deploy_enabled"])
}

// Reading is not a decision, so a project access token may.
func TestManifestSectionsReadsWithAProjectAccessToken(t *testing.T) {
	setGitCommandTestHome(t)
	setGitCommandTestToken(t, cliconfig.ProjectTokenPrefix+"token")
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.manifestSections = []string{"frontends"}

	out, err := executeGitCommand(t, api.serve(), nil, "", "manifest-sections")
	require.NoError(t, err)
	assert.Contains(t, out, "Manifest sections: frontends\n")
}

// Where no repository can be connected, as in local mode, no push arrives to
// apply anything, so both commands say why and nothing is sent.
func TestManifestSectionsWhereNoRepositoryCanBeConnected(t *testing.T) {
	for _, args := range [][]string{
		{"manifest-sections"},
		{"manifest-sections", "set", "frontends", "--yes"},
		{"manifest-sections", "set", "--none"},
	} {
		setGitCommandTestHome(t)
		api := newGitAPI(t)
		api.providerStatus = http.StatusServiceUnavailable

		_, err := executeGitCommand(t, api.serve(), nil, "", args...)
		require.Error(t, err, "%q", args)
		assert.Contains(t, err.Error(), "git provider integration is not configured on this API", "%q", args)
		assert.Contains(t, err.Error(), "If this is local mode, that is expected", "%q", args)
		assert.NotContains(t, err.Error(), "Connect one with", "%q", args)
		assert.Nil(t, api.sentSectionsBody(), "%q", args)
	}
}

// An API older than manifest sections leaves them out of the settings, which
// must not read as "none", and has no route to set them.
func TestManifestSectionsOnAnAPIWithoutThem(t *testing.T) {
	for _, args := range [][]string{{"manifest-sections"}, {"manifest-sections", "set", "frontends", "--yes"}} {
		setGitCommandTestHome(t)
		api := newGitAPI(t)
		api.connected = "octo/storefront"
		api.sectionsUnsupported = true

		_, err := executeGitCommand(t, api.serve(), nil, "", args...)
		require.Error(t, err, "%q", args)
		assert.Contains(t, err.Error(), "this Volcano API can't apply volcano-config.yaml sections on push yet", "%q", args)
	}
}

func TestManifestSectionsSetChoosesTheSections(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"

	out, err := executeGitCommand(t, api.serve(), nil, "", "manifest-sections", "set", "frontends", "--yes")
	require.NoError(t, err)

	assert.Equal(t, map[string]any{"sections": []any{"frontends"}}, api.sentSectionsBody())
	assert.Contains(t, out, "A push now applies frontends from volcano-config.yaml")
	assert.Contains(t, out, "Manifest sections: frontends\n")
	assert.Contains(t, out, "Nothing was applied.")
	assert.Contains(t, out, "A push to main applies these once its deploys succeed.")
	assert.NotContains(t, out, "This lets", "--yes skips the warning")
}

// Choosing a section lets anyone who can push change it, so that is put to
// the person first, naming the repository and branch.
func TestManifestSectionsSetAsksBeforeAddingASection(t *testing.T) {
	for _, tc := range []struct {
		answer string
		sent   bool
	}{{answer: "y\n", sent: true}, {answer: "n\n", sent: false}} {
		setGitCommandTestHome(t)
		api := newGitAPI(t)
		api.connected = "octo/storefront"

		out, err := executeGitCommand(t, api.serve(), nil, tc.answer, "manifest-sections", "set", "frontends")
		require.NoError(t, err, tc.answer)

		assert.Contains(t, out, "This lets anyone who can push to main on octo/storefront change the project's "+
			"frontends through volcano-config.yaml. A push only adds: it never deletes, replaces, or widens exposure.")
		assert.Contains(t, out, "Apply frontends on push?")
		if tc.sent {
			assert.NotNil(t, api.sentSectionsBody())
		} else {
			assert.Nil(t, api.sentSectionsBody(), "a declined change is not sent")
		}
	}
}

// Taking a section away gives nobody anything, so it is not asked about.
func TestManifestSectionsSetNoneDoesNotAsk(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.manifestSections = []string{"frontends"}

	out, err := executeGitCommand(t, api.serve(), nil, "", "manifest-sections", "set", "--none")
	require.NoError(t, err)

	assert.Equal(t, map[string]any{"sections": []any{}}, api.sentSectionsBody())
	assert.NotContains(t, out, "This lets")
	assert.Contains(t, out, "A push now applies no volcano-config.yaml sections")
	assert.Contains(t, out, "Manifest sections: none\n")
	assert.NotContains(t, out, "manifest-sections set frontends", "the command that just ran is not suggested back")
}

// A section already applied is not asked about again.
func TestManifestSectionsSetKeepingASectionDoesNotAsk(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.manifestSections = []string{"frontends"}

	out, err := executeGitCommand(t, api.serve(), nil, "", "manifest-sections", "set", "frontends")
	require.NoError(t, err)
	assert.NotContains(t, out, "This lets")
	assert.Equal(t, map[string]any{"sections": []any{"frontends"}}, api.sentSectionsBody())
}

// A project with nothing connected can choose its sections ahead of a
// repository; the warning then names whoever will push to it.
func TestManifestSectionsSetBeforeARepositoryIsConnected(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	out, err := executeGitCommand(t, api.serve(), nil, "y\n", "manifest-sections", "set", "frontends")
	require.NoError(t, err)

	assert.Contains(t, out, "This lets anyone who can push to the production branch of the repository you connect")
	assert.Equal(t, map[string]any{"sections": []any{"frontends"}}, api.sentSectionsBody())
	assert.Contains(t, out, "No repository is connected, so no push applies them yet.")
}

// A project access token is refused before anything is asked or sent, with
// the exit code of a refused credential, and says how a person signs in.
func TestManifestSectionsSetRefusesAProjectAccessToken(t *testing.T) {
	setGitCommandTestHome(t)
	setGitCommandTestToken(t, cliconfig.ProjectTokenPrefix+"token")
	api := newGitAPI(t)
	api.connected = "octo/storefront"

	out, err := executeGitCommand(t, api.serve(), nil, "y\n", "manifest-sections", "set", "frontends")
	require.Error(t, err)

	require.ErrorIs(t, err, cliconfig.ErrAccountTokenRequired)
	assert.Equal(t, personRefusal, err.Error())
	assert.NotContains(t, out, "This lets", "nothing is asked")
	assert.Nil(t, api.sentSectionsBody())
}

// A token in VOLCANO_TOKEN is used instead of a sign-in, so the refusal says
// to unset it.
func TestManifestSectionsSetSaysToUnsetVolcanoToken(t *testing.T) {
	setGitCommandTestHome(t)
	t.Setenv("VOLCANO_TOKEN", cliconfig.ProjectTokenPrefix+"token")
	t.Setenv("VOLCANO_PROJECT_ID", gitProjectID)
	api := newGitAPI(t)

	_, err := executeGitCommand(t, api.serve(), nil, "", "manifest-sections", "set", "--none")
	require.Error(t, err)
	assert.Equal(t, "choosing what a push applies is a person's decision: a project access token can't. "+
		"Unset VOLCANO_TOKEN and sign in with 'volcano login', then run this again", err.Error())
}

// The API has the last word on who may choose. Its refusal of a project
// access token, by code or in its own words, gets the same answer as the
// CLI's; person_required for another reason keeps Volcano's words; any other
// 403 is the API's.
func TestManifestSectionsSetExplainsTheAPIsRefusal(t *testing.T) {
	for _, tc := range []struct {
		name    string
		message string
		code    string
		want    string
	}{
		{
			name:    "project token refused",
			message: "project access tokens cannot choose what a push applies; a person chooses with a platform token",
			want:    personRefusal,
		},
		{name: "read-only token", message: "project access token is read-only", want: personRefusal},
		{name: "person required without words", code: "person_required", want: personRefusal},
		{
			name:    "person required in Volcano's words",
			message: "a platform token that never expires can't choose what a push applies; run `volcano login`",
			code:    "person_required",
			want: "the manifest sections weren't changed: a platform token that never expires can't choose " +
				"what a push applies; run `volcano login`",
		},
		{
			name:    "another refusal",
			message: "project not owned by the caller",
			want:    "failed to set the manifest sections: HTTP 403: project not owned by the caller",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setGitCommandTestHome(t)
			cliapi.ResetLastRefusalForTest()
			api := newGitAPI(t)
			api.connected = "octo/storefront"
			api.sectionsStatus, api.sectionsMessage, api.sectionsCode = http.StatusForbidden, tc.message, tc.code

			_, err := executeGitCommand(t, api.serve(), nil, "", "manifest-sections", "set", "frontends", "--yes")
			require.Error(t, err)
			assert.Equal(t, tc.want, err.Error())
			assert.Equal(t, http.StatusForbidden, cliapi.Status(err), "the exit code still says refused")
		})
	}
}

// Which sections exist is the API's to say, and its reason is shown as given.
func TestManifestSectionsSetShowsAnUnknownSectionRefusal(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"

	_, err := executeGitCommand(t, api.serve(), nil, "", "manifest-sections", "set", "functions", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to set the manifest sections: HTTP 400: unknown manifest section: functions")
}

func TestManifestSectionsSetRefusesWhatCannotNameSections(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"manifest-sections", "set"}, "name the sections a push applies, such as frontends, or pass --none"},
		{[]string{"manifest-sections", "set", "frontends", "--none"}, "pass sections or --none, not both"},
		{[]string{"manifest-sections", "set", " "}, "a section is empty; pass --none to apply none"},
	} {
		setGitCommandTestHome(t)
		api := newGitAPI(t)
		api.connected = "octo/storefront"

		_, err := executeGitCommand(t, api.serve(), nil, "", tc.args...)
		require.Error(t, err, "%q", tc.args)
		assert.Contains(t, err.Error(), tc.want, "%q", tc.args)
		assert.Nil(t, api.sentSectionsBody(), "%q", tc.args)
	}
}

// Without a terminal to ask on, adding a section fails and names --yes rather
// than reading a closed stdin as a decline.
func TestManifestSectionsSetWithoutATerminalNeedsYes(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"

	closed, err := os.Open(os.DevNull)
	require.NoError(t, err)
	t.Cleanup(func() { _ = closed.Close() })

	server := api.serve()
	cmd := New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(closed)
	cmd.SetArgs([]string{"manifest-sections", "set", "frontends"})

	err = cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stdin is not a terminal. Pass --yes to proceed")
	assert.Contains(t, err.Error(), "This lets anyone who can push to main on octo/storefront")
	assert.Nil(t, api.sentSectionsBody())
}
