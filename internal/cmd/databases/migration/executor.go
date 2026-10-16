// Package migration runs SQL migrations against a Volcano database connection.
package migration

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Kong/volcano-cli/internal/sensitive"
)

const migrationExecutionTimeout = 5 * time.Minute

func executeSQLMigration(parent context.Context, connectionString, sql string) error {
	ctx, cancel := context.WithTimeout(parent, migrationExecutionTimeout)
	defer cancel()

	conn, err := pgx.Connect(ctx, connectionString)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", sensitive.Wrap(err))
	}
	defer conn.Close(context.Background()) //nolint:contextcheck // fresh ctx so cleanup runs even if migration ctx was cancelled

	// PostgreSQL errors can quote row values and SQL text, so they are marked
	// sensitive: shown in the terminal, never sent in usage telemetry.
	if err := conn.PgConn().Exec(ctx, sql).Close(); err != nil {
		return fmt.Errorf("failed to execute migration SQL: %w", sensitive.Wrap(err))
	}

	return nil
}
