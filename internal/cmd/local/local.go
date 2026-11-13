// Package local wires commands that target the local development server.
package local

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/spf13/cobra"

	accesstokenscmd "github.com/Kong/volcano-cli/internal/cmd/accesstokens"
	"github.com/Kong/volcano-cli/internal/cmd/cmdutil"
	configcmd "github.com/Kong/volcano-cli/internal/cmd/config"
	databasescmd "github.com/Kong/volcano-cli/internal/cmd/databases"
	migrationcmd "github.com/Kong/volcano-cli/internal/cmd/databases/migration"
	deploycmd "github.com/Kong/volcano-cli/internal/cmd/deploy"
	deploymentscmd "github.com/Kong/volcano-cli/internal/cmd/deployments"
	domainscmd "github.com/Kong/volcano-cli/internal/cmd/domains"
	durablecmd "github.com/Kong/volcano-cli/internal/cmd/durable"
	frontendscmd "github.com/Kong/volcano-cli/internal/cmd/frontends"
	functionscmd "github.com/Kong/volcano-cli/internal/cmd/functions"
	lavaflowcmd "github.com/Kong/volcano-cli/internal/cmd/lavaflow"
	logscmd "github.com/Kong/volcano-cli/internal/cmd/logs"
	notificationscmd "github.com/Kong/volcano-cli/internal/cmd/notifications"
	sandboxescmd "github.com/Kong/volcano-cli/internal/cmd/sandboxes"
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

// NewResourceCommands returns the direct local resource command tree.
func NewResourceCommands(deps cliruntime.Deps) []*cobra.Command {
	cache := &infoCache{runner: deps.LocalCommandRunner}
	localDeps := withLocalConfig(deps, cache)
	usageDeps := localDeps
	// Project usage requires the local user token, not the Sandbox service key.
	usageDeps.LocalMode = false

	return refuseProjectFlag([]*cobra.Command{
		sandboxescmd.NewLocal(withLocalSandboxConfig(deps, cache), usageDeps),
		databasescmd.NewLocalWithOptions(localDeps, databasescmd.LocalOptions{
			CreateDefaults: cache.databaseCreateDefaults,
		}),
		migrationcmd.NewLocal(localDeps),
		storagecmd.New(localDeps),
		configcmd.New(localDeps),
		functionscmd.NewLocal(localDeps),
		durablecmd.NewLocal(localDeps),
		frontendscmd.NewLocal(localDeps),
		logscmd.New(localDeps),
		variablescmd.New(localDeps),
		notificationscmd.New(localDeps),
		deploycmd.NewLocal(localDeps),
		deploymentscmd.New(localDeps),
		accesstokenscmd.NewCloudOnly(),
		domainscmd.NewCloudOnly(),
		lavaflowcmd.NewCloudOnly(),
		newReset(deps),
	})
}

// errLocalProjectFlag is why a local command refuses --project.
var errLocalProjectFlag = errors.New("it selects a cloud project, and local development has only one project. " +
	"Drop --project, or run the command under 'volcano cloud' to use a cloud project")

// refuseProjectFlag gives each local command a hidden --project that fails
// with errLocalProjectFlag, so `volcano cloud --project` used on a local
// command says why instead of reporting an unknown flag. The error keeps
// cobra's invalid-argument form, so it still counts as a usage error rather
// than a problem to report. Cloud-only commands parse no flags and refuse on
// their own.
func refuseProjectFlag(cmds []*cobra.Command) []*cobra.Command {
	for _, cmd := range cmds {
		if cmd.DisableFlagParsing {
			continue
		}
		cmd.PersistentFlags().String("project", "", "")
		_ = cmd.PersistentFlags().MarkHidden("project")
		cmd.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
			if !cmd.Flags().Changed("project") {
				return nil
			}
			value, _ := cmd.Flags().GetString("project")
			return fmt.Errorf("invalid argument %q for \"--project\" flag: %w", value, errLocalProjectFlag)
		}
	}
	return cmds
}

// New returns the deprecated local command tree.
func New(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "local",
		Short: "Manage local development resources",
		Long:  "Manage resources in the local development project backed by the running Volcano server container.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(NewResourceCommands(deps)...)
	return cmdutil.HideDeprecatedAlias(cmd, `warning: "volcano local ..." is deprecated; use direct local commands such as "volcano functions deploy"`)
}

func withLocalConfig(deps cliruntime.Deps, cache *infoCache) cliruntime.Deps {
	deps.CommandPathPrefix = "volcano"
	// Local mode is a single-tenant sandbox: session clients send no credential
	// and the local server defaults to the pre-provisioned local user.
	deps.LocalMode = true
	deps.ConfigLoader = func() (*cliconfig.Config, error) {
		ctx, cancel := context.WithTimeout(context.Background(), localInfoTimeout)
		defer cancel()

		info, err := cache.load(ctx)
		if err != nil {
			return nil, err
		}

		// Local mode never uses a project link, so a bad one must not cost the
		// user's function aliases.
		var functionAliases map[string]map[string]string
		if persisted, err := cliconfig.LoadWithoutLink(); err == nil {
			functionAliases = persisted.FunctionAliases
		}

		return &cliconfig.Config{
			APIBaseURL:      info.APIURL,
			UserToken:       info.UserToken,
			UserID:          info.UserID,
			FunctionAliases: functionAliases,
			CurrentProject: &cliconfig.ProjectConfig{
				ID:   info.ProjectID,
				Name: info.ProjectName,
			},
			IgnoreEnv: true,
		}, nil
	}
	return deps
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

// Sandbox APIs retain authentication locally, unlike the function harness.
func withLocalSandboxConfig(deps cliruntime.Deps, cache *infoCache) cliruntime.Deps {
	local := withLocalConfig(deps, cache)
	load := local.ConfigLoader
	local.LocalMode = false
	local.ConfigLoader = func() (*cliconfig.Config, error) {
		cfg, err := load()
		if err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), localInfoTimeout)
		defer cancel()
		info, err := cache.load(ctx)
		if err != nil {
			return nil, err
		}
		cfg.UserToken = info.ServiceKey
		return cfg, nil
	}
	return local
}
