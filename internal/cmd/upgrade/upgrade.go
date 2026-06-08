// Package upgrade implements the top-level CLI self-upgrade command.
package upgrade

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	if notice, ok := cachedNotice(version.Version); ok {
		printUpdateNotice(cmd, notice)
		return
	}
	parent := cmd.Context()
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, updateCheckTimeout)
	defer cancel()
	notice, err := update.CheckLatest(ctx, version.Version, updateOptions(deps))
	if err != nil {
		return
	}
	if notice == nil {
		return
	}
	writeNoticeCache(notice.Latest)
	printUpdateNotice(cmd, notice)
}

func shouldSkipUpdateCheck(cmd *cobra.Command) bool {
	if version.Version == "dev" {
		return true
	}
	if cmd.Flags().Changed("help") || cmd.Flags().Changed("version") {
		return true
	}
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

func cachedNotice(current string) (*update.Notice, bool) {
	cache, err := readNoticeCache()
	if err != nil || cache.Latest == "" || time.Since(cache.CheckedAt) > updateCheckMaxAge {
		return nil, false
	}
	newer, err := update.NewerThan(cache.Latest, current)
	if err != nil || !newer {
		return nil, false
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
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_ = json.NewEncoder(f).Encode(noticeCache{CheckedAt: time.Now().UTC(), Latest: latest})
}

func noticeCachePath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "volcano", "update-check.json"), nil
}
