package localmode

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"

	"github.com/Kong/volcano-cli/internal/output"
)

// Doctor runs local-development preflight checks and reports actionable
// remediation. It probes for a Docker-compatible engine and Docker Compose v2
// (capability, not a specific brand) and returns an error when a required
// prerequisite is missing so scripts and CI can gate on the exit code.
func (s Service) Doctor(ctx context.Context, w io.Writer) error {
	fmt.Fprintln(w, "Volcano local development doctor")
	fmt.Fprintln(w)

	// Without the Docker CLI nothing else can be probed, so fail fast.
	clientVersion, err := s.dockerClientVersion(ctx)
	if err != nil {
		renderCheckFail(w, "Docker CLI", dockerInstallGuidance())
		return errors.New("docker CLI not available; install a Docker-compatible engine")
	}
	output.Success(w, "Docker CLI (%s)", clientVersion)

	var failed bool

	if serverVersion, err := s.dockerServerVersion(ctx); err != nil {
		renderCheckFail(w, "Docker engine", dockerEngineGuidance())
		failed = true
	} else {
		output.Success(w, "Docker engine (%s)", serverVersion)
	}

	if composeVersion, err := s.dockerComposeVersion(ctx); err != nil {
		renderCheckFail(w, "Docker Compose v2", composeGuidance())
		failed = true
	} else {
		output.Success(w, "Docker Compose v2 (%s)", composeVersion)
	}

	fmt.Fprintln(w)
	if failed {
		fmt.Fprintln(w, "Some checks failed. Fix the items above, then run 'volcano start'.")
		return errors.New("local development prerequisites are not satisfied")
	}

	if s.serverRunning(ctx) {
		fmt.Fprintln(w, "All checks passed. Volcano local development is running.")
	} else {
		fmt.Fprintln(w, "All checks passed. Run 'volcano start' to launch local development.")
	}
	return nil
}

// dockerClientVersion probes the Docker CLI without contacting the engine, so it
// succeeds even when the daemon is down. A failure means the CLI is absent.
func (s Service) dockerClientVersion(ctx context.Context) (string, error) {
	out, err := s.runDocker(ctx, "version", "--format", "{{.Client.Version}}")
	return strings.TrimSpace(string(out)), err
}

// dockerServerVersion probes the engine/daemon. It fails when the CLI is present
// but the daemon is not reachable (not started, wrong DOCKER_HOST, etc.).
func (s Service) dockerServerVersion(ctx context.Context) (string, error) {
	out, err := s.runDocker(ctx, "version", "--format", "{{.Server.Version}}")
	return strings.TrimSpace(string(out)), err
}

// dockerComposeVersion probes the Docker Compose v2 plugin (the `docker compose`
// subcommand). Legacy standalone `docker-compose` is not accepted.
func (s Service) dockerComposeVersion(ctx context.Context) (string, error) {
	out, err := s.runDocker(ctx, "compose", "version", "--short")
	return strings.TrimSpace(string(out)), err
}

func renderCheckFail(w io.Writer, name, remedy string) {
	fmt.Fprintf(w, "✗ %s\n", name)
	for line := range strings.SplitSeq(remedy, "\n") {
		fmt.Fprintf(w, "    %s\n", line)
	}
}

func dockerInstallGuidance() string {
	var options string
	switch runtime.GOOS {
	case "darwin":
		options = "Install a Docker-compatible engine (Docker Desktop, OrbStack, or Colima)."
	case "linux":
		options = "Install a Docker-compatible engine (Docker Engine, or Podman with the docker CLI)."
	case "windows":
		options = "Install a Docker-compatible engine (Docker Desktop)."
	default:
		options = "Install a Docker-compatible engine that provides the 'docker' CLI and 'docker compose'."
	}
	return options + "\nDocs: https://docs.docker.com/engine/install/"
}

func dockerEngineGuidance() string {
	return "The Docker CLI is installed but the engine isn't reachable.\n" +
		"Start your Docker engine (open Docker Desktop/OrbStack, or run 'colima start' / 'sudo systemctl start docker')."
}

func composeGuidance() string {
	return "Docker Compose v2 is required (the 'docker compose' subcommand).\n" +
		"Upgrade Docker or install the Compose plugin: https://docs.docker.com/compose/install/"
}
