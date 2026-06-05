package databases

import (
	"testing"

	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestDatabaseCreateRequiresRegionAndPostgresVersion(t *testing.T) {
	setDatabaseCommandTestHome(t)

	_, err := executeDatabaseCommand(t, New(cliruntime.Deps{}), "create", "app", "--pg-version", "16")
	require.ErrorContains(t, err, `required flag(s) "region" not set`)

	_, err = executeDatabaseCommand(t, New(cliruntime.Deps{}), "create", "app", "--region", "aws-us-east-1")
	require.ErrorContains(t, err, `required flag(s) "pg-version" not set`)
}
