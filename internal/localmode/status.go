package localmode

import (
	"context"
	"fmt"
	"io"

	"github.com/Kong/volcano-cli/internal/output"
)

func (s Service) printStatusDetails(ctx context.Context, w io.Writer) error {
	info, err := s.fetchInfo(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch local metadata: %w", err)
	}
	s.printStatusDetailsWithInfo(ctx, w, info)
	return nil
}

func (s Service) printStatusDetailsWithInfo(ctx context.Context, w io.Writer, info Info) {
	output.LocalModeStatus(w, output.LocalModeStatusDetails{
		Services: []output.LocalModeServiceStatus{
			{Name: "PostgreSQL", Running: s.dialTCP(ctx, postgresAddress) == nil},
			{Name: "Redis", Running: s.checkRedis(ctx) == nil},
			{Name: "API Server", Running: s.checkHealth(ctx) == nil},
			{Name: "Server", Running: s.serverRunning(ctx)},
		},
		ProjectID:   info.ProjectID,
		UserID:      info.UserID,
		APIURL:      info.APIURL,
		AnonKey:     info.AnonKey,
		ServiceKey:  info.ServiceKey,
		DatabaseURL: info.DatabaseURL,
		PSQLCommand: info.PSQLCommandHint(),
	})
}

func (s Service) checkRedis(ctx context.Context) error {
	_, err := s.runDocker(ctx, "exec", redisContainerName, "redis-cli", "ping")
	return err
}
