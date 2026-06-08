// Package upgrade implements the top-level CLI self-upgrade command.
package upgrade

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	"github.com/Kong/volcano-cli/internal/update"
	"github.com/Kong/volcano-cli/internal/version"
)

const (
	updateCheckTimeout = 2 * time.Second
	updateCheckMaxAge  = 24 * time.Hour
)

type noticeCache struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest"`
}

// New returns the upgrade command.
func New(deps cliruntime.Deps) *cobra.Command {
	var verifySignature bool
	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade Volcano CLI to the latest release",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts := updateOptions(deps)
			opts.RequireSignatureVerification = verifySignature
			return update.Upgrade(cmd.Context(), version.Version, cmd.OutOrStdout(), opts)
		},
	}
	cmd.Flags().BoolVar(&verifySignature, "verify-signature", false, "Require Sigstore signature verification with cosign")
	return cmd
}

func updateOptions(deps cliruntime.Deps) update.Options {
	return update.Options{
		HTTPClient:     deps.HTTPClient,
		GitHubAPIURL:   deps.UpdateGitHubAPIURL,
		ExecutablePath: deps.ExecutablePath,
		CommandRunner:  deps.UpdateCommandRunner,
	}
}

// MaybePrintUpdateNotice checks whether a newer CLI release is available and prints a non-blocking notice.
func MaybePrintUpdateNotice(cmd *cobra.Command, deps cliruntime.Deps) {
	if shouldSkipUpdateCheck(cmd) {
		return
	}
	if notice, ok := noticeFromCache(version.Version); ok {
		if notice != nil {
			printUpdateNotice(cmd, notice)
		}
		return
	}
	parent := cmd.Context()
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, updateCheckTimeout)
	defer cancel()
	release, err := update.LatestRelease(ctx, updateOptions(deps))
	if err != nil {
		return
	}
	latest := strings.TrimSpace(release.TagName)
	if latest == "" {
		return
	}
	writeNoticeCache(latest)
	newer, err := update.NewerThan(latest, version.Version)
	if err != nil || !newer {
		return
	}
	notice := &update.Notice{Current: version.Version, Latest: latest}
	printUpdateNotice(cmd, notice)
}

func shouldSkipUpdateCheck(cmd *cobra.Command) bool {
	if version.Version == "dev" {
		return true
	}
	if cmd.Flags().Changed("help") || cmd.Flags().Changed("version") {
		return true
	}
	// Skip when invoking the root command with no subcommand — cobra renders help, and
	// we don't want a network check to delay or pollute that output.
	if cmd.Parent() == nil && len(cmd.Flags().Args()) == 0 {
		return true
	}
	for c := cmd; c != nil; c = c.Parent() {
		if skipUpdateCheckCommand(c.Name()) {
			return true
		}
	}
	return false
}

func skipUpdateCheckCommand(name string) bool {
	switch name {
	case "version", "upgrade", "help", "completion", "__complete", "__completeNoDesc":
		return true
	default:
		return false
	}
}

func printUpdateNotice(cmd *cobra.Command, notice *update.Notice) {
	fmt.Fprintf(cmd.ErrOrStderr(), "A newer Volcano CLI version is available: %s (current %s). Run `volcano upgrade` to upgrade.\n", notice.Latest, notice.Current)
}

func noticeFromCache(current string) (*update.Notice, bool) {
	cache, err := readNoticeCache()
	if err != nil || cache.Latest == "" {
		return nil, false
	}
	// Use absolute age so future-dated CheckedAt values (clock skew, restored backups)
	// are treated as stale rather than fresh-forever.
	age := time.Since(cache.CheckedAt)
	if age < 0 {
		age = -age
	}
	if age > updateCheckMaxAge {
		return nil, false
	}
	newer, err := update.NewerThan(cache.Latest, current)
	if err != nil {
		return nil, false
	}
	if !newer {
		return nil, true
	}
	return &update.Notice{Current: current, Latest: cache.Latest}, true
}

func readNoticeCache() (*noticeCache, error) {
	path, err := noticeCachePath()
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var cache noticeCache
	if err := json.NewDecoder(f).Decode(&cache); err != nil {
		return nil, err
	}
	return &cache, nil
}

func writeNoticeCache(latest string) {
	path, err := noticeCachePath()
	if err != nil {
		return
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	// Write to a sibling temp file and rename, so concurrent CLI invocations can't
	// observe a half-written JSON file.
	f, err := os.CreateTemp(dir, "update-check-*.json")
	if err != nil {
		return
	}
	tmpPath := f.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := json.NewEncoder(f).Encode(noticeCache{CheckedAt: time.Now().UTC(), Latest: latest}); err != nil {
		_ = f.Close()
		return
	}
	if err := f.Close(); err != nil {
		return
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return
	}
	cleanup = false
}

func noticeCachePath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "volcano", "update-check.json"), nil
}
