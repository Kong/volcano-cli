package database

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Kong/volcano-cli/internal/api"
	clisession "github.com/Kong/volcano-cli/internal/session"
)

// ErrNoServiceKey indicates the project has no service key for Ping to send.
var ErrNoServiceKey = errors.New("the project has no service key, which the database API needs to run a connection test")

// Ping runs the platform's connection test, a SELECT 1 through the database
// API, against a database or, when branch is set, one of its branches. It
// returns how long the round trip took.
func (s Service) Ping(ctx context.Context, name, branch string) (time.Duration, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return 0, err
	}
	client := authenticated.API
	if !s.pingWithSessionToken {
		if client, err = serviceKeyClient(ctx, authenticated); err != nil {
			return 0, err
		}
	}

	started := time.Now()
	if err := client.PingDatabase(ctx, name, branch); err != nil {
		return 0, pingError(name, branch, err)
	}
	return time.Since(started), nil
}

// serviceKeyClient returns a client that authenticates with one of the
// project's service keys, as the dashboard's connection test does. The
// database API accepts no platform token, and every service key may query it,
// whatever its permissions. The key never leaves the client.
func serviceKeyClient(ctx context.Context, authenticated *clisession.ProjectSession) (*api.Client, error) {
	keys, err := authenticated.API.ListServiceKeys(ctx, authenticated.ProjectID, 1, 1)
	if err != nil {
		return nil, fmt.Errorf("failed to read the project's service keys: %w", err)
	}
	if len(keys.Data) == 0 || keys.Data[0].KeyValue == nil || strings.TrimSpace(*keys.Data[0].KeyValue) == "" {
		return nil, ErrNoServiceKey
	}
	client, err := authenticated.APIWithToken(*keys.Data[0].KeyValue)
	if err != nil {
		return nil, fmt.Errorf("failed to create api client: %w", err)
	}
	return client, nil
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
