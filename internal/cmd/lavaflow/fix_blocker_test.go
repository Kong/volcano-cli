package lavaflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	clilavaflow "github.com/Kong/volcano-cli/internal/lavaflow"
)

// The API's fix_blocker says what stops LavaFlow fixing issues while it is
// on. status, settings and enable end with Volcano's words and the command
// that acts on it.

const (
	repositoryRequiredMessage = "Link your project to GitHub to start automatically fixing your issues."
	repositoryChangedMessage  = "Turn LavaFlow on again to start fixing issues in acme/shop."
)

func repositoryRequired() map[string]any {
	return map[string]any{"code": "repository_required", "message": repositoryRequiredMessage}
}

func repositoryChanged() map[string]any {
	return map[string]any{"code": "repository_changed", "message": repositoryChangedMessage}
}

func TestStatusAndSettingsSayWhatStopsLavaFlowAndWhatToRun(t *testing.T) {
	for name, tc := range map[string]struct {
		blocker    map[string]any
		repository map[string]any
		want       string
	}{
		"no repository": {
			repositoryRequired(), nil,
			"Warning: " + repositoryRequiredMessage + " Run volcano git connect <git-url>.",
		},
		"repository changed": {
			repositoryChanged(), lavaflowLinkedRepository(),
			"Warning: " + repositoryChangedMessage + " Run volcano cloud lavaflow enable.",
		},
		"a code the CLI doesn't know": {
			map[string]any{"code": "quota_reached", "message": "LavaFlow reached this month's limit."}, nil,
			"Warning: LavaFlow reached this month's limit.\n",
		},
	} {
		for _, command := range []string{"status", "settings"} {
			t.Run(name+" "+command, func(t *testing.T) {
				fake, deps := newLavaflowServer(t)
				fake.set(func(s *lavaflowServer) {
					s.settings["enabled"] = true
					s.settings["fix_blocker"], s.settings["repository"] = tc.blocker, tc.repository
					s.repoConnected = tc.repository != nil
				})

				out, err := executeLavaflowCommand(t, New(deps), command)
				require.NoError(t, err)
				assert.Contains(t, out, tc.want)
				assert.Equal(t, 1, strings.Count(out, "Warning:"), "one hint, not the CLI's own as well: %q", out)
				assertLavaFlowWording(t, out)
			})
		}
	}
}

// settings names the repository and branch fix pull requests go to, and says
// when there is none only when Volcano says so: an API older than the
// settings' repository leaves it out.
func TestSettingsNamesTheRepositoryAndBranch(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) { s.settings["repository"] = lavaflowLinkedRepository() })
	out, err := executeLavaflowCommand(t, New(deps), "settings")
	require.NoError(t, err)
	assert.Contains(t, out, "Repository: acme/shop\nBranch: main\n")
	assert.NotContains(t, out, "Warning:")

	fake, deps = newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) { s.settings["enabled"], s.settings["fix_blocker"] = true, repositoryRequired() })
	out, err = executeLavaflowCommand(t, New(deps), "settings")
	require.NoError(t, err)
	assert.Contains(t, out, "Repository: none connected\n")

	_, deps = newLavaflowServer(t)
	out, err = executeLavaflowCommand(t, New(deps), "settings")
	require.NoError(t, err)
	assert.NotContains(t, out, "Repository:")
}

func TestSettingsJSONHasTheRepositoryAndFixBlocker(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.settings["enabled"] = true
		s.settings["repository"], s.settings["fix_blocker"] = lavaflowLinkedRepository(), repositoryChanged()
	})

	out, err := executeLavaflowCommand(t, New(deps), "settings", "--json")
	require.NoError(t, err)
	value := decodeJSON(t, out)
	assert.Equal(t, repositoryChanged(), value["fix_blocker"])
	assert.Equal(t, asJSON(t, lavaflowLinkedRepository()), value["repository"])
}

// With no repository, enable doesn't say LavaFlow opens fix pull requests in
// one, and warns instead of reporting LavaFlow on.
func TestEnableWithoutARepositoryWarns(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.repoConnected = false
		// What Volcano answers once LavaFlow is on with no repository.
		s.settings["fix_blocker"] = repositoryRequired()
	})

	out, errOut, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader(""), "enable", "--yes")
	require.NoError(t, err)
	assert.Equal(t, clilavaflow.EnableDisclosureWithoutRepository+"\n", errOut)
	assert.NotContains(t, errOut, "in your connected repository")
	assert.True(t, strings.HasPrefix(out, "Warning: LavaFlow is on, but it isn't fixing issues yet\n"), "%q", out)
	assert.NotContains(t, out, "✓")
	assert.Contains(t, out, "Repository: none connected\n")
	assert.True(t, strings.HasSuffix(out,
		"Warning: "+repositoryRequiredMessage+" Run volcano git connect <git-url>.\n"), "%q", out)
	assert.Equal(t, map[string]any{"enabled": true}, fake.lastUpdate(t))
}

// With a repository, enable says what LavaFlow does in the approved words and
// reports it on.
func TestEnableWithARepositoryReportsItOn(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) { s.settings["repository"] = lavaflowLinkedRepository() })

	out, errOut, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader(""), "enable", "--yes")
	require.NoError(t, err)
	assert.Equal(t, clilavaflow.EnableDisclosure+"\n", errOut)
	assert.True(t, strings.HasPrefix(out, "✓ LavaFlow is on\n"), "%q", out)
	assert.Contains(t, out, "Repository: acme/shop\nBranch: main\n")
	assert.NotContains(t, out, "Warning:")
	assert.Zero(t, fake.repoReads, "the settings name the repository")
}

// The help says how to connect a repository and that changing it later needs
// LavaFlow turned on again, which stays true whether or not the first
// connection carries LavaFlow over.
func TestEnableHelpSaysToTurnLavaFlowOnAgainAfterChangingTheRepository(t *testing.T) {
	_, deps := newLavaflowServer(t)

	out, err := executeLavaflowCommand(t, New(deps), "enable", "--help")
	require.NoError(t, err)
	flat := strings.Join(strings.Fields(out), " ")
	assert.Contains(t, flat, "Connect a repository with 'volcano git connect'. If you change it later, turn LavaFlow on again.")
	assert.NotContains(t, flat, "so LavaFlow can open fix pull requests")
}
