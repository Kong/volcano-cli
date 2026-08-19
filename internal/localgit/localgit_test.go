package localgit

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestParseGitHubRepositoryAcceptsEveryFormGitHubHandsOut(t *testing.T) {
	t.Parallel()
	for name, rawURL := range map[string]string{
		"scp-like ssh":       "git@github.com:octo/storefront.git",
		"scp-like no suffix": "git@github.com:octo/storefront",
		"ssh url":            "ssh://git@github.com/octo/storefront.git",
		"https":              "https://github.com/octo/storefront.git",
		"https no suffix":    "https://github.com/octo/storefront",
		"https trailing":     "https://github.com/octo/storefront/",
		"https with user":    "https://octo@github.com/octo/storefront.git",
		"http":               "http://github.com/octo/storefront.git",
		"www host":           "https://www.github.com/octo/storefront.git",
		"uppercase host":     "git@GitHub.com:octo/storefront.git",
		"ssh with port":      "ssh://git@github.com:22/octo/storefront.git",
		"surrounding space":  "  git@github.com:octo/storefront.git  ",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			repository, err := ParseGitHubRepository(rawURL)
			require.NoError(t, err)
			assert.Equal(t, "octo", repository.Owner)
			assert.Equal(t, "storefront", repository.Name)
			assert.Equal(t, "octo/storefront", repository.FullName())
		})
	}
}

// A repository name may itself end in ".git"; only the URL suffix is stripped.
func TestParseGitHubRepositoryKeepsADotGitInTheName(t *testing.T) {
	t.Parallel()
	repository, err := ParseGitHubRepository("git@github.com:octo/dot.git.git")
	require.NoError(t, err)
	assert.Equal(t, "dot.git", repository.Name)
}

func TestParseGitHubRepositoryRejectsOtherHosts(t *testing.T) {
	t.Parallel()
	for name, rawURL := range map[string]string{
		"gitlab":            "git@gitlab.com:octo/storefront.git",
		"bitbucket https":   "https://bitbucket.org/octo/storefront.git",
		"github enterprise": "git@github.example.com:octo/storefront.git",
		// A lookalike host: the check is the whole hostname, not a suffix.
		"lookalike suffix": "https://notgithub.com/octo/storefront.git",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseGitHubRepository(rawURL)
			require.ErrorIs(t, err, ErrNotGitHub)
		})
	}
}

func TestParseGitHubRepositoryRejectsUnusableURLs(t *testing.T) {
	t.Parallel()
	for name, rawURL := range map[string]string{
		"empty":          "",
		"blank":          "   ",
		"no separator":   "github.com",
		"no repo":        "git@github.com:octo",
		"empty owner":    "https://github.com//storefront.git",
		"empty repo":     "https://github.com/octo/",
		"extra segment":  "https://github.com/octo/storefront/tree/main",
		"unknown scheme": "ftp://github.com/octo/storefront.git",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseGitHubRepository(rawURL)
			require.Error(t, err)
			assert.True(t,
				errors.Is(err, ErrUnparsableRemote) || errors.Is(err, ErrNotGitHub),
				"want an unparsable or not-GitHub error, got %v", err)
		})
	}
}

func TestRemotesReadsEveryRemoteInOrder(t *testing.T) {
	t.Parallel()
	client := clientReturning(t, "git remote -v", ""+
		"origin\tgit@github.com:octo/storefront.git (fetch)\n"+
		"origin\tgit@github.com:octo/storefront.git (push)\n"+
		"upstream\thttps://github.com/acme/storefront.git (fetch)\n"+
		"upstream\thttps://github.com/acme/storefront.git (push)\n")

	remotes, err := client.Remotes(t.Context())
	require.NoError(t, err)
	require.Len(t, remotes, 2)
	assert.Equal(t, Remote{Name: "origin", URL: "git@github.com:octo/storefront.git"}, remotes[0])
	assert.Equal(t, Remote{Name: "upstream", URL: "https://github.com/acme/storefront.git"}, remotes[1])
}

// A remote with a separate push URL reports two different URLs under one name.
// The fetch URL is the one that identifies the repository, so the push entry
// has to be discarded rather than merely deduplicated away — a push-first
// ordering would otherwise pick the wrong URL.
func TestRemotesTakesTheFetchURLNotThePushURL(t *testing.T) {
	t.Parallel()
	client := clientReturning(t, "git remote -v", ""+
		"origin\tgit@github.com:octo/mirror.git (push)\n"+
		"origin\tgit@github.com:octo/storefront.git (fetch)\n")

	remotes, err := client.Remotes(t.Context())
	require.NoError(t, err)
	require.Len(t, remotes, 1)
	assert.Equal(t, "git@github.com:octo/storefront.git", remotes[0].URL)
}

// git stores and prints a remote URL verbatim, so one containing a space has to
// survive parsing. Dropping the line does not fail — it silently changes which
// remote gets selected, and hides the ambiguity guard that should have fired.
func TestRemotesReadsARemoteURLContainingASpace(t *testing.T) {
	t.Parallel()
	client := clientReturning(t, "git remote -v", ""+
		"origin\thttps://github.com/octo/store front.git (fetch)\n"+
		"origin\thttps://github.com/octo/store front.git (push)\n"+
		"upstream\thttps://github.com/acme/other.git (fetch)\n"+
		"upstream\thttps://github.com/acme/other.git (push)\n")

	remotes, err := client.Remotes(t.Context())
	require.NoError(t, err)
	require.Len(t, remotes, 2)
	assert.Equal(t, "https://github.com/octo/store front.git", remotes[0].URL)

	// Two remotes and one named origin: origin wins, rather than the list
	// collapsing to one entry and the lone-remote branch picking upstream.
	selected, err := SelectRemote(remotes, "")
	require.NoError(t, err)
	assert.Equal(t, "origin", selected.Name)
}

func TestRemotesReportsAnEmptyRemoteList(t *testing.T) {
	t.Parallel()
	_, err := clientReturning(t, "git remote -v", "").Remotes(t.Context())
	require.ErrorIs(t, err, ErrNoRemotes)
}

func TestRemotesReportsANonRepository(t *testing.T) {
	t.Parallel()
	client := Client{runner: cliruntime.CommandRunnerFunc(
		func(context.Context, string, ...string) ([]byte, error) {
			return nil, errors.New("exit status 128")
		})}

	_, err := client.Remotes(t.Context())
	require.ErrorIs(t, err, ErrGitUnavailable)
}

// git's own message is the only thing that separates "you are in the wrong
// directory" from "add this path to safe.directory", which is routine in a
// container whose checkout is owned by another user.
func TestRemotesKeepsGitsOwnDiagnosis(t *testing.T) {
	t.Parallel()
	client := Client{runner: cliruntime.CommandRunnerFunc(
		func(context.Context, string, ...string) ([]byte, error) {
			return nil, &exec.ExitError{
				ProcessState: &os.ProcessState{},
				Stderr:       []byte("fatal: detected dubious ownership in repository at '/workspace'\n"),
			}
		})}

	_, err := client.Remotes(t.Context())
	require.ErrorIs(t, err, ErrGitUnavailable)
	assert.Contains(t, err.Error(), "dubious ownership")
}

// A remote rewritten by CI carries a job token. Any message naming the remote
// has to drop it, or it lands in the build log.
func TestParseGitHubRepositoryRedactsCredentialsInErrors(t *testing.T) {
	t.Parallel()
	_, err := ParseGitHubRepository("https://gitlab-ci-token:glcbt-SUPERSECRET@gitlab.com/octo/storefront.git")

	require.ErrorIs(t, err, ErrNotGitHub)
	assert.NotContains(t, err.Error(), "SUPERSECRET")
	assert.Contains(t, err.Error(), "***@gitlab.com")
}

// The same leak in the shape GitHub itself documents: the token is the whole
// userinfo, with no password field to spot.
func TestParseGitHubRepositoryRedactsATokenCarriedAsTheWholeUserInfo(t *testing.T) {
	t.Parallel()
	_, err := ParseGitHubRepository("https://ghp_16C7e42F292c6912E7710c838347Ae178B4a@gitlab.com/octo/storefront.git")

	require.ErrorIs(t, err, ErrNotGitHub)
	assert.NotContains(t, err.Error(), "ghp_16C7e42F292c6912E7710c838347Ae178B4a")
	assert.Contains(t, err.Error(), "***@gitlab.com")
}

// Unicode folding cannot normalize a host: strings.ToLower maps U+0130 to ASCII
// "i", so this would otherwise fold to github.com and bind the project to a
// repository on the real github.com without saying so.
func TestParseGitHubRepositoryRejectsAUnicodeLookalikeHost(t *testing.T) {
	t.Parallel()
	for name, rawURL := range map[string]string{
		"dotted capital I": "https://g\u0130thub.com/octo/storefront.git",
		"cyrillic o":       "https://github.c\u043fm/octo/storefront.git",
		"fullwidth":        "https://ｇithub.com/octo/storefront.git",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseGitHubRepository(rawURL)
			require.ErrorIs(t, err, ErrNotGitHub)
		})
	}
}

func TestRedact(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ in, want string }{
		"password form dropped": {
			"https://gitlab-ci-token:glcbt-SECRET@gitlab.com/octo/repo.git",
			"https://***@gitlab.com/octo/repo.git",
		},
		// A token is as often the entire userinfo as it is a password field:
		// "https://<pat>@github.com/owner/repo.git" is GitHub's own documented
		// form. Nothing distinguishes that from a user name, so all of it goes.
		"whole userinfo dropped": {
			"https://ghp_16C7e42F292c6912E7710c838347Ae178B4a@github.com/octo/repo.git",
			"https://***@github.com/octo/repo.git",
		},
		"bare user name dropped too": {
			"https://octo@github.com/octo/repo.git", "https://***@github.com/octo/repo.git",
		},
		"no userinfo is untouched": {
			"https://github.com/octo/repo.git", "https://github.com/octo/repo.git",
		},
		"scp-like is untouched": {
			"git@github.com:octo/repo.git", "git@github.com:octo/repo.git",
		},
		"authority only": {
			"https://user:pw@github.com", "https://***@github.com",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, Redact(tc.in))
		})
	}
}

func TestSelectRemote(t *testing.T) {
	t.Parallel()
	origin := Remote{Name: "origin", URL: "git@github.com:octo/storefront.git"}
	upstream := Remote{Name: "upstream", URL: "git@github.com:acme/storefront.git"}
	fork := Remote{Name: "fork", URL: "git@github.com:me/storefront.git"}

	t.Run("a lone remote is taken whatever it is called", func(t *testing.T) {
		t.Parallel()
		selected, err := SelectRemote([]Remote{upstream}, "")
		require.NoError(t, err)
		assert.Equal(t, upstream, selected)
	})

	t.Run("origin wins among several", func(t *testing.T) {
		t.Parallel()
		selected, err := SelectRemote([]Remote{upstream, origin}, "")
		require.NoError(t, err)
		assert.Equal(t, origin, selected)
	})

	t.Run("several remotes without origin are ambiguous", func(t *testing.T) {
		t.Parallel()
		_, err := SelectRemote([]Remote{upstream, fork}, "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--remote")
	})

	t.Run("a named remote is used even when origin exists", func(t *testing.T) {
		t.Parallel()
		selected, err := SelectRemote([]Remote{origin, upstream}, "upstream")
		require.NoError(t, err)
		assert.Equal(t, upstream, selected)
	})

	t.Run("a missing named remote is an error", func(t *testing.T) {
		t.Parallel()
		_, err := SelectRemote([]Remote{origin}, "nope")
		require.Error(t, err)
		assert.Contains(t, err.Error(), `no remote named "nope"`)
	})
}

// clientReturning builds a Client whose runner asserts the command line it is
// given and answers with stdout.
func clientReturning(t *testing.T, wantCommand, stdout string) Client {
	t.Helper()
	return Client{runner: cliruntime.CommandRunnerFunc(
		func(_ context.Context, name string, args ...string) ([]byte, error) {
			assert.Equal(t, wantCommand, strings.TrimSpace(name+" "+strings.Join(args, " ")))
			return []byte(stdout), nil
		})}
}

// A repository address copied out of a browser carries a query string or a
// fragment. Carrying either into the name yields a plausible-looking wrong
// answer rather than an error, which is worse than rejecting it.
func TestParseGitHubRepositoryDropsQueryAndFragment(t *testing.T) {
	t.Parallel()
	for name, rawURL := range map[string]string{
		"query":              "https://github.com/octo/storefront?tab=readme-ov-file",
		"fragment":           "https://github.com/octo/storefront#readme",
		"query then suffix":  "https://github.com/octo/storefront.git?foo=bar",
		"fragment and query": "https://github.com/octo/storefront?a=b#c",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			repository, err := ParseGitHubRepository(rawURL)
			require.NoError(t, err)
			assert.Equal(t, "octo/storefront", repository.FullName())
		})
	}
}

func TestParseGitHubRepositoryTrimsTheGitSuffixWhateverItsCase(t *testing.T) {
	t.Parallel()
	repository, err := ParseGitHubRepository("git@github.com:octo/storefront.GIT")
	require.NoError(t, err)
	assert.Equal(t, "storefront", repository.Name)
}

// The owner and name are validated against what GitHub accepts, so a URL this
// package splits wrongly errors instead of producing a wrong answer. A port in
// a scp-like URL is the case that lands in the owner.
func TestParseGitHubRepositoryRejectsNamesGitHubWouldNotAccept(t *testing.T) {
	t.Parallel()
	for name, rawURL := range map[string]string{
		"scp-like with port":  "git@github.com:22:octo/storefront.git",
		"space in name":       "https://github.com/octo/store front",
		"colon in owner":      "https://github.com/oc:to/storefront",
		"tilde in name":       "https://github.com/octo/store~front",
		"path after the repo": "https://github.com/octo/storefront/issues/1",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseGitHubRepository(rawURL)
			require.ErrorIs(t, err, ErrUnparsableRemote)
		})
	}
}
