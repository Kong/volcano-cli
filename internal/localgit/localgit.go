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
	// FetchURL is where the remote fetches from.
	FetchURL string
	// PushURLs is every destination a push to this remote reaches. git accepts
	// several `remote.<name>.pushurl` entries and a single push updates all of
	// them, so a remote with more than one names no single repository to bind.
	// git prints the fetch URL as the sole push URL unless a pushurl is set.
	PushURLs []string
}

// PushTarget returns the one repository a push to this remote reaches. A push is
// what triggers a deployment, so this — not the fetch URL — is what a binding
// has to name; a remote that reaches several has no answer, and choosing one
// would bind a repository while pushes kept updating the others.
func (r Remote) PushTarget() (string, error) {
	switch len(r.PushURLs) {
	case 1:
		return r.PushURLs[0], nil
	case 0:
		return "", fmt.Errorf("remote %q has nothing to push to", r.Name)
	default:
		redacted := make([]string, 0, len(r.PushURLs))
		for _, url := range r.PushURLs {
			redacted = append(redacted, Redact(url))
		}
		return "", fmt.Errorf(
			"remote %q pushes to %d repositories (%s), so there is no single one to connect; "+
				"pass the repository URL to choose",
			r.Name, len(r.PushURLs), strings.Join(redacted, ", "))
	}
}

// Diverges reports whether this remote fetches from a different repository than
// the single one it pushes to.
//
// Repositories, not URL strings: fetching over https and pushing over ssh is an
// ordinary setup — `git remote set-url --push` and url.<base>.pushInsteadOf both
// produce it — and the two spellings name the same repository. Comparing the
// strings would report every such checkout as pointing at two places.
func (r Remote) Diverges() bool {
	if len(r.PushURLs) != 1 || r.FetchURL == "" {
		return false
	}

	push, pushErr := ParseGitHubRepository(r.PushURLs[0])
	fetch, fetchErr := ParseGitHubRepository(r.FetchURL)
	if pushErr != nil || fetchErr != nil {
		// One of them is not a GitHub repository at all, which is a difference
		// worth reporting whatever the strings look like.
		return r.FetchURL != r.PushURLs[0]
	}
	return !strings.EqualFold(push.FullName(), fetch.FullName())
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

// Remotes returns the remotes configured in the working directory, in git's own
// output order, each carrying both of its URLs.
func (c Client) Remotes(ctx context.Context) ([]Remote, error) {
	out, err := c.runner.Run(ctx, "git", "remote", "-v")
	if err != nil {
		return nil, gitFailure(err)
	}

	remotes := make([]Remote, 0, 2)
	index := make(map[string]int, 2)
	for line := range strings.SplitSeq(string(out), "\n") {
		name, url, kind, ok := parseRemoteLine(line)
		if !ok {
			continue
		}

		position, seen := index[name]
		if !seen {
			index[name] = len(remotes)
			remotes = append(remotes, Remote{Name: name})
			position = index[name]
		}
		switch kind {
		case remotePush:
			// Appended, not assigned: several pushurl entries produce several
			// push lines, and keeping only the last would silently bind one
			// repository while pushes went to all of them.
			remotes[position].PushURLs = append(remotes[position].PushURLs, url)
		case remoteFetch:
			remotes[position].FetchURL = url
		case remoteNone:
		}
	}

	// A remote with no push line is kept: dropping it made a named lookup deny
	// a remote git still lists, and quietly moved the selection to another one.
	// PushTarget is where having nothing to push to is reported.
	if len(remotes) == 0 {
		return nil, ErrNoRemotes
	}
	return remotes, nil
}

// PushRemote reports the remote a `git push` with no arguments would send to,
// or "" when the configuration does not say. git decides this in a precedence
// git help config spells out — branch.<name>.pushRemote, then
// remote.pushDefault, then branch.<name>.remote — and only falls back to origin
// when none is set. Picking origin regardless would bind the repository a
// triangular or fork checkout never pushes to.
func (c Client) PushRemote(ctx context.Context) string {
	branch := c.configValue(ctx, "branch")
	if branch != "" {
		if remote := c.configValue(ctx, "branch."+branch+".pushRemote"); remote != "" {
			return remote
		}
	}
	if remote := c.configValue(ctx, "remote.pushDefault"); remote != "" {
		return remote
	}
	if branch != "" {
		return c.configValue(ctx, "branch."+branch+".remote")
	}
	return ""
}

// configValue reads one git config key, or the current branch name for the
// sentinel "branch". An unset key exits non-zero, which is not a failure worth
// reporting: it means the configuration does not say, which is the answer.
func (c Client) configValue(ctx context.Context, key string) string {
	args := []string{"config", "--get", key}
	if key == "branch" {
		args = []string{"branch", "--show-current"}
	}
	out, err := c.runner.Run(ctx, "git", args...)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

type remoteKind int

const (
	// remoteNone is a line that names a remote and gives it no URL. git prints
	// one for a remote whose url is unset, and the name has to be kept: denying
	// a remote git still lists moved the selection elsewhere in silence.
	remoteNone remoteKind = iota
	remoteFetch
	remotePush
)

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
// -v`, reporting which of the two it is. Both are kept: a remote with a
// separate pushurl names two different repositories, and which one matters
// depends on the question being asked.
//
// The URL is taken as everything between the tab and the trailing marker, not
// as a whitespace-delimited field: git stores and prints a remote URL verbatim,
// so one containing a space would otherwise be skipped entirely — and a skipped
// remote does not fail, it quietly changes which remote gets selected.
func parseRemoteLine(line string) (name, url string, kind remoteKind, ok bool) {
	name, rest, found := strings.Cut(strings.TrimRight(line, "\r\n"), "\t")
	if !found || name == "" {
		return "", "", remoteNone, false
	}
	if rest == "" {
		return name, "", remoteNone, true
	}
	if url, found = strings.CutSuffix(rest, " (push)"); found {
		return name, url, remotePush, url != ""
	}
	if url, found = strings.CutSuffix(rest, " (fetch)"); found {
		return name, url, remoteFetch, url != ""
	}
	return "", "", remoteNone, false
}

// SelectRemote picks the remote to connect. A named remote must exist. With no
// name, the remote git would push to wins, then a lone remote, then "origin";
// anything else is ambiguous and the caller has to choose.
//
// pushRemote comes from Client.PushRemote and is what makes this agree with
// git rather than with a convention: in a fork checkout the repository that
// receives pushes — and so the one a deployment comes from — is routinely not
// origin.
func SelectRemote(remotes []Remote, name, pushRemote string) (Remote, error) {
	if name = strings.TrimSpace(name); name != "" {
		return namedRemote(remotes, name)
	}

	if pushRemote != "" {
		if remote, err := namedRemote(remotes, pushRemote); err == nil {
			return remote, nil
		}
		// A pushDefault naming a remote that does not exist is a broken
		// configuration git would fail on too; fall through rather than making
		// this command the one that reports it.
	}

	if len(remotes) == 1 {
		return remotes[0], nil
	}
	for _, remote := range remotes {
		if remote.Name == DefaultRemoteName {
			return remote, nil
		}
	}

	names := make([]string, 0, len(remotes))
	for _, remote := range remotes {
		names = append(names, remote.Name)
	}
	return Remote{}, fmt.Errorf(
		"this repository has %d remotes (%s) and none is named %q; pass the repository URL, or name one with --remote",
		len(remotes), strings.Join(names, ", "), DefaultRemoteName)
}

func namedRemote(remotes []Remote, name string) (Remote, error) {
	for _, remote := range remotes {
		if remote.Name == name {
			return remote, nil
		}
	}
	return Remote{}, fmt.Errorf("no remote named %q in this repository", name)
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

	host, path, ok := splitRemoteURL(rawURL)
	if !ok {
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

// Placeholder stands in for a remote URL whose parts could not be identified,
// so that none of its contents are echoed.
const Placeholder = "[redacted: unrecognized remote URL]"

// Redact renders a remote URL safe to print. CI systems rewrite remotes to
// carry a job token — GitLab's is "https://gitlab-ci-token:<token>@…", and the
// url.<base>.insteadOf idiom injects one into any URL — so a message naming the
// remote would otherwise put that token in the build log.
//
// A recognized URL comes back with its whole userinfo removed, not just a
// password field: a token is as often the entire userinfo
// ("https://<pat>@github.com/owner/repo.git" is GitHub's own documented form),
// and nothing distinguishes that from a user name.
//
// Anything whose parts cannot be identified comes back as Placeholder rather
// than verbatim. git accepts remotes whose "URL" is a command line
// ("ext::ssh -o Password=… %S repo.git"), a local path, or — when an argument
// was mistyped — a bare token, and none of those can be safely echoed.
func Redact(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	scheme, rest, found := strings.Cut(rawURL, "://")
	if !found {
		return redactScpLike(rawURL)
	}

	authority, remainder, hasPath := strings.Cut(rest, "/")
	// An "@" past the authority means the split above cannot be trusted to have
	// captured the whole userinfo: a password containing "/" pushes the rest of
	// it into the path. Nothing about such a URL is echoed.
	if strings.Contains(remainder, "@") {
		return Placeholder
	}
	// A query or fragment carries credentials of its own — GitLab accepts
	// "?private_token=…" — and neither is part of what identifies the remote.
	remainder = trimQueryAndFragment(remainder)
	userInfo, hostPort, hasUserInfo := strings.Cut(authority, "@")
	if hasUserInfo {
		if !looksLikeHost(hostPort) {
			return Placeholder
		}
		authority = "***@" + hostPort
	} else if !looksLikeHost(userInfo) {
		return Placeholder
	}

	redacted := scheme + "://" + authority
	if hasPath {
		redacted += "/" + remainder
	}
	return redacted
}

// redactScpLike handles the "[user@]host:path" form. The userinfo goes here for
// the same reason it goes from a URL: "TOKEN@gitlab.com:octo/repo.git" is a
// remote a script can produce, and nothing tells that apart from the "git@…" a
// user typed. Anything without this shape is not echoed at all.
func redactScpLike(rawURL string) string {
	authority, path, found := strings.Cut(rawURL, ":")
	if !found || strings.Contains(path, "@") {
		return Placeholder
	}

	_, host, hasUserInfo := strings.Cut(authority, "@")
	if !hasUserInfo {
		host = authority
	}
	if !looksLikeHost(host) {
		return Placeholder
	}
	path = trimQueryAndFragment(path)
	if hasUserInfo {
		return "***@" + host + ":" + path
	}
	return host + ":" + path
}

// trimQueryAndFragment drops everything from the first "?" or "#", whichever
// comes first.
func trimQueryAndFragment(path string) string {
	path, _, _ = strings.Cut(path, "?")
	path, _, _ = strings.Cut(path, "#")
	return path
}

// looksLikeHost reports whether s could be a host[:port]. It is deliberately
// narrow — a dot is required — because its job is to tell a hostname apart from
// the first word of a command line or the head of a filesystem path.
func looksLikeHost(s string) bool {
	if s == "" || !strings.Contains(s, ".") {
		return false
	}
	for _, r := range s {
		switch {
		case isASCIIAlphanumeric(r), r == '.', r == '-', r == ':', r == '[', r == ']':
		default:
			return false
		}
	}
	return true
}

// splitRemoteURL separates a remote URL's host from its path without going
// through net/url, which does not understand the scp-like SSH form.
func splitRemoteURL(rawURL string) (host, path string, ok bool) {
	if scheme, rest, found := strings.Cut(rawURL, "://"); found {
		switch asciiLower(scheme) {
		case "ssh", "git", "http", "https":
		default:
			return "", "", false
		}
		authority, remainder, _ := strings.Cut(rest, "/")
		// An "@" in the path means the authority split cannot be trusted: a
		// credential containing "/" spills into it, and the host read out of
		// what is left would be wrong. Neither a GitHub owner nor a repository
		// name may contain "@", so nothing legitimate is refused here.
		if strings.Contains(remainder, "@") {
			return "", "", false
		}
		return stripUserInfo(authority), remainder, true
	}

	// scp-like: [user@]host:path — the colon separates host from path, and the
	// path is relative, so a port cannot appear here.
	authority, remainder, found := strings.Cut(rawURL, ":")
	if !found {
		return "", "", false
	}
	return stripUserInfo(authority), remainder, true
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
	return asciiLower(authority)
}

// asciiLower folds only ASCII, leaving every other byte alone. Unicode folding
// cannot be used to normalize a host for comparison: strings.ToLower maps
// U+0130 (İ) to ASCII "i", so "gİthub.com" would fold to "github.com" and pass
// a check it must fail. isGitHubHost rejects the unfolded byte on its own.
func asciiLower(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}, s)
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
