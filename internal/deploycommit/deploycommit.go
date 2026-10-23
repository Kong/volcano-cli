// Package deploycommit reports which commit a cloud deploy's source came from,
// so Volcano can tie the deployment to its repository and LavaFlow can open
// fix pull requests against the code that actually runs.
//
// It sends only the GitHub repository as owner/name, the commit ID, and
// whether the uploaded files differ from that commit: never a remote URL, a
// path, a branch, or a diff. When any part is missing or would not pass the
// server's check, it reports nothing at all. The server refuses a malformed
// commit with a 400 before anything deploys, so sending a doubtful one would
// fail the customer's deploy over a field they never asked for.
package deploycommit

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha1" //nolint:gosec // git names a blob by its SHA-1; this matches it, it does not secure anything
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/localgit"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// OptOutEnv turns reporting off when set to "off".
const OptOutEnv = "VOLCANO_DEPLOY_COMMIT"

// gitTimeout bounds each git call, so a slow or hung git never holds up a
// deploy; a timeout reports nothing, like any other failure.
const gitTimeout = 10 * time.Second

// The server's own checks, so a value it would refuse is never sent.
var (
	shaPattern   = regexp.MustCompile(`^[0-9a-f]{40}$`)
	ownerPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})$`)
	namePattern  = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
)

// Source is the commit checked out where a deploy reads its files. Detect
// reads it once per deploy; Commit then reports it for each archive the
// deploy uploads.
type Source struct {
	repository string
	sha        string
	// blobs holds every file in the commit by its git blob ID.
	blobs map[string]struct{}
}

// Detect reads the commit checked out in dir: origin's GitHub repository, HEAD,
// and the files HEAD holds. It returns nil when it cannot say for sure, and
// in local development, which never reports a commit.
//
// The commit comes from dir itself, in CI as anywhere else. A workflow's
// GITHUB_SHA names the commit that triggered it, which is not what a checkout
// of another ref or repository holds. On a pull_request run HEAD is the merge
// commit GitHub made for the check, which is on no branch, so the server
// records it unverified: true, and nothing more is claimed.
func Detect(ctx context.Context, deps cliruntime.Deps, dir string) *Source {
	if deps.LocalMode || strings.EqualFold(strings.TrimSpace(os.Getenv(OptOutEnv)), "off") {
		return nil
	}
	git := gitRunner(deps)

	remote, err := run(ctx, git, dir, "remote", "get-url", "origin")
	if err != nil {
		return nil
	}
	parsed, err := localgit.ParseGitHubRepository(remote)
	if err != nil {
		return nil
	}
	head, err := run(ctx, git, dir, "rev-parse", "HEAD")
	if err != nil {
		return nil
	}
	repository, sha := parsed.FullName(), strings.ToLower(strings.TrimSpace(head))
	if !valid(repository, sha) {
		return nil
	}

	tree, err := run(ctx, git, dir, "ls-tree", "-r", "-z", "--full-tree", "HEAD")
	if err != nil {
		return nil
	}
	return &Source{repository: repository, sha: sha, blobs: treeBlobs(tree)}
}

// Commit reports the commit for one uploaded archive, a gzip-compressed tar.
// The archive is clean only when every file in it has the content of a file
// in the commit. Anything else the packager took — an untracked or ignored
// file, or an edit git's own status would not show — marks it dirty. skip
// names entries the CLI writes itself rather than reads from the project. It
// returns nil when s is nil or the archive cannot be read.
func (s *Source) Commit(archive []byte, skip ...string) *api.DeployCommit {
	if s == nil {
		return nil
	}
	dirty, err := s.differs(archive, skip)
	if err != nil {
		return nil
	}
	return &api.DeployCommit{Repository: s.repository, SHA: s.sha, Dirty: dirty}
}

func (s *Source) differs(archive []byte, skip []string) (bool, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return false, err
	}
	defer func() { _ = gz.Close() }()
	entries := tar.NewReader(gz)
	for {
		header, err := entries.Next()
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if header.Typeflag != tar.TypeReg || slices.Contains(skip, strings.TrimPrefix(header.Name, "./")) {
			continue
		}
		blob, err := blobID(entries, header.Size)
		if err != nil {
			return false, err
		}
		if _, ok := s.blobs[blob]; !ok {
			return true, nil
		}
	}
}

// blobID is the ID git gives a file with this content.
func blobID(content io.Reader, size int64) (string, error) {
	hash := sha1.New() //nolint:gosec // see the import
	fmt.Fprintf(hash, "blob %d\x00", size)
	if _, err := io.Copy(hash, content); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// treeBlobs reads `git ls-tree -r -z` output: "<mode> <type> <object>\t<path>"
// records separated by NUL.
func treeBlobs(tree string) map[string]struct{} {
	blobs := map[string]struct{}{}
	for record := range strings.SplitSeq(tree, "\x00") {
		meta, _, ok := strings.Cut(record, "\t")
		fields := strings.Fields(meta)
		if ok && len(fields) == 3 && fields[1] == "blob" {
			blobs[fields[2]] = struct{}{}
		}
	}
	return blobs
}

// valid applies the server's checks: a GitHub owner/name and a full SHA-1
// commit ID. A SHA-256 repository's 64-character IDs do not pass.
func valid(repository, sha string) bool {
	owner, name, ok := strings.Cut(repository, "/")
	return ok && ownerPattern.MatchString(owner) && namePattern.MatchString(name) &&
		name != "." && name != ".." && shaPattern.MatchString(sha)
}

func run(ctx context.Context, git cliruntime.CommandRunner, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	out, err := git.Run(ctx, "git", append([]string{"-C", dir}, args...)...)
	return string(out), err
}

func gitRunner(deps cliruntime.Deps) cliruntime.CommandRunner {
	if deps.GitCommandRunner != nil {
		return deps.GitCommandRunner
	}
	return cliruntime.CommandRunnerFunc(func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, args...).Output() //nolint:gosec // the command is git; its arguments are constants and the deploy's own directory
	})
}
