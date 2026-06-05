// Package migration runs SQL migrations against a Volcano database connection.
package migration

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const migrationExecutionTimeout = 5 * time.Minute

func executeSQLMigration(parent context.Context, connectionString, sql string) error {
	ctx, cancel := context.WithTimeout(parent, migrationExecutionTimeout)
	defer cancel()

	conn, err := pgx.Connect(ctx, connectionString)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer conn.Close(context.Background()) //nolint:contextcheck // fresh ctx so cleanup runs even if migration ctx was cancelled

	if err := conn.PgConn().Exec(ctx, sql).Close(); err != nil {
		return fmt.Errorf("failed to execute migration SQL: %w", err)
	}

	return nil
}
