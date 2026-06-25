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

	image, _ := s.resolveImage()
	env = withoutEnvKey(env, "VOLCANO_IMAGE")
	env = append(env, "VOLCANO_IMAGE="+image)

	return env, image, nil
}

// resolveImage returns the local-mode server image to run and whether it is a
// custom image (i.e. differs from the bundled default). Precedence (highest
// first): explicit image (WithImage/--image) > VOLCANO_IMAGE process env >
// project .env.local > defaultVolcanoImage. A custom image is treated as
// local-only: it is never pulled and must already exist locally. The bundled
// default is left to Docker Compose's normal pull-if-missing behavior even when
// it is selected explicitly.
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

func (s Service) startDockerServices(ctx context.Context, env []string) error {
	composePath, cleanup, err := s.writeComposeFile()
	if err != nil {
		return err
	}
	defer cleanup()

	_, err = s.runner.Run(ctx, Command{
		Name: dockerCommand,
		Args: []string{"compose", "-f", composePath, "-p", composeProjectName, "up", "-d", "--force-recreate"},
		Env:  env,
	})
	return err
}

func (s Service) writeComposeFile() (string, func(), error) {
	tmpFile, err := os.CreateTemp(s.tempDir, "docker-compose-*.yml")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temp compose file: %w", err)
	}
	composePath := tmpFile.Name()
	cleanup := func() { _ = os.Remove(composePath) }

	if _, err := tmpFile.Write(dockerComposeTemplate); err != nil {
		_ = tmpFile.Close()
		cleanup()
		return "", nil, fmt.Errorf("failed to write compose file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("failed to close compose file: %w", err)
	}
	return composePath, cleanup, nil
}

func (s Service) composeDown(ctx context.Context, clean bool) error {
	composePath, cleanup, err := s.writeComposeFile()
	if err != nil {
		return err
	}
	defer cleanup()

	args := []string{"compose", "-f", composePath, "-p", composeProjectName, "down"}
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
