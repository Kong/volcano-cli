package localgit

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
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
	assert.Equal(t, Remote{
		Name:     "origin",
		FetchURL: "git@github.com:octo/storefront.git",
		PushURLs: []string{"git@github.com:octo/storefront.git"},
	}, remotes[0])
	assert.Equal(t, Remote{
		Name:     "upstream",
		FetchURL: "https://github.com/acme/storefront.git",
		PushURLs: []string{"https://github.com/acme/storefront.git"},
	}, remotes[1])
}

// A remote with a separate pushurl reports two different URLs under one name.
// A push is what triggers a deployment, so the push URL is the one that decides
// which repository to bind — whichever order git prints them in.
func TestRemotesTakesThePushURLAndKeepsTheFetchURL(t *testing.T) {
	t.Parallel()
	for name, output := range map[string]string{
		"push first": "origin\tgit@github.com:octo/mirror.git (push)\n" +
			"origin\tgit@github.com:octo/storefront.git (fetch)\n",
		"fetch first": "origin\tgit@github.com:octo/storefront.git (fetch)\n" +
			"origin\tgit@github.com:octo/mirror.git (push)\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			remotes, err := clientReturning(t, "git remote -v", output).Remotes(t.Context())
			require.NoError(t, err)
			require.Len(t, remotes, 1)
			assert.Equal(t, "git@github.com:octo/mirror.git", remotes[0].PushURLs[0])
			assert.Equal(t, "git@github.com:octo/storefront.git", remotes[0].FetchURL)
			assert.True(t, remotes[0].Diverges())
		})
	}
}

// The ordinary remote pushes where it fetches, and does not diverge.
func TestRemotesReportsNoDivergenceForAnOrdinaryRemote(t *testing.T) {
	t.Parallel()
	remotes, err := clientReturning(t, "git remote -v", ""+
		"origin\tgit@github.com:octo/storefront.git (fetch)\n"+
		"origin\tgit@github.com:octo/storefront.git (push)\n").Remotes(t.Context())
	require.NoError(t, err)
	require.Len(t, remotes, 1)
	assert.False(t, remotes[0].Diverges())
}

// A remote with nothing to push to is still reported. Dropping it made a named
// lookup deny a remote git still lists, and silently moved the selection to
// another one; PushTarget is where having nothing to push to is raised.
func TestRemotesKeepsARemoteWithNoPushURL(t *testing.T) {
	t.Parallel()
	remotes, err := clientReturning(t, "git remote -v",
		"origin\tgit@github.com:octo/storefront.git (fetch)\n").Remotes(t.Context())
	require.NoError(t, err)
	require.Len(t, remotes, 1)
	assert.Empty(t, remotes[0].PushURLs)

	_, err = remotes[0].PushTarget()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `remote "origin" has nothing to push to`)
}

// Having no remotes is not Remotes' verdict to give. A checkout with none can
// still have a push route — remote.pushDefault holding a URL is one — so the
// empty list comes back as a list, and SelectRemote decides whether it matters.
func TestRemotesReturnsAnEmptyListRatherThanFailing(t *testing.T) {
	t.Parallel()
	remotes, err := clientReturning(t, "git remote -v", "").Remotes(t.Context())
	require.NoError(t, err)
	assert.Empty(t, remotes)
}

// A query string can carry a credential of its own, and it is no part of what
// identifies the remote.
func TestRedactDropsAQueryAndFragment(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ in, want string }{
		"query": {
			"https://gitlab.com/org/repo.git?private_token=SECRET",
			"https://gitlab.com/org/repo.git",
		},
		"fragment": {"https://gitlab.com/org/repo.git#SECRET", "https://gitlab.com/org/repo.git"},
		"both with userinfo": {
			"https://u:p@gitlab.com/org/repo.git?token=SECRET#frag",
			"https://***@gitlab.com/org/repo.git",
		},
		"scp-like query": {"git@gitlab.com:org/repo.git?token=SECRET", "***@gitlab.com:org/repo.git"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, Redact(tc.in))
		})
	}
}

func TestParseGitHubRepositoryRedactsAQuerySecretInErrors(t *testing.T) {
	t.Parallel()
	_, err := ParseGitHubRepository("https://gitlab.com/org/repo.git?private_token=CANARYSECRET")

	require.ErrorIs(t, err, ErrNotGitHub)
	assert.NotContains(t, err.Error(), "CANARYSECRET")
	assert.NotContains(t, err.Error(), "private_token")
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
	assert.Equal(t, "https://github.com/octo/store front.git", remotes[0].PushURLs[0])

	// Two remotes and one named origin: origin wins, rather than the list
	// collapsing to one entry and the lone-remote branch picking upstream.
	selected, err := SelectRemote(remotes, "", PushRemote{})
	require.NoError(t, err)
	assert.Equal(t, "origin", selected.Name)
}

// Where the empty list does become a failure: nothing else named a destination,
// so there is no remote to fall back on.
func TestSelectRemoteReportsAnEmptyRemoteListWhenNothingElseDecides(t *testing.T) {
	t.Parallel()
	_, err := SelectRemote(nil, "", PushRemote{})
	require.ErrorIs(t, err, ErrNoRemotes)

	// And when the user named one that cannot exist.
	_, err = SelectRemote(nil, "origin", PushRemote{})
	require.ErrorIs(t, err, ErrNoRemotes)
}

// One line ending comes off, not every trailing one. A cutset read a value that
// ends in a newline as the valid remote name underneath it, and Windows is a
// release target so the terminator there may be "\r\n".
func TestTrimRecordTerminatorRemovesOneLineEnding(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ in, want string }{
		"lf terminator":         {"origin\n", "origin"},
		"crlf terminator":       {"origin\r\n", "origin"},
		"value ending in lf":    {"origin\n\n", "origin\n"},
		"value ending in crlf":  {"origin\r\n\r\n", "origin\r\n"},
		"no terminator at all":  {"origin", "origin"},
		"padding is not a line": {" origin \n", " origin "},
		"empty":                 {"\n", ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, trimRecordTerminator(tc.in))
		})
	}
}

// Resolving the rewrite is only half the job: the binding has to be made from
// it. git pushes to the rewritten URL, so binding the configured one binds a
// repository the push never reaches — silently, since both are valid GitHub
// URLs and nothing fails.
func TestSelectRemoteBindsTheRewrittenPushURL(t *testing.T) {
	t.Parallel()
	selected, err := SelectRemote(nil, "", PushRemote{
		Name:         "https://github.com/octo/decoy.git",
		Source:       "remote.pushDefault",
		RewrittenURL: "https://github.com/octo/storefront.git",
	})
	require.NoError(t, err)

	pushURL, err := selected.PushTarget()
	require.NoError(t, err)
	repository, err := ParseGitHubRepository(pushURL)
	require.NoError(t, err)
	assert.Equal(t, "octo/storefront", repository.FullName())
	assert.NotEqual(t, "octo/decoy", repository.FullName())
}

// A rewrite does not apply to a remote name: git looks the name up first, and
// only an unmatched value is treated as a URL.
func TestSelectRemoteIgnoresARewriteWhenTheValueNamesARemote(t *testing.T) {
	t.Parallel()
	fork := Remote{Name: "fork", PushURLs: []string{"git@github.com:me/storefront.git"}}

	selected, err := SelectRemote([]Remote{fork}, "", PushRemote{
		Name:         "fork",
		Source:       "remote.pushDefault",
		RewrittenURL: "https://github.com/wrong/repository.git",
	})
	require.NoError(t, err)
	assert.Equal(t, "fork", selected.Name)
}

// But a push route needs no remote list at all: git follows a URL in these keys
// out of a checkout with no remotes, so refusing it for an empty `git remote -v`
// would refuse a repository a push really does deploy from.
func TestSelectRemoteFollowsAPushURLWithNoRemotesAtAll(t *testing.T) {
	t.Parallel()
	selected, err := SelectRemote(nil, "",
		PushRemote{Name: "https://github.com/me/storefront.git", Source: "remote.pushDefault"})
	require.NoError(t, err)
	assert.False(t, selected.Named())

	pushURL, err := selected.PushTarget()
	require.NoError(t, err)
	repository, err := ParseGitHubRepository(pushURL)
	require.NoError(t, err)
	assert.Equal(t, "me/storefront", repository.FullName())
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
		"scp-like userinfo dropped": {
			"git@github.com:octo/repo.git", "***@github.com:octo/repo.git",
		},
		"scp-like without userinfo kept": {
			"github.com:octo/repo.git", "github.com:octo/repo.git",
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
	origin := Remote{Name: "origin", PushURLs: []string{"git@github.com:octo/storefront.git"}}
	upstream := Remote{Name: "upstream", PushURLs: []string{"git@github.com:acme/storefront.git"}}
	fork := Remote{Name: "fork", PushURLs: []string{"git@github.com:me/storefront.git"}}

	t.Run("a lone remote is taken whatever it is called", func(t *testing.T) {
		t.Parallel()
		selected, err := SelectRemote([]Remote{upstream}, "", PushRemote{})
		require.NoError(t, err)
		assert.Equal(t, upstream, selected)
	})

	t.Run("origin wins among several", func(t *testing.T) {
		t.Parallel()
		selected, err := SelectRemote([]Remote{upstream, origin}, "", PushRemote{})
		require.NoError(t, err)
		assert.Equal(t, origin, selected)
	})

	t.Run("several remotes without origin are ambiguous", func(t *testing.T) {
		t.Parallel()
		_, err := SelectRemote([]Remote{upstream, fork}, "", PushRemote{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--remote")
	})

	t.Run("a named remote is used even when origin exists", func(t *testing.T) {
		t.Parallel()
		selected, err := SelectRemote([]Remote{origin, upstream}, "upstream", PushRemote{})
		require.NoError(t, err)
		assert.Equal(t, upstream, selected)
	})

	t.Run("a missing named remote is an error", func(t *testing.T) {
		t.Parallel()
		_, err := SelectRemote([]Remote{origin}, "nope", PushRemote{})
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

// git accepts remotes whose "URL" is not a URL at all. None of these can be
// echoed: the transport-helper form is a command line that can carry a
// password, a local path names directories, and a mistyped argument is often a
// bare token. The value is replaced rather than trusted.
func TestRedactRefusesToEchoUnrecognizedForms(t *testing.T) {
	t.Parallel()
	for name, raw := range map[string]string{
		"transport helper": "ext::ssh -o Password=CANARY git@github.com %S octo/repo.git",
		"bare token":       "ghp_16C7e42F292c6912E7710c838347Ae178B4a",
		"local path":       "/Users/someone/private-clients/acme/repo.git",
		"relative path":    "../sibling/repo.git",
		"file url":         "file:///Users/someone/private/repo.git",
		// A colon with nothing credential-shaped after it: the first segment is
		// not a host, which is all that separates these from a scp-like remote.
		"transport helper without userinfo": "ext::ssh -i /home/me/.ssh/CANARY_KEY %S repo.git",
		"windows path":                      `C:\\Users\\someone\\private-clients\\CANARY\\repo.git`,
		// The userinfo here does contain a dot, so only the "@" past the
		// authority marks the split as untrustworthy.
		"dotted user and slash in credential": "https://user.name:aa/CANARY@github.com/octo/repo.git",
		// Redacted userinfo, but what is left is not a host either.
		"userinfo on a non-host": "https://user:pw@CANARY-not-a-host/octo/repo.git",
		// A "/" inside the credential pushes the rest of it past the authority,
		// so the userinfo cannot be located and nothing is echoed.
		"slash in credential": "https://user:aa/CANARY@github.com/octo/repo.git",
		"at sign in path":     "git@github.com:octo/CANARY@repo.git",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, Placeholder, Redact(raw))
		})
	}
}

// The same URLs must not reach an error message either, whichever way they fail
// to parse.
func TestParseGitHubRepositoryNeverEchoesAnUnrecognizedRemote(t *testing.T) {
	t.Parallel()
	for name, raw := range map[string]string{
		"transport helper":    "ext::ssh -o Password=CANARY git@github.com %S octo/repo.git",
		"bare token":          "ghp_CANARYTOKEN",
		"local path":          "/Users/someone/private-clients/CANARY/repo.git",
		"slash in credential": "https://user:aa/CANARY@github.com/octo/repo.git",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseGitHubRepository(raw)
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "CANARY")
			assert.Contains(t, err.Error(), Placeholder)
		})
	}
}

// A credential containing "/" also makes the host unreadable, so the URL is
// reported as unparsable rather than misdiagnosed as hosted somewhere else.
func TestParseGitHubRepositoryDoesNotMisreadTheHostPastACredential(t *testing.T) {
	t.Parallel()
	_, err := ParseGitHubRepository("https://user:aa/secret@github.com/octo/storefront.git")
	require.ErrorIs(t, err, ErrUnparsableRemote)
}

// Recognized URLs still say what they are: refusing to echo everything would
// cost the user the one detail that identifies the remote.
func TestRedactStillEchoesRecognizedForms(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ in, want string }{
		"scp-like no userinfo":   {"github.com:octo/repo.git", "github.com:octo/repo.git"},
		"scp-like with userinfo": {"git@github.com:octo/repo.git", "***@github.com:octo/repo.git"},
		"https":                  {"https://gitlab.com/octo/repo.git", "https://gitlab.com/octo/repo.git"},
		"https userinfo": {
			"https://gitlab-ci-token:SECRET@gitlab.com/octo/repo.git",
			"https://***@gitlab.com/octo/repo.git",
		},
		// The userinfo goes even when it is plainly a login: an https URL carries
		// tokens there, and one rule for both is safer than guessing per scheme.
		"ssh url": {"ssh://git@github.com:22/octo/repo.git", "ssh://***@github.com:22/octo/repo.git"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, Redact(tc.in))
		})
	}
}

// A token in the scp-like form has no password field to spot, exactly as in the
// URL form, so the same rule applies: the userinfo goes.
func TestRedactDropsAScpLikeUserInfoCarryingAToken(t *testing.T) {
	t.Parallel()
	assert.Equal(t,
		"***@gitlab.com:octo/repo.git",
		Redact("ghp_16C7e42F292c6912E7710c838347Ae178B4a@gitlab.com:octo/repo.git"))
}

func TestParseGitHubRepositoryRedactsAScpLikeTokenInErrors(t *testing.T) {
	t.Parallel()
	_, err := ParseGitHubRepository("ghp_16C7e42F292c6912E7710c838347Ae178B4a@gitlab.com:octo/storefront.git")

	require.ErrorIs(t, err, ErrNotGitHub)
	assert.NotContains(t, err.Error(), "ghp_16C7e42F292c6912E7710c838347Ae178B4a")
	assert.Contains(t, err.Error(), "***@gitlab.com:octo/storefront.git")
}

// git accepts several remote.<name>.pushurl entries and a single push updates
// every one of them, so `git remote -v` prints several (push) lines. Keeping
// only the last would bind one repository while pushes kept reaching the
// others — and the CLI would report that one as "the push target".
func TestRemotesKeepsEveryPushURL(t *testing.T) {
	t.Parallel()
	remotes, err := clientReturning(t, "git remote -v", ""+
		"origin\thttps://github.com/octo/storefront.git (fetch)\n"+
		"origin\tgit@github.com:octo/mirror-one.git (push)\n"+
		"origin\tgit@github.com:octo/mirror-two.git (push)\n").Remotes(t.Context())
	require.NoError(t, err)
	require.Len(t, remotes, 1)

	assert.Equal(t, []string{
		"git@github.com:octo/mirror-one.git",
		"git@github.com:octo/mirror-two.git",
	}, remotes[0].PushURLs)
	assert.False(t, remotes[0].Diverges(), "divergence is about one push target, and there is not one")
}

// With several push targets there is no single repository to connect, so the
// caller is told to choose rather than having one chosen for it.
func TestPushTargetRefusesSeveralPushURLs(t *testing.T) {
	t.Parallel()
	remote := Remote{Name: "origin", PushURLs: []string{
		"https://user:SECRET@github.com/octo/mirror-one.git",
		"git@github.com:octo/mirror-two.git",
	}}

	_, err := remote.PushTarget()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `remote "origin" pushes to 2 repositories`)
	assert.Contains(t, err.Error(), "pass the repository URL to choose")
	// The list names them, so the credential in one has to be redacted.
	assert.NotContains(t, err.Error(), "SECRET")
	assert.Contains(t, err.Error(), "***@github.com/octo/mirror-one.git")
}

func TestPushTargetReturnsTheOnlyPushURL(t *testing.T) {
	t.Parallel()
	target, err := Remote{Name: "origin", PushURLs: []string{"git@github.com:octo/storefront.git"}}.PushTarget()
	require.NoError(t, err)
	assert.Equal(t, "git@github.com:octo/storefront.git", target)
}

func TestPushTargetRefusesARemoteWithNothingToPushTo(t *testing.T) {
	t.Parallel()
	_, err := Remote{Name: "origin"}.PushTarget()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nothing to push to")
}

// git routes a bare `git push` through branch.<name>.pushRemote, then
// remote.pushDefault, then branch.<name>.remote, and only falls back to origin
// when none is set (git help config). A fork checkout routinely pushes to
// something other than origin, and that is the repository a deployment comes
// from.
func TestPushRemoteFollowsGitsPrecedence(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		config map[string]string
		want   string
		source string
	}{
		"nothing configured": {map[string]string{"git branch --show-current": "main"}, "", ""},
		"branch pushRemote wins over everything": {map[string]string{
			"git branch --show-current":               "main",
			"git config --get branch.main.pushRemote": "fork",
			"git config --get remote.pushDefault":     "upstream",
			"git config --get branch.main.remote":     "origin",
		}, "fork", "branch.main.pushRemote"},
		"pushDefault beats branch remote": {map[string]string{
			"git branch --show-current":           "main",
			"git config --get remote.pushDefault": "upstream",
			"git config --get branch.main.remote": "origin",
		}, "upstream", ""},
		"branch remote is the last word": {map[string]string{
			"git branch --show-current":           "main",
			"git config --get branch.main.remote": "upstream",
		}, "upstream", ""},
		// Detached HEAD prints nothing and succeeds, so the branch-scoped keys
		// are simply not asked for.
		"detached HEAD skips the branch keys": {map[string]string{
			"git branch --show-current":           "",
			"git config --get remote.pushDefault": "upstream",
		}, "upstream", ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			client := Client{runner: configRunner(tc.config)}
			push, err := client.PushRemote(t.Context())
			require.NoError(t, err)
			assert.Equal(t, tc.want, push.Name)
			if tc.source != "" {
				assert.Equal(t, tc.source, push.Source)
			}
		})
	}
}

func TestSelectRemotePrefersWhereGitPushes(t *testing.T) {
	t.Parallel()
	origin := Remote{Name: "origin", PushURLs: []string{"git@github.com:acme/upstream.git"}}
	fork := Remote{Name: "fork", PushURLs: []string{"git@github.com:me/storefront.git"}}

	t.Run("the push remote wins over the origin convention", func(t *testing.T) {
		t.Parallel()
		selected, err := SelectRemote([]Remote{origin, fork}, "", PushRemote{Name: "fork", Source: "remote.pushDefault"})
		require.NoError(t, err)
		assert.Equal(t, "fork", selected.Name)
	})

	t.Run("--remote still wins over the push remote", func(t *testing.T) {
		t.Parallel()
		selected, err := SelectRemote([]Remote{origin, fork}, "origin", PushRemote{Name: "fork", Source: "remote.pushDefault"})
		require.NoError(t, err)
		assert.Equal(t, "origin", selected.Name)
	})

	// Falling back to origin would bind a repository this checkout never pushes
	// to, which is the failure the routing exists to avoid.
	t.Run("a push remote naming nothing is refused", func(t *testing.T) {
		t.Parallel()
		_, err := SelectRemote([]Remote{origin, fork}, "",
			PushRemote{Name: "missing", Source: "remote.pushDefault"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), `remote.pushDefault names "missing"`)
		assert.Contains(t, err.Error(), "neither a remote in this repository nor a repository URL")
		assert.Contains(t, err.Error(), "--remote")
		// The refusal must not be reachable by silently picking origin instead.
		assert.NotContains(t, err.Error(), "acme/upstream")
	})

	t.Run("the ambiguity error names the remotes", func(t *testing.T) {
		t.Parallel()
		_, err := SelectRemote([]Remote{fork, {Name: "other"}}, "", PushRemote{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "fork, other")
	})
}

// git-push(1) takes "either a URL or the name of a remote", and all three
// routing keys feed it, so a URL in one of them is a working push route — a
// bare `git push` really does send there. Refusing it would refuse a checkout
// that deploys perfectly well, and falling back to origin would bind the wrong
// repository. Verified against git: with remote.pushDefault set to a URL and
// origin pointing elsewhere, `git push` updates the URL's repository.
func TestSelectRemoteFollowsAURLInThePushConfiguration(t *testing.T) {
	t.Parallel()
	origin := Remote{Name: "origin", PushURLs: []string{"git@github.com:acme/upstream.git"}}

	for name, key := range map[string]string{
		"pushRemote":  "branch.main.pushRemote",
		"pushDefault": "remote.pushDefault",
		"remote":      "branch.main.remote",
	} {
		t.Run("a URL in "+name+" is followed", func(t *testing.T) {
			t.Parallel()
			selected, err := SelectRemote([]Remote{origin}, "",
				PushRemote{Name: "https://github.com/me/storefront.git", Source: key})
			require.NoError(t, err)
			// No remote in this repository describes it, so it has no name.
			assert.False(t, selected.Named())
			assert.Empty(t, selected.Name)

			pushURL, err := selected.PushTarget()
			require.NoError(t, err)
			repository, err := ParseGitHubRepository(pushURL)
			require.NoError(t, err)
			assert.Equal(t, "me/storefront", repository.FullName())
			// Both sides are the same URL, so there is no divergence to report.
			assert.False(t, selected.Diverges())
		})
	}

	t.Run("an scp-like URL is followed too", func(t *testing.T) {
		t.Parallel()
		selected, err := SelectRemote([]Remote{origin}, "",
			PushRemote{Name: "git@github.com:me/storefront.git", Source: "remote.pushDefault"})
		require.NoError(t, err)
		pushURL, err := selected.PushTarget()
		require.NoError(t, err)
		repository, err := ParseGitHubRepository(pushURL)
		require.NoError(t, err)
		assert.Equal(t, "me/storefront", repository.FullName())
	})

	// A push there succeeds; it just lands somewhere Volcano cannot deploy from.
	// Telling the user to fix the setting would be wrong advice.
	t.Run("a URL hosted elsewhere is refused as not GitHub", func(t *testing.T) {
		t.Parallel()
		_, err := SelectRemote([]Remote{origin}, "",
			PushRemote{Name: "https://gitlab.com/me/storefront.git", Source: "remote.pushDefault"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not a github.com repository")
		assert.Contains(t, err.Error(), "remote.pushDefault")
		assert.NotContains(t, err.Error(), "fix that setting")
	})
}

// The routing keys are read out of the repository's config, and CI rewrites put
// a job token in one as a matter of course — GitLab's own recipe is
// "https://gitlab-ci-token:<token>@…". An error naming the value verbatim would
// print that token into the build log.
func TestPushConfigurationValuesAreNeverEchoedWithCredentials(t *testing.T) {
	t.Parallel()
	const canary = "s3cr3t-canary-token"

	for name, value := range map[string]string{
		"a GitLab CI token":           "https://gitlab-ci-token:" + canary + "@gitlab.com/me/app.git",
		"a PAT as the whole userinfo": "https://" + canary + "@github.enterprise.test/me/app.git",
		"an scp-like credential":      canary + "@gitlab.com:me/app.git",
		"a transport helper":          "ext::ssh -o Password=" + canary + " %S repo.git",
		"a credential with a slash":   "https://user:" + canary + "/x@gitlab.com/me/app.git",
	} {
		t.Run(name+" is not echoed", func(t *testing.T) {
			t.Parallel()
			_, err := SelectRemote(
				[]Remote{{Name: "origin", PushURLs: []string{"git@github.com:acme/app.git"}}}, "",
				PushRemote{Name: value, Source: "remote.pushDefault"})
			require.Error(t, err)
			assert.NotContains(t, err.Error(), canary)
			// The key the user has to look at is still named.
			assert.Contains(t, err.Error(), "remote.pushDefault")
		})
	}
}

// --remote takes a name, and the argument takes a URL. A script reaching for
// $CI_REPOSITORY_URL gets the flag wrong, and that value carries a job token.
func TestSelectRemoteRefusesAURLGivenAsARemoteName(t *testing.T) {
	t.Parallel()
	const canary = "s3cr3t-canary-token"
	origin := Remote{Name: "origin", PushURLs: []string{"git@github.com:acme/app.git"}}

	_, err := SelectRemote([]Remote{origin}, "https://gitlab-ci-token:"+canary+"@gitlab.com/me/app.git",
		PushRemote{})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), canary)
	assert.Contains(t, err.Error(), "--remote takes the name of a Git remote, not a URL")

	// "@" is not the test: git accepts "we@ird" as a remote name, so a value
	// with no colon is looked up as the name it is.
	_, err = SelectRemote([]Remote{origin}, "we@ird", PushRemote{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `no remote named "we@ird"`)
}

// Fetching over https and pushing over ssh is an ordinary setup and names one
// repository. Comparing URL strings reported every such checkout as pointing at
// two places — and redacted the "git@" as though it were a credential.
func TestDivergesComparesRepositoriesNotURLs(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		fetch, push string
		want        bool
	}{
		"https fetch, ssh push, same repo": {
			"https://github.com/octo/storefront.git", "git@github.com:octo/storefront.git", false,
		},
		"dot-git on one side only": {
			"https://github.com/octo/storefront", "https://github.com/octo/storefront.git", false,
		},
		"ssh url versus scp-like": {
			"ssh://git@github.com/octo/storefront.git", "git@github.com:octo/storefront.git", false,
		},
		"different case": {
			"https://github.com/Octo/Storefront.git", "git@github.com:octo/storefront.git", false,
		},
		"genuinely different repositories": {
			"https://github.com/acme/upstream.git", "git@github.com:octo/storefront.git", true,
		},
		"one is not a GitHub repository at all": {
			"https://gitlab.com/octo/storefront.git", "git@github.com:octo/storefront.git", true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			remote := Remote{Name: "origin", FetchURL: tc.fetch, PushURLs: []string{tc.push}}
			assert.Equal(t, tc.want, remote.Diverges())
		})
	}
}

// configRunner answers the config reads behind PushRemote. An entry that is
// missing fails with a real exit status 1, which is how git reports an unset key
// and what tells that apart from a genuine failure.
func configRunner(outputs map[string]string) cliruntime.CommandRunner {
	return cliruntime.CommandRunnerFunc(func(_ context.Context, name string, args ...string) ([]byte, error) {
		command := strings.TrimSpace(name + " " + strings.Join(args, " "))
		if out, ok := outputs[command]; ok {
			return []byte(out), nil
		}
		return nil, exitStatus(1)
	})
}

// exitStatus builds a real *exec.ExitError carrying code, by running a command
// that exits with it. ProcessState cannot be constructed by hand.
func exitStatus(code int) error {
	err := exec.Command("sh", "-c", "exit "+strconv.Itoa(code)).Run()
	return err
}

// A malformed config, or any other real git failure, exits with something other
// than 1. Reading that as "not set" would let this command quietly disagree with
// the git the user runs.
func TestPushRemoteReportsARealGitFailure(t *testing.T) {
	t.Parallel()
	client := Client{runner: cliruntime.CommandRunnerFunc(
		func(_ context.Context, _ string, args ...string) ([]byte, error) {
			if args[0] == "branch" {
				return []byte("main\n"), nil
			}
			return nil, exitStatus(128)
		})}

	_, err := client.PushRemote(t.Context())
	require.ErrorIs(t, err, ErrGitUnavailable)
}

// The branch read can fail for the same reasons — dubious ownership, an
// unreadable config — and swallowing it would make a broken repository look
// like one with no push routing configured.
func TestPushRemoteReportsAFailingBranchRead(t *testing.T) {
	t.Parallel()
	client := Client{runner: cliruntime.CommandRunnerFunc(
		func(_ context.Context, _ string, args ...string) ([]byte, error) {
			if args[0] == "branch" {
				return nil, exitStatus(128)
			}
			return []byte("upstream\n"), nil
		})}

	_, err := client.PushRemote(t.Context())
	require.ErrorIs(t, err, ErrGitUnavailable)
}

// An unset key is an answer, not a failure, and git says so with exit 1 alone.
func TestPushRemoteTreatsAnUnsetKeyAsNoAnswer(t *testing.T) {
	t.Parallel()
	client := Client{runner: configRunner(map[string]string{"git branch --show-current": "main"})}

	push, err := client.PushRemote(t.Context())
	require.NoError(t, err)
	assert.Empty(t, push.Name)
}
