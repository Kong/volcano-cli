// Package update checks for and installs Volcano CLI releases.
package update

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	defaultGitHubAPIURL = "https://api.github.com/repos/Kong/volcano-cli"
	defaultTimeout      = 10 * time.Second
	signatureWorkflow   = "https://github.com/Kong/volcano-cli/.github/workflows/publish-cli.yml"
	signatureOIDCIssuer = "https://token.actions.githubusercontent.com"
)

// HTTPClient is the subset of http.Client used by release checks and upgrades.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// CommandRunner runs an external verification command.
type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// RunnerFunc adapts a function to CommandRunner.
type RunnerFunc func(ctx context.Context, name string, args ...string) ([]byte, error)

// Run calls f(ctx, name, args...).
func (f RunnerFunc) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return f(ctx, name, args...)
}

// Options configures update operations.
type Options struct {
	HTTPClient                   HTTPClient
	GitHubAPIURL                 string
	ExecutablePath               string
	CommandRunner                CommandRunner
	RequireSignatureVerification bool
	// InstallMethod overrides auto-detection of how the CLI was installed.
	// Empty means detect from the marker file / executable path.
	InstallMethod InstallMethod
	// ManagerRunner runs a package-manager upgrade command (npm/brew/…),
	// streaming output to the writer passed to Upgrade. Nil uses os/exec with
	// inherited stdio.
	ManagerRunner func(ctx context.Context, out io.Writer, name string, args ...string) error
	// LookPath resolves a command name to a path. Nil uses exec.LookPath.
	LookPath func(name string) (string, error)
}

// Release is the subset of GitHub release metadata the CLI needs.
type Release struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

// Asset is a downloadable GitHub release asset.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// LatestRelease fetches GitHub's latest release metadata.
func LatestRelease(ctx context.Context, opts Options) (*Release, error) {
	client := releaseHTTPClient(opts)
	baseURL := strings.TrimRight(strings.TrimSpace(opts.GitHubAPIURL), "/")
	if baseURL == "" {
		baseURL = defaultGitHubAPIURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/releases/latest", http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create release request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "volcano-cli")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch latest release: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("failed to fetch latest release: github returned %s", resp.Status)
	}
	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("failed to decode latest release: %w", err)
	}
	return &release, nil
}

func releaseHTTPClient(opts Options) HTTPClient {
	if opts.HTTPClient != nil {
		return opts.HTTPClient
	}
	return &http.Client{Timeout: defaultTimeout}
}

func assetDownloadHTTPClient(opts Options) HTTPClient {
	if opts.HTTPClient != nil {
		return opts.HTTPClient
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = defaultTimeout
	return &http.Client{Transport: transport}
}

// Upgrade upgrades the CLI. It delegates to the package manager the CLI was
// installed with (npm, brew, …); for script/manual installs it downloads the
// latest release and replaces the running binary in place.
func Upgrade(ctx context.Context, current string, out io.Writer, opts Options) error {
	exePath, err := resolveExecutablePath(opts)
	if err != nil {
		return err
	}
	method := opts.InstallMethod
	if method == InstallUnknown {
		method = DetectInstallMethod(exePath)
	}
	if name, args, managed := UpgradeCommandFor(method); managed {
		return upgradeViaManager(ctx, current, out, opts, method, name, args)
	}
	if goruntime.GOOS == "windows" && opts.ExecutablePath == "" {
		return errors.New("self-upgrade is not supported on Windows; download the latest installer from GitHub releases")
	}
	return upgradeViaDownload(ctx, current, out, opts, exePath)
}

func resolveExecutablePath(opts Options) (string, error) {
	exePath := opts.ExecutablePath
	if exePath == "" {
		var err error
		exePath, err = os.Executable()
		if err != nil {
			return "", fmt.Errorf("failed to resolve running executable: %w", err)
		}
	}
	resolved, err := filepath.EvalSymlinks(exePath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve executable path: %w", err)
	}
	return resolved, nil
}

// upgradeViaManager upgrades a package-manager install by running its upgrade
// command. If the manager is not on PATH it prints the command instead so the
// user can run it themselves.
//
// For managers whose @latest package is identified exactly by the release
// version (npm/pnpm/yarn/bun), it skips the reinstall when already on the latest
// release, mirroring the self-download path — otherwise the manager is invoked
// on every `volcano upgrade` even when nothing is newer. brew is excluded (see
// releaseVersionIsPackageVersion) so its same-version revision rebuilds still
// apply. The manager-availability check runs before the release lookup so a
// missing manager returns immediately instead of blocking on a network
// round-trip. The up-to-date check is best effort: on a network error or a
// dev/unparseable current version it returns false and the upgrade proceeds.
func upgradeViaManager(ctx context.Context, current string, out io.Writer, opts Options, method InstallMethod, name string, args []string) error {
	lookPath := opts.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	full := strings.Join(append([]string{name}, args...), " ")
	if _, err := lookPath(name); err != nil {
		// Manager not on PATH: printing the command is the useful outcome, not an error.
		fmt.Fprintf(out, "Volcano CLI was installed with %s. Upgrade it with:\n  %s\n", method, full)
		return nil //nolint:nilerr // lookPath miss is expected; we guide the user instead of failing
	}
	if releaseVersionIsPackageVersion(method) && upToDate(ctx, current, opts) {
		fmt.Fprintf(out, "Volcano CLI is already up to date (%s).\n", current)
		return nil
	}
	fmt.Fprintf(out, "Volcano CLI was installed with %s; upgrading with `%s`...\n", method, full)
	run := opts.ManagerRunner
	if run == nil {
		run = defaultManagerRunner
	}
	if err := run(ctx, out, name, args...); err != nil {
		return fmt.Errorf("failed to upgrade with %s: %w", method, err)
	}
	return nil
}

// releaseVersionIsPackageVersion reports whether the manager's @latest package
// is identified exactly by the GitHub release version, making a GitHub-semver
// up-to-date check authoritative. Homebrew is excluded: it can ship formula
// revision/bottle rebuilds (1.2.3 -> 1.2.3_1) at the same upstream tag, and
// `brew upgrade` picks those up cheaply, so the brew path always runs.
func releaseVersionIsPackageVersion(m InstallMethod) bool {
	switch m {
	case InstallNPM, InstallPNPM, InstallYarn, InstallBun:
		return true
	default:
		return false
	}
}

// upToDate reports whether current is the latest released version. It returns
// false on any uncertainty (unparseable current version, network error) so the
// caller upgrades rather than skipping.
func upToDate(ctx context.Context, current string, opts Options) bool {
	if _, err := parseStableVersion(current); err != nil {
		return false
	}
	release, err := LatestRelease(ctx, opts)
	if err != nil {
		return false
	}
	newer, err := NewerThan(release.TagName, current)
	if err != nil {
		return false
	}
	return !newer
}

func defaultManagerRunner(ctx context.Context, out io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // command from the fixed install-method mapping
	cmd.Stdin = os.Stdin
	cmd.Stdout = out
	cmd.Stderr = out
	return cmd.Run()
}

// upgradeViaDownload downloads the latest release and replaces the running
// binary at exePath (already resolved through symlinks).
func upgradeViaDownload(ctx context.Context, current string, out io.Writer, opts Options, exePath string) error {
	release, err := LatestRelease(ctx, opts)
	if err != nil {
		return err
	}
	newer := true
	if _, err := parseStableVersion(current); err == nil {
		newer, err = NewerThan(release.TagName, current)
		if err != nil {
			return err
		}
	}
	if !newer {
		fmt.Fprintf(out, "Volcano CLI is already up to date (%s).\n", current)
		return nil
	}

	binaryName, err := PlatformBinaryName()
	if err != nil {
		return err
	}
	binaryAsset, err := release.Asset(binaryName)
	if err != nil {
		return err
	}
	var bundleAsset Asset
	if opts.requireSignatureVerification() {
		bundleAsset, err = release.Asset(binaryName + ".sigstore.json")
		if err != nil {
			return err
		}
	}
	checksumsAsset, err := release.Asset("SHA256SUMS")
	if err != nil {
		return err
	}

	// Stage the download under the system temp dir so users without write access
	// to filepath.Dir(exePath) (e.g. /usr/local/bin) can still download and verify.
	// The final install copies the verified binary into a sibling of exePath
	// before renaming, so the atomic-swap guarantee is preserved.
	tmpDir, err := os.MkdirTemp("", "volcano-upgrade-")
	if err != nil {
		return fmt.Errorf("failed to create temporary upgrade directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	tmpBinary := filepath.Join(tmpDir, binaryName)
	tmpBundle := tmpBinary + ".sigstore.json"
	tmpChecksums := filepath.Join(tmpDir, "SHA256SUMS")

	fmt.Fprintf(out, "Downloading Volcano CLI %s...\n", release.TagName)
	if err := downloadFile(ctx, opts, binaryAsset.BrowserDownloadURL, tmpBinary); err != nil {
		return err
	}
	if err := downloadFile(ctx, opts, checksumsAsset.BrowserDownloadURL, tmpChecksums); err != nil {
		return err
	}
	if opts.requireSignatureVerification() {
		if err := downloadFile(ctx, opts, bundleAsset.BrowserDownloadURL, tmpBundle); err != nil {
			return err
		}
		if err := verifySignature(ctx, opts, tmpBinary, tmpBundle, release.TagName); err != nil {
			return err
		}
	}
	if err := verifyChecksum(tmpBinary, tmpChecksums, binaryName); err != nil {
		return err
	}
	if err := os.Chmod(tmpBinary, 0o755); err != nil { //nolint:gosec // downloaded CLI must be executable
		return fmt.Errorf("failed to make downloaded binary executable: %w", err)
	}
	if err := installBinary(tmpBinary, exePath); err != nil {
		return err
	}
	fmt.Fprintf(out, "Upgraded Volcano CLI from %s to %s.\n", current, release.TagName)
	return nil
}

// installBinary atomically replaces exePath with src. It first tries a direct rename
// (fast path when src and exePath share a filesystem). If rename fails with EXDEV —
// typically because src is on /tmp (tmpfs) and exePath is on a different filesystem —
// it copies src into a sibling of exePath and renames that into place instead.
func installBinary(src, exePath string) error {
	if err := os.Rename(src, exePath); err == nil {
		return nil
	} else if !errors.Is(err, syscall.EXDEV) {
		return fmt.Errorf("failed to replace %s: %w", exePath, err)
	}

	stagingDir := filepath.Dir(exePath)
	staged, err := os.CreateTemp(stagingDir, ".volcano-upgrade-*")
	if err != nil {
		return fmt.Errorf("failed to stage upgrade in %s: %w", stagingDir, err)
	}
	stagedPath := staged.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(stagedPath)
		}
	}()

	if err := copyToFile(staged, src); err != nil {
		_ = staged.Close()
		return err
	}
	if err := staged.Close(); err != nil {
		return fmt.Errorf("failed to close staged binary: %w", err)
	}
	if err := os.Chmod(stagedPath, 0o755); err != nil { //nolint:gosec // downloaded CLI must be executable
		return fmt.Errorf("failed to make staged binary executable: %w", err)
	}
	if err := os.Rename(stagedPath, exePath); err != nil {
		return fmt.Errorf("failed to replace %s: %w", exePath, err)
	}
	cleanup = false
	return nil
}

func copyToFile(dst *os.File, srcPath string) error {
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("failed to open staged binary: %w", err)
	}
	defer func() { _ = srcFile.Close() }()
	if _, err := io.Copy(dst, srcFile); err != nil {
		return fmt.Errorf("failed to copy staged binary: %w", err)
	}
	return nil
}

// Asset returns the release asset with name.
func (r Release) Asset(name string) (Asset, error) {
	for _, asset := range r.Assets {
		if asset.Name == name {
			return asset, nil
		}
	}
	return Asset{}, fmt.Errorf("latest release does not include required asset %q", name)
}

// PlatformBinaryName returns the published release asset name for this process.
func PlatformBinaryName() (string, error) {
	osName := goruntime.GOOS
	if osName == "darwin" {
		osName = "macos"
	}
	arch := goruntime.GOARCH
	if arch != "amd64" && arch != "arm64" {
		return "", fmt.Errorf("unsupported architecture: %s", goruntime.GOARCH)
	}
	ext := ""
	if osName == "windows" {
		ext = ".exe"
	}
	target := osName + "-" + arch
	switch target {
	case "linux-amd64", "linux-arm64", "macos-amd64", "macos-arm64", "windows-amd64":
		return "volcano-" + target + ext, nil
	case "windows-arm64":
		return "", errors.New("unsupported platform: windows-arm64; Volcano CLI does not publish a Windows arm64 binary yet")
	default:
		return "", fmt.Errorf("unsupported platform: %s", target)
	}
}

// NewerThan reports whether candidate is newer than current.
func NewerThan(candidate, current string) (bool, error) {
	c, err := parseStableVersion(candidate)
	if err != nil {
		return false, fmt.Errorf("invalid latest version %q: %w", candidate, err)
	}
	cur, err := parseStableVersion(current)
	if err != nil {
		return false, fmt.Errorf("invalid current version %q: %w", current, err)
	}
	return c.compare(cur) > 0, nil
}

type stableVersion struct {
	major int
	minor int
	patch int
}

func parseStableVersion(raw string) (stableVersion, error) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(raw), "v")
	parts := strings.Split(trimmed, ".")
	if len(parts) != 3 {
		return stableVersion{}, errors.New("expected vMAJOR.MINOR.PATCH")
	}
	parsed := make([]int, 3)
	for i, part := range parts {
		if part == "" || strings.HasPrefix(part, "+") || strings.HasPrefix(part, "-") {
			return stableVersion{}, errors.New("expected non-negative numeric version components")
		}
		value, err := strconv.Atoi(part)
		if err != nil {
			return stableVersion{}, errors.New("expected non-negative numeric version components")
		}
		// Round-trip through strconv.Itoa to reject leading zeroes ("01" parses as 1 but
		// would re-format to "1"), preserving strict semver behavior.
		if strconv.Itoa(value) != part {
			return stableVersion{}, errors.New("version components must not contain leading zeroes")
		}
		parsed[i] = value
	}
	return stableVersion{major: parsed[0], minor: parsed[1], patch: parsed[2]}, nil
}

func (v stableVersion) compare(other stableVersion) int {
	left := []int{v.major, v.minor, v.patch}
	right := []int{other.major, other.minor, other.patch}
	for i := range left {
		if left[i] > right[i] {
			return 1
		}
		if left[i] < right[i] {
			return -1
		}
	}
	return 0
}

func downloadFile(ctx context.Context, opts Options, rawURL, target string) error {
	client := assetDownloadHTTPClient(opts)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return fmt.Errorf("failed to create download request: %w", err)
	}
	req.Header.Set("User-Agent", "volcano-cli")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download %s: %w", rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("failed to download %s: server returned %s", rawURL, resp.Status)
	}
	f, err := os.Create(target)
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", target, err)
	}
	defer func() { _ = f.Close() }()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("failed to write %s: %w", target, err)
	}
	return nil
}

func verifySignature(ctx context.Context, opts Options, file, bundle, version string) error {
	runner := opts.CommandRunner
	if runner == nil {
		runner = RunnerFunc(func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).CombinedOutput() //nolint:gosec // constant command from caller
		})
	}
	args := []string{
		"verify-blob", file,
		"--bundle", bundle,
		"--certificate-identity", signatureWorkflow + "@refs/tags/" + version,
		"--certificate-oidc-issuer", signatureOIDCIssuer,
	}
	output, err := runner.Run(ctx, "cosign", args...)
	if err != nil {
		if len(output) > 0 {
			return fmt.Errorf("failed to verify Volcano CLI signature with cosign: %w: %s", err, strings.TrimSpace(string(output)))
		}
		return fmt.Errorf("failed to verify Volcano CLI signature with cosign: %w", err)
	}
	return nil
}

func verifyChecksum(binaryPath, checksumsPath, binaryName string) error {
	expected, err := checksumFor(checksumsPath, binaryName)
	if err != nil {
		return err
	}
	f, err := os.Open(binaryPath)
	if err != nil {
		return fmt.Errorf("failed to open downloaded binary for checksum verification: %w", err)
	}
	defer func() { _ = f.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return fmt.Errorf("failed to hash downloaded binary: %w", err)
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("checksum mismatch for %s", binaryName)
	}
	return nil
}

func checksumFor(checksumsPath, binaryName string) (string, error) {
	f, err := os.Open(checksumsPath)
	if err != nil {
		return "", fmt.Errorf("failed to open SHA256SUMS: %w", err)
	}
	defer func() { _ = f.Close() }()

	var names []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		names = append(names, name)
		if name == binaryName {
			return fields[0], nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("failed to read SHA256SUMS: %w", err)
	}
	sort.Strings(names)
	return "", fmt.Errorf("SHA256SUMS does not include %s; found: %s", binaryName, strings.Join(names, ", "))
}

func (opts Options) requireSignatureVerification() bool {
	return opts.RequireSignatureVerification || os.Getenv("VOLCANO_REQUIRE_SIGNATURE_VERIFICATION") == "1"
}
