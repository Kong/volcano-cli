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
	"strconv"
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
	//
	// An empty list is returned as one rather than as ErrNoRemotes. Having no
	// remote is only a failure for a caller that needed one, and a checkout with
	// no remotes at all can still have a push route: remote.pushDefault holding
	// a URL is one, and `git push` follows it. SelectRemote decides.
	return remotes, nil
}

// PushRemote names the remote a `git push` with no arguments would send to, and
// the config key that decided it. Both are empty when the configuration does not
// say, which is the common case.
//
// git resolves this in a precedence git help config spells out —
// branch.<name>.pushRemote, then remote.pushDefault, then branch.<name>.remote —
// and only falls back to origin when none is set. Picking origin regardless
// would bind the repository a triangular or fork checkout never pushes to.
type PushRemote struct {
	Name string
	// Source is the config key that set Name, so a configuration pointing
	// somewhere unusable can be reported by the key the user has to fix.
	Source string
	// RewrittenURL is Name after git's push URL rewriting, and is empty when no
	// rule matched — the ordinary case. It only applies when Name is used as a
	// URL rather than as a remote name, because git looks the name up first.
	RewrittenURL string
}

// PushRemote reads the push routing out of the repository's configuration.
func (c Client) PushRemote(ctx context.Context) (PushRemote, error) {
	branch, err := c.currentBranch(ctx)
	if err != nil {
		return PushRemote{}, err
	}

	keys := make([]string, 0, 3)
	if branch != "" {
		keys = append(keys, "branch."+branch+".pushRemote")
	}
	keys = append(keys, "remote.pushDefault")
	if branch != "" {
		keys = append(keys, "branch."+branch+".remote")
	}

	for _, key := range keys {
		value, set, err := c.configValue(ctx, key)
		if err != nil {
			return PushRemote{}, err
		}
		if !set {
			continue
		}
		if err := checkPushRoute(key, value); err != nil {
			return PushRemote{}, err
		}

		rewritten, err := c.rewritePushURL(ctx, value)
		if err != nil {
			return PushRemote{}, err
		}
		return PushRemote{Name: value, Source: key, RewrittenURL: rewritten}, nil
	}
	return PushRemote{}, nil
}

// checkPushRoute refuses a value git cannot push to. A key that is set decides
// the route even when its value cannot work: git does not fall through to the
// next key or to origin, it fails — measured, not assumed. So treating one of
// these as "not configured" would bind whatever the convention picked, for a
// checkout that cannot push at all.
func checkPushRoute(key, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		// git: "fatal: No configured push destination." for an empty value, and
		// "'   ' does not appear to be a git repository" for whitespace.
		return fmt.Errorf(
			"%s is set to an empty value, so this branch has no push destination; "+
				"unset it, name a remote with --remote, or pass the repository URL", key)
	}
	if value != trimmed {
		// git uses the value verbatim, so the padding is part of the path it
		// looks for: "' origin ' does not appear to be a git repository".
		return fmt.Errorf(
			"%s is set to %s, and git uses that verbatim — the surrounding whitespace makes it "+
				"a repository that does not exist; fix that setting, name a remote with --remote, "+
				"or pass the repository URL", key, describeConfigValue(value))
	}
	return nil
}

// rewritePushURL resolves what a push to this value actually reaches, and
// returns "" when no rule matched it — which is the ordinary case. A rule that
// matches and rewrites the value to itself returns that value, not "".
//
// git rewrites a push destination before using it: url.<base>.pushInsteadOf
// replaces a matching prefix, url.<base>.insteadOf does so when no
// pushInsteadOf matches, and the longest matching prefix wins. All three
// measured against git, not read off the documentation. Binding the configured
// value would bind a repository the push never reaches.
//
// Only a value used as a URL needs this. A named remote needs nothing: `git
// remote -v` already prints the rewritten URL on its (push) line.
func (c Client) rewritePushURL(ctx context.Context, value string) (string, error) {
	out, err := c.runner.Run(ctx, "git", "config", "--get-regexp", `^url\..*\.(push)?insteadof$`)
	if err != nil {
		if isUnsetConfigKey(err) {
			return "", nil // no rewrite rules configured
		}
		return "", gitFailure(err)
	}

	// Two passes rather than one map, because a pushInsteadOf match wins over any
	// insteadOf match however much shorter it is.
	push, fetch := map[string]string{}, map[string]string{}
	for line := range strings.SplitSeq(string(out), "\n") {
		key, prefix, found := strings.Cut(strings.TrimRight(line, "\r"), " ")
		if !found || prefix == "" {
			continue
		}
		// The key is "url." + base + "." + variable, and the base is a URL
		// prefix full of dots and colons, so it is cut from both ends. git
		// lowercases the variable name in this output; the base it prints
		// verbatim.
		base, ok := strings.CutPrefix(key, "url.")
		if !ok {
			continue
		}
		// First wins, so a repeated prefix keeps the earliest rule. git reads
		// config in file order and `git config --add` appends, and a push then
		// follows the first of two rules sharing a prefix — measured. Assigning
		// over the earlier one resolved the second while git used the first.
		if trimmed, ok := strings.CutSuffix(base, ".pushinsteadof"); ok {
			if _, seen := push[prefix]; !seen {
				push[prefix] = trimmed
			}
			continue
		}
		if trimmed, ok := strings.CutSuffix(base, ".insteadof"); ok {
			if _, seen := fetch[prefix]; !seen {
				fetch[prefix] = trimmed
			}
		}
	}

	if rewritten, ok := longestPrefixRewrite(value, push); ok {
		return rewritten, nil
	}
	rewritten, _ := longestPrefixRewrite(value, fetch)
	return rewritten, nil
}

// longestPrefixRewrite replaces the longest matching prefix of value with the
// base configured for it, reporting whether any rule matched.
//
// Whether a rule matched, not whether it changed the value: git honours a
// matching pushInsteadOf rather than falling through to insteadOf even when the
// rule rewrites the URL to itself — measured. Treating an identity rewrite as
// "no rewrite" reached the fetch rule and resolved a different repository.
func longestPrefixRewrite(value string, rules map[string]string) (string, bool) {
	longest, rewritten := -1, ""
	for prefix, base := range rules {
		if len(prefix) > longest && strings.HasPrefix(value, prefix) {
			longest, rewritten = len(prefix), base+value[len(prefix):]
		}
	}
	return rewritten, longest >= 0
}

func (c Client) currentBranch(ctx context.Context) (string, error) {
	// Detached HEAD prints nothing and succeeds, so an empty answer here is
	// "no branch", not a failure.
	out, err := c.runner.Run(ctx, "git", "branch", "--show-current")
	if err != nil {
		return "", gitFailure(err)
	}
	return strings.TrimSpace(string(out)), nil
}

// configValue reads one git config key, reporting whether it is set at all. An
// unset key is not a failure — it means the configuration does not say, which is
// an answer — and git says so with exit status 1 specifically. Anything else is a
// real failure (a malformed config exits 128), and reporting it as "not set"
// would let this command quietly disagree with the git the user runs.
//
// Only the record terminator git appends is removed. The value is not trimmed,
// because git does not trim it either: " origin " is not the origin remote but a
// path git reports as not a repository, and a set-but-empty value is not the same
// as an unset one. Trimming erased both distinctions and turned them into a
// fallback.
func (c Client) configValue(ctx context.Context, key string) (value string, set bool, err error) {
	out, err := c.runner.Run(ctx, "git", "config", "--get", key)
	switch {
	case err == nil:
		return trimRecordTerminator(string(out)), true, nil
	case isUnsetConfigKey(err):
		return "", false, nil
	default:
		return "", false, gitFailure(err)
	}
}

// trimRecordTerminator removes the one line ending git puts after a value, and
// nothing else.
//
// A cutset is wrong here: git's config syntax admits a value ending in a newline
// (`pushdefault = "origin\n"`), for which `git config --get` emits "origin\n\n".
// Stripping both turned it into the valid remote "origin" and bound that
// repository, while `git push` refuses the value outright — "fatal: 'origin
// ' does not appear to be a git repository". One terminator off means the value
// keeps its own newline and is refused as the padded value it is.
//
// The "\r" goes with it because Windows is a release target and a terminator
// there may be "\r\n"; leaving the "\r" behind would refuse a perfectly good
// value as padded. A value genuinely ending in "\r" is indistinguishable from
// that and is accepted, which git would refuse — the rarer of the two mistakes.
func trimRecordTerminator(out string) string {
	return strings.TrimSuffix(strings.TrimSuffix(out, "\n"), "\r")
}

// isUnsetConfigKey reports whether err is `git config --get` saying the key is
// not set, which it signals with exit status 1 and nothing else.
func isUnsetConfigKey(err error) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() == 1
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

// SelectRemote picks the push destination to connect. A named remote must exist.
// With no name, where git would push wins, then a lone remote, then "origin";
// anything else is ambiguous and the caller has to choose.
//
// push comes from Client.PushRemote and is what makes this agree with git rather
// than with a convention: in a fork checkout the repository that receives pushes
// — and so the one a deployment comes from — is routinely not origin.
func SelectRemote(remotes []Remote, name string, push PushRemote) (Remote, error) {
	if name = strings.TrimSpace(name); name != "" {
		// --remote names a remote; the argument takes a URL. A script passing
		// $CI_REPOSITORY_URL here hits this, and that value carries a job
		// token, so it is not echoed once it looks like a URL.
		if looksLikeURL(name) {
			return Remote{}, fmt.Errorf(
				"--remote takes the name of a Git remote, not a URL (%s); "+
					"pass the repository URL as the argument instead", Redact(name))
		}
		if len(remotes) == 0 {
			return Remote{}, ErrNoRemotes
		}
		return namedRemote(remotes, name)
	}

	// Before the remote list, because a push route does not need one: git
	// follows a URL in these keys out of a checkout with no remotes at all.
	if push.Name != "" {
		return pushDestination(remotes, push)
	}

	// Nothing named a destination and there is no remote to fall back on. This
	// is the one place the empty list is a failure.
	if len(remotes) == 0 {
		return Remote{}, ErrNoRemotes
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

// pushDestination resolves what git's push configuration points at.
//
// git looks the value up as a remote name and, finding none, uses it as a URL:
// git-push(1) documents its repository as "either a URL or the name of a
// remote", and remote.pushDefault, branch.<name>.pushRemote and
// branch.<name>.remote all feed it. A URL there is a working push route, so
// refusing it would refuse a checkout that deploys perfectly well.
//
// What must not happen is falling back to origin, which binds a repository this
// checkout never pushes to — the whole failure this routing exists to avoid.
func pushDestination(remotes []Remote, push PushRemote) (Remote, error) {
	// The name first, because that is git's order: it looks the value up as a
	// remote and only treats it as a URL when no remote matches. A rewrite rule
	// does not apply to a remote name.
	if remote, err := namedRemote(remotes, push.Name); err == nil {
		return remote, nil
	}

	// Used as a URL, so git's rewriting applies: what a push reaches is not
	// necessarily what the key says.
	target := push.Name
	if push.RewrittenURL != "" {
		target = push.RewrittenURL
	}

	_, err := ParseGitHubRepository(target)
	switch {
	case err == nil:
		return directPush(target), nil
	case errors.Is(err, ErrNotGitHub):
		// The push route works; it just does not lead anywhere Volcano can
		// deploy from. Saying "fix that setting" would be wrong advice.
		return Remote{}, fmt.Errorf(
			"%s sends this branch's pushes to %s, which is not a github.com repository; "+
				"pass a GitHub repository URL, or name a remote with --remote",
			push.Source, Redact(target))
	default:
		return Remote{}, fmt.Errorf(
			"%s names %s, which is neither a remote in this repository nor a repository URL, "+
				"so there is nothing to connect; fix that setting, name a remote with --remote, "+
				"or pass the repository URL",
			push.Source, describeConfigValue(target))
	}
}

// directPush is the destination for a push configuration holding a URL rather
// than a remote name. It has no name because no remote in this repository
// describes it, and both URLs are the same one: there is no fetch side to
// diverge from.
func directPush(url string) Remote {
	return Remote{FetchURL: url, PushURLs: []string{url}}
}

// Named reports whether this destination is one of the repository's configured
// remotes. An unnamed one came from a URL in git's push configuration, and has
// to be described by its redacted URL rather than by a remote name that does
// not exist.
func (r Remote) Named() bool { return r.Name != "" }

// looksLikeURL reports whether a value given where a remote name belongs is
// really a URL. A colon is the test: git refuses it in a remote name ("fatal:
// 'we:ird' is not a valid remote name"), and every Git URL form has one — after
// the scheme, or between host and path in the scp-like form. "@" is not a test,
// however tempting: "we@ird" is a name git accepts.
func looksLikeURL(value string) bool { return strings.Contains(value, ":") }

// describeConfigValue renders a push configuration value for a message. A remote
// name is quoted as it stands — the user chose that label, and naming it is what
// makes the error actionable. Anything URL-shaped goes through Redact instead,
// since git accepts a URL in these keys and a CI rewrite puts a job token in one.
func describeConfigValue(value string) string {
	if looksLikeURL(value) {
		return Redact(value)
	}
	return strconv.Quote(value)
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
