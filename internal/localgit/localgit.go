// Package localgit reads the local Git repository through the git binary.
//
// It is deliberately read-only where credentials are concerned: nothing here
// writes a token into .git/config, and nothing here pushes. Pushing is always
// the user's own `git push`, with the credentials already on their machine.
package localgit

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

var (
	// ErrGitUnavailable indicates git could not report this directory's
	// remotes. The directory may not be a repository, git may be missing, or
	// git may have refused to read it — dubious ownership in a container,
	// an unreadable config. git's own message is appended whenever there is
	// one, because the fix differs sharply per cause and "not a git
	// repository" points nowhere near most of them.
	ErrGitUnavailable = errors.New("could not read this directory's Git repository")
	// ErrNoRemotes indicates a repository that has no remote configured.
	ErrNoRemotes = errors.New("no remote URLs found in your Git config")
	// ErrNotGitHub indicates a remote URL that does not point at github.com.
	ErrNotGitHub = errors.New("remote is not a github.com repository")
	// ErrUnparsableRemote indicates a remote URL that is not a recognizable Git URL.
	ErrUnparsableRemote = errors.New("remote URL could not be parsed")
)

// DefaultRemoteName is the remote picked when a repository has several and the
// caller did not name one.
const DefaultRemoteName = "origin"

// Remote is one configured Git remote.
type Remote struct {
	Name string
	URL  string
}

// Repository is a GitHub repository identified by a remote URL.
type Repository struct {
	Owner string
	Name  string
}

// FullName returns the owner/name form the API selects repositories by.
func (r Repository) FullName() string { return r.Owner + "/" + r.Name }

// Client runs git in the working directory.
type Client struct {
	runner cliruntime.CommandRunner
}

// New returns a client that runs the real git binary unless deps injects a
// runner.
func New(deps cliruntime.Deps) Client {
	runner := deps.GitCommandRunner
	if runner == nil {
		runner = cliruntime.CommandRunnerFunc(func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).Output() //nolint:gosec // command name and args are constants from this package
		})
	}
	return Client{runner: runner}
}

// Remotes returns the fetch remotes configured in the working directory,
// deduplicated by name and kept in git's own output order.
func (c Client) Remotes(ctx context.Context) ([]Remote, error) {
	out, err := c.runner.Run(ctx, "git", "remote", "-v")
	if err != nil {
		return nil, gitFailure(err)
	}

	remotes := make([]Remote, 0, 2)
	seen := make(map[string]struct{}, 2)
	for line := range strings.SplitSeq(string(out), "\n") {
		name, url, ok := parseRemoteLine(line)
		if !ok {
			continue
		}
		if _, duplicate := seen[name]; duplicate {
			continue
		}
		seen[name] = struct{}{}
		remotes = append(remotes, Remote{Name: name, URL: url})
	}

	if len(remotes) == 0 {
		return nil, ErrNoRemotes
	}
	return remotes, nil
}

// gitFailure surfaces git's own diagnosis when it gave one. exec.Output leaves
// it in ExitError.Stderr, and it is the only thing that distinguishes "run this
// somewhere else" from "run git config --global --add safe.directory".
func gitFailure(err error) error {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if message := strings.TrimSpace(string(exitErr.Stderr)); message != "" {
			return fmt.Errorf("%w: %s", ErrGitUnavailable, message)
		}
	}
	return ErrGitUnavailable
}

// parseRemoteLine reads one "<name>\t<url> (fetch|push)" line from `git remote
// -v`. Only fetch entries are kept: a remote configured with a separate push
// URL would otherwise be reported twice with different URLs, and the fetch URL
// is the one that identifies the repository.
func parseRemoteLine(line string) (name, url string, ok bool) {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) < 3 || fields[2] != "(fetch)" {
		return "", "", false
	}
	return fields[0], fields[1], true
}

// SelectRemote picks the remote to connect. A named remote must exist. With no
// name, a lone remote is taken as-is and "origin" wins among several; anything
// else is ambiguous and the caller has to choose.
func SelectRemote(remotes []Remote, name string) (Remote, error) {
	name = strings.TrimSpace(name)
	if name != "" {
		for _, remote := range remotes {
			if remote.Name == name {
				return remote, nil
			}
		}
		return Remote{}, fmt.Errorf("no remote named %q in this repository", name)
	}

	if len(remotes) == 1 {
		return remotes[0], nil
	}
	for _, remote := range remotes {
		if remote.Name == DefaultRemoteName {
			return remote, nil
		}
	}
	return Remote{}, fmt.Errorf(
		"this repository has %d remotes and none is named %q; pass the repository URL, or name one with --remote",
		len(remotes), DefaultRemoteName)
}

// ParseGitHubRepository resolves a Git remote URL to the GitHub repository it
// names. It accepts the three forms GitHub hands out — scp-like SSH
// (git@github.com:owner/repo.git), ssh:// URLs, and https:// URLs — and rejects
// anything hosted elsewhere, since the platform's only provider is GitHub.
func ParseGitHubRepository(rawURL string) (Repository, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return Repository{}, ErrUnparsableRemote
	}

	host, path, err := splitRemoteURL(rawURL)
	if err != nil {
		return Repository{}, fmt.Errorf("%w: %s", ErrUnparsableRemote, Redact(rawURL))
	}
	if !isGitHubHost(host) {
		return Repository{}, fmt.Errorf("%w: %s", ErrNotGitHub, Redact(rawURL))
	}

	owner, name, ok := splitOwnerRepo(path)
	if !ok {
		return Repository{}, fmt.Errorf("%w: %s", ErrUnparsableRemote, Redact(rawURL))
	}
	return Repository{Owner: owner, Name: name}, nil
}

// Redact removes any credential embedded in a remote URL, so a URL can be
// echoed back to the user. CI systems rewrite remotes to carry a job token —
// GitLab's is "https://gitlab-ci-token:<token>@…" — and an error message
// naming the remote would otherwise put that token in the build log.
func Redact(rawURL string) string {
	scheme, rest, found := strings.Cut(rawURL, "://")
	if !found {
		// scp-like: [user@]host:path carries no password field.
		return rawURL
	}
	authority, remainder, hasPath := strings.Cut(rest, "/")
	userInfo, hostPort, hasUserInfo := strings.Cut(authority, "@")
	if !hasUserInfo {
		return rawURL
	}

	// Keep the user name: it is not the secret, and it tells the user which
	// remote this is. Only a password field is dropped.
	if name, _, hasPassword := strings.Cut(userInfo, ":"); hasPassword {
		authority = name + ":***@" + hostPort
	}
	redacted := scheme + "://" + authority
	if hasPath {
		redacted += "/" + remainder
	}
	return redacted
}

// splitRemoteURL separates a remote URL's host from its path without going
// through net/url, which does not understand the scp-like SSH form.
func splitRemoteURL(rawURL string) (host, path string, err error) {
	if scheme, rest, found := strings.Cut(rawURL, "://"); found {
		switch strings.ToLower(scheme) {
		case "ssh", "git", "http", "https":
		default:
			return "", "", ErrUnparsableRemote
		}
		authority, remainder, _ := strings.Cut(rest, "/")
		return stripUserInfo(authority), remainder, nil
	}

	// scp-like: [user@]host:path — the colon separates host from path, and the
	// path is relative, so a port cannot appear here.
	authority, remainder, found := strings.Cut(rawURL, ":")
	if !found {
		return "", "", ErrUnparsableRemote
	}
	return stripUserInfo(authority), remainder, nil
}

// stripUserInfo drops a leading "user@" and any ":port" suffix, leaving a bare
// hostname to compare against github.com.
func stripUserInfo(authority string) string {
	if _, after, found := strings.Cut(authority, "@"); found {
		authority = after
	}
	if before, _, found := strings.Cut(authority, ":"); found {
		authority = before
	}
	return strings.ToLower(authority)
}

func isGitHubHost(host string) bool {
	return host == "github.com" || host == "www.github.com"
}

// splitOwnerRepo reads the owner and repository name out of a remote URL's
// path, tolerating a leading slash, a trailing slash, and the .git suffix.
//
// A query string or fragment is dropped first: copying a repository's address
// out of a browser yields ".../storefront?tab=readme-ov-file", and carrying
// that into the name would send the user off to fix a repository-access
// problem that does not exist.
func splitOwnerRepo(path string) (owner, name string, ok bool) {
	path, _, _ = strings.Cut(path, "?")
	path, _, _ = strings.Cut(path, "#")
	path = strings.Trim(path, "/")
	path = trimGitSuffix(path)

	owner, name, found := strings.Cut(path, "/")
	if !found || !validOwner(owner) || !validRepositoryName(name) {
		return "", "", false
	}
	return owner, name, true
}

// trimGitSuffix removes a trailing ".git" whatever its case, leaving a
// repository that is itself named "something.git" intact.
func trimGitSuffix(path string) string {
	if len(path) >= 4 && strings.EqualFold(path[len(path)-4:], ".git") {
		return path[:len(path)-4]
	}
	return path
}

// validOwner and validRepositoryName hold the parse to what GitHub actually
// accepts. They are the backstop that turns a URL this package mis-split into
// an error rather than a plausible-looking wrong answer — a port in a
// scp-like URL ("host:22:owner/repo") lands in the owner, for instance.
func validOwner(owner string) bool {
	if owner == "" {
		return false
	}
	for _, r := range owner {
		if !isASCIIAlphanumeric(r) && r != '-' {
			return false
		}
	}
	return true
}

func validRepositoryName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if !isASCIIAlphanumeric(r) && r != '-' && r != '_' && r != '.' {
			return false
		}
	}
	return true
}

func isASCIIAlphanumeric(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}
