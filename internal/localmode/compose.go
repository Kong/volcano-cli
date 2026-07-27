package localmode

import (
	"context"
	_ "embed"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/Kong/volcano-cli/internal/output"
)

//go:embed assets/docker-compose.template.yml
var dockerComposeTemplate []byte

//go:embed assets/docker-compose.persistence.yml
var dockerComposePersistence []byte

func (s Service) checkDocker(ctx context.Context) error {
	_, err := s.runDocker(ctx, "version")
	return err
}

func (s Service) serverRunning(ctx context.Context) bool {
	inspectOutput, err := s.runDocker(ctx, "inspect", "--format={{.State.Running}}", serverContainerName)
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(inspectOutput)) == "true"
}

func (s Service) composeProjectHasContainers(ctx context.Context) bool {
	containers, err := s.runDocker(
		ctx,
		"ps",
		"-a",
		"--quiet",
		"--filter",
		"label=com.docker.compose.project="+composeProjectName,
	)
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(containers)) != ""
}

func (s Service) composeEnvironment() ([]string, string, error) {
	env := append([]string{}, s.environ()...)

	overrides, err := localEnvOverrides()
	if err != nil {
		return nil, "", err
	}
	env = append(env, overrides...)
	env = dropIncompleteFirstPartyBootstrap(env)

	image, _ := s.resolveImage()
	env = withoutEnvKey(env, "VOLCANO_IMAGE")
	env = append(env, "VOLCANO_IMAGE="+image)

	return env, image, nil
}

// firstPartyBootstrapKeys is the complete set the local server needs to run
// first-party bootstrap. The server treats it as all-or-nothing: if any subset
// is present it attempts bootstrap and hard-fails (never becoming ready) unless
// every var is set.
var firstPartyBootstrapKeys = []string{
	"VOLCANO_FIRST_PARTY_USER_ID",
	"VOLCANO_FIRST_PARTY_USER_DISPLAY_NAME",
	"VOLCANO_FIRST_PARTY_USER_TOKEN",
	"VOLCANO_FIRST_PARTY_PROJECT_ID",
	"VOLCANO_FIRST_PARTY_PROJECT_NAME",
	"VOLCANO_FIRST_PARTY_ANON_KEY",
	"VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID",
}

// dropIncompleteFirstPartyBootstrap removes the first-party bootstrap vars (and
// the paired ANON_KEY_SECRET) from env unless the whole set is present and
// non-empty. `volcano start` forwards the process env and .env.local to the
// local server; a partial set is the common case — a developer keeps
// VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID / _ANON_KEY in .env.local for `volcano
// login`/`signup`, which `volcano start` also forwards — and it makes the
// server's bootstrap fail so the stack never becomes ready. Local development
// doesn't need first-party bootstrap: the server auto-provisions its pre-baked
// local user when these are absent. So we strip a partial set and keep it only
// when a caller deliberately provides every var. The CLI's own auth flows are
// unaffected — they read these from the CLI process env, not the server's.
func dropIncompleteFirstPartyBootstrap(env []string) []string {
	for _, key := range firstPartyBootstrapKeys {
		if value, ok := lastEnvValue(env, key); !ok || strings.TrimSpace(value) == "" {
			for _, k := range firstPartyBootstrapKeys {
				env = withoutEnvKey(env, k)
			}
			return withoutEnvKey(env, "ANON_KEY_SECRET")
		}
	}
	return env
}

// resolveImage returns the local-mode server image to run and whether it is a
// custom image (i.e. differs from the bundled default). Precedence (highest
// first): explicit image (WithImage/--image) > VOLCANO_IMAGE process env >
// project .env.local > defaultVolcanoImage. A custom image is treated as
// local-only: it is never pulled and must already exist locally. The bundled
// default is refreshed on start by a best-effort pull (see
// refreshDefaultServerImage), even when it is selected explicitly.
func (s Service) resolveImage() (string, bool) {
	image := defaultVolcanoImage
	switch {
	case s.image != "":
		image = s.image
	case strings.TrimSpace(s.getenv("VOLCANO_IMAGE")) != "":
		image = strings.TrimSpace(s.getenv("VOLCANO_IMAGE"))
	default:
		if overrides, err := localEnvOverrides(); err == nil {
			if fileImage, ok := envValue(overrides, "VOLCANO_IMAGE"); ok && strings.TrimSpace(fileImage) != "" {
				image = strings.TrimSpace(fileImage)
			}
		}
	}
	return image, image != defaultVolcanoImage
}

// imageExistsLocally reports whether a Docker image reference is present in the
// local image store. It never contacts a registry.
func (s Service) imageExistsLocally(ctx context.Context, ref string) bool {
	_, err := s.runDocker(ctx, "image", "inspect", ref)
	return err == nil
}

// ensureCustomImageAvailable fails fast when an explicitly selected (custom)
// local-mode image is not present locally. The CLI never pulls unpublished
// local-mode images, so this surfaces an actionable build message instead of a
// confusing registry-pull error. The bundled default is refreshed on start by a
// best-effort pull (see refreshDefaultServerImage) even when selected
// explicitly.
func (s Service) ensureCustomImageAvailable(ctx context.Context) error {
	image, customImage := s.resolveImage()
	if customImage && !s.imageExistsLocally(ctx, image) {
		return fmt.Errorf("image %q not found locally; the CLI does not pull unpublished local-mode images. Build it (e.g. in volcano-hosting: make docker-build DOCKER_TAG=<tag>) and ensure the tag matches, or run `docker pull %s` first if it is published", image, image)
	}
	return nil
}

func (s Service) startDockerServices(ctx context.Context, w io.Writer, env []string) error {
	composePaths, cleanup, err := s.writeComposeFiles()
	if err != nil {
		return err
	}
	defer cleanup()

	s.refreshDefaultServerImage(ctx, w, composePaths, env)

	args := composeFileArgs(composePaths)
	args = append(args, "-p", composeProjectName, "up", "-d", "--force-recreate")
	_, err = s.runner.Run(ctx, Command{
		Name: dockerCommand,
		Args: args,
		Env:  env,
	})
	return err
}

// refreshDefaultServerImage pulls the rolling default local-mode image so
// `volcano start` picks up the latest published build instead of a stale
// cached copy (the default tag is a moving target, and Compose only pulls it
// when absent). It is skipped for an explicitly selected custom image, which
// the CLI never pulls and which must already exist locally. Best-effort: a
// pull failure (e.g. offline) is a warning and `up` falls back to the cached
// image.
func (s Service) refreshDefaultServerImage(ctx context.Context, w io.Writer, composePaths, env []string) {
	image, customImage := s.resolveImage()
	if customImage {
		return
	}
	fmt.Fprintf(w, "Pulling latest local-mode image: %s\n", image)
	args := composeFileArgs(composePaths)
	args = append(args, "-p", composeProjectName, "pull", serverComposeService)
	if _, err := s.runner.Run(ctx, Command{Name: dockerCommand, Args: args, Env: env}); err != nil {
		// Best-effort: `up` still runs and uses a cached image if one exists;
		// a first start with no cached image will fail there with a clearer error.
		output.Warning(w, "could not pull latest local-mode image; using a cached copy if present: %v", err)
	}
}

func (s Service) writeComposeFiles() ([]string, func(), error) {
	paths := make([]string, 0, 2)
	cleanup := func() {
		for _, path := range paths {
			_ = os.Remove(path)
		}
	}

	for _, contents := range [][]byte{dockerComposeTemplate, dockerComposePersistence} {
		path, err := s.writeComposeFile(contents)
		if err != nil {
			cleanup()
			return nil, nil, err
		}
		paths = append(paths, path)
	}

	return paths, cleanup, nil
}

func (s Service) writeComposeFile(contents []byte) (string, error) {
	tmpFile, err := os.CreateTemp(s.tempDir, "docker-compose-*.yml")
	if err != nil {
		return "", fmt.Errorf("failed to create temp compose file: %w", err)
	}
	composePath := tmpFile.Name()

	if _, err := tmpFile.Write(contents); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(composePath)
		return "", fmt.Errorf("failed to write compose file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(composePath)
		return "", fmt.Errorf("failed to close compose file: %w", err)
	}
	return composePath, nil
}

func composeFileArgs(paths []string) []string {
	args := []string{"compose"}
	for _, path := range paths {
		args = append(args, "-f", path)
	}
	return args
}

func (s Service) composeDown(ctx context.Context, clean bool) error {
	composePaths, cleanup, err := s.writeComposeFiles()
	if err != nil {
		return err
	}
	defer cleanup()

	args := composeFileArgs(composePaths)
	args = append(args, "-p", composeProjectName, "down")
	if clean {
		args = append(args, "-v")
	}
	_, err = s.runDocker(ctx, args...)
	return err
}

func (s Service) printContainerLogs(ctx context.Context, w io.Writer, containerName string, tailLines int) {
	fmt.Fprintf(w, "Startup failed, printing docker logs for %s\n", containerName)

	args := []string{"logs"}
	if tailLines > 0 {
		args = append(args, "--tail", strconv.Itoa(tailLines))
	}
	args = append(args, containerName)

	logs, err := s.runDocker(ctx, args...)
	if err != nil {
		output.Warning(w, "failed to get logs for %s: %v", containerName, err)
		return
	}
	if len(logs) > 0 {
		fmt.Fprint(w, string(logs))
		if !strings.HasSuffix(string(logs), "\n") {
			fmt.Fprintln(w)
		}
	}
}

func (s Service) runDocker(ctx context.Context, args ...string) ([]byte, error) {
	return s.runner.Run(ctx, Command{Name: dockerCommand, Args: args})
}

func envValue(env []string, key string) (string, bool) {
	prefix := key + "="
	for _, entry := range env {
		if value, ok := strings.CutPrefix(entry, prefix); ok {
			return value, true
		}
	}
	return "", false
}

// lastEnvValue returns the value of the last entry for key, matching how a child
// process resolves duplicate env entries (later wins). composeEnvironment
// appends .env.local after the process env, so the last occurrence is the value
// the server would actually see.
func lastEnvValue(env []string, key string) (string, bool) {
	prefix := key + "="
	value, ok := "", false
	for _, entry := range env {
		if v, found := strings.CutPrefix(entry, prefix); found {
			value, ok = v, true
		}
	}
	return value, ok
}

func withoutEnvKey(env []string, key string) []string {
	prefix := key + "="
	filtered := env[:0]
	for _, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}
