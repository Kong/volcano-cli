package update

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func latestDownloadRelease(ctx context.Context, opts Options) (*Release, error) {
	base := strings.TrimRight(strings.TrimSpace(opts.DownloadURL), "/")
	if base == "" {
		base = defaultDownloadURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/latest-version", http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create release request: %w", err)
	}
	req.Header.Set("User-Agent", "volcano-cli")
	resp, err := releaseHTTPClient(opts).Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch latest release: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch latest release: server returned %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 128))
	if err != nil {
		return nil, fmt.Errorf("read latest release: %w", err)
	}
	tag := strings.TrimSpace(string(body))
	if _, err := parseStableVersion(tag); err != nil || !strings.HasPrefix(tag, "v") || len(body) == 128 {
		return nil, fmt.Errorf("invalid latest release version %q", tag)
	}
	release := &Release{TagName: tag}
	for _, name := range []string{
		"volcano-linux-amd64", "volcano-linux-arm64", "volcano-macos-amd64",
		"volcano-macos-arm64", "volcano-windows-amd64.exe", "SHA256SUMS",
	} {
		release.Assets = append(release.Assets, Asset{Name: name, BrowserDownloadURL: base + "/download/" + tag + "/" + name})
		if name != "SHA256SUMS" {
			release.Assets = append(release.Assets, Asset{Name: name + ".sigstore.json", BrowserDownloadURL: base + "/download/" + tag + "/" + name + ".sigstore.json"})
		}
	}
	return release, nil
}
