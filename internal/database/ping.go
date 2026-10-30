package database

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/Kong/volcano-cli/internal/api"
)

// Ping runs the platform's connection test, a SELECT 1 through the database
// API, against a database or, when branch is set, one of its branches. It
// returns how long the round trip took.
func (s Service) Ping(ctx context.Context, name, branch string) (time.Duration, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return 0, err
	}
	client := authenticated.API
	if s.pingToken != nil {
		token, err := s.pingToken(ctx, authenticated)
		if err != nil {
			return 0, err
		}
		if client, err = authenticated.APIWithToken(token); err != nil {
			return 0, fmt.Errorf("failed to create api client: %w", err)
		}
	}

	started := time.Now()
	if err := client.PingDatabase(ctx, name, branch); err != nil {
		return 0, pingError(name, branch, err)
	}
	return time.Since(started), nil
}

func pingError(name, branch string, err error) error {
	target := fmt.Sprintf("database %q", name)
	if branch != "" {
		target = fmt.Sprintf("branch %q of database %q", branch, name)
	}
	if api.Status(err) == http.StatusNotFound {
		return api.WithCause(fmt.Errorf("%s not found", target), err)
	}
	return fmt.Errorf("could not connect to %s: %w", target, err)
}
