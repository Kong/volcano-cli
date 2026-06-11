package localmode

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Kong/volcano-cli/internal/output"
)

// Reset resets local-mode data in place through the running server container.
func (s Service) Reset(ctx context.Context, w io.Writer) error {
	if err := s.checkDocker(ctx); err != nil {
		output.LocalModeDockerUnavailable(w)
		return fmt.Errorf("docker not found: %w", err)
	}

	if !s.serverRunning(ctx) {
		return errors.New("volcano is not running; run 'volcano start' first")
	}

	resetOutput, err := s.runDocker(ctx, localResetCommandArgs()...)
	if err != nil {
		trimmedOutput := strings.TrimSpace(string(resetOutput))
		if trimmedOutput == "" {
			return fmt.Errorf("failed to reset local Volcano: %w", err)
		}
		return fmt.Errorf("failed to reset local Volcano: %w\n%s", err, trimmedOutput)
	}

	if trimmed := strings.TrimSpace(string(resetOutput)); trimmed != "" {
		fmt.Fprintln(w, trimmed)
	} else {
		fmt.Fprintln(w, "Local reset complete.")
	}

	info, err := s.fetchInfo(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch local metadata after reset: %w", err)
	}
	if err := saveDevState(info); err != nil {
		return fmt.Errorf("failed to save dev state after reset: %w", err)
	}

	fmt.Fprintln(w, "Redeploy migrations with: volcano migrations deploy --all -d app")
	return nil
}

func localResetCommandArgs() []string {
	return []string{"exec", serverContainerName, serverBinaryPath, "local", "reset", "--yes", "--format", "text"}
}
