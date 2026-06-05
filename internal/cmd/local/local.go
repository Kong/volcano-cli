// Package local wires commands that target the local development server.
package local

import (
	"context"
	"sync"
	"time"

	"github.com/spf13/cobra"

	configcmd "github.com/Kong/volcano-cli/internal/cmd/config"
	databasescmd "github.com/Kong/volcano-cli/internal/cmd/databases"
	migrationcmd "github.com/Kong/volcano-cli/internal/cmd/databases/migration"
	functionscmd "github.com/Kong/volcano-cli/internal/cmd/functions"
	storagecmd "github.com/Kong/volcano-cli/internal/cmd/storage"
	variablescmd "github.com/Kong/volcano-cli/internal/cmd/variables"
	cliconfig "github.com/Kong/volcano-cli/internal/config"
	"github.com/Kong/volcano-cli/internal/localmode"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const localInfoTimeout = 30 * time.Second

type infoCache struct {
	runner localmode.CommandRunner
	once   sync.Once
	info   localmode.Info
	err    error
}

// New returns the local command tree.
func New(deps cliruntime.Deps) *cobra.Command {
	cache := &infoCache{runner: deps.LocalCommandRunner}
	localDeps := withLocalConfig(deps, cache)

	cmd := &cobra.Command{
		Use:   "local",
		Short: "Manage local development resources",
		Long:  "Manage resources in the local development project backed by the running Volcano server container.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(databasescmd.NewLocalWithOptions(localDeps, databasescmd.LocalOptions{
		CreateDefaults: cache.databaseCreateDefaults,
	}))
	cmd.AddCommand(migrationcmd.NewLocal(localDeps))
	cmd.AddCommand(storagecmd.NewWithOptions(
		localDeps,
		storagecmd.WithObjectTokenProvider(cache.storageObjectToken),
	))
	cmd.AddCommand(configcmd.New(localDeps))
	cmd.AddCommand(functionscmd.NewLocal(localDeps))
	cmd.AddCommand(variablescmd.New(localDeps))
	cmd.AddCommand(newReset(deps))
	return cmd
}

func withLocalConfig(deps cliruntime.Deps, cache *infoCache) cliruntime.Deps {
	deps.ConfigLoader = func() (*cliconfig.Config, error) {
		ctx, cancel := context.WithTimeout(context.Background(), localInfoTimeout)
		defer cancel()

		info, err := cache.load(ctx)
		if err != nil {
			return nil, err
		}

		return &cliconfig.Config{
			APIBaseURL: info.APIURL,
			UserToken:  info.UserToken,
			UserID:     info.UserID,
			CurrentProject: &cliconfig.ProjectConfig{
				ID:   info.ProjectID,
				Name: info.ProjectName,
			},
			IgnoreEnv: true,
		}, nil
	}
	return deps
}

func (c *infoCache) storageObjectToken(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, localInfoTimeout)
	defer cancel()

	info, err := c.load(ctx)
	if err != nil {
		return "", err
	}
	return info.ServiceKey, nil
}

func (c *infoCache) databaseCreateDefaults(ctx context.Context) (databasescmd.LocalCreateDefaults, error) {
	ctx, cancel := context.WithTimeout(ctx, localInfoTimeout)
	defer cancel()

	info, err := c.load(ctx)
	if err != nil {
		return databasescmd.LocalCreateDefaults{}, err
	}

	return databasescmd.LocalCreateDefaults{
		Region:          info.DefaultDatabaseRegion,
		PostgresVersion: info.DefaultDatabasePostgresVersion,
	}, nil
}

func (c *infoCache) load(ctx context.Context) (localmode.Info, error) {
	c.once.Do(func() {
		c.info, c.err = localmode.FetchInfo(ctx, c.runner)
	})
	return c.info, c.err
}
