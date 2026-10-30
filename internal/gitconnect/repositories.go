package gitconnect

import (
	"context"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// Installation is one GitHub App installation the caller's GitHub connection
// reaches, and the repositories it grants.
type Installation struct {
	Connection   apiclient.GitConnection
	Installation apiclient.GitInstallation
	Repositories []apiclient.GitRepository
}

// Repositories lists every repository the caller can connect a project to,
// grouped by the installation that grants it.
//
// Any failed lookup fails the whole list. A list with an installation missing
// would read as "the App cannot see those repositories", which is a different
// problem with a different fix.
func (s Service) Repositories(ctx context.Context) ([]Installation, error) {
	authenticated, err := s.accountScoped()
	if err != nil {
		return nil, err
	}

	connections, err := authenticated.API.ListGitConnections(ctx)
	if err != nil {
		return nil, classifyProvider(err, "failed to list your GitHub connections")
	}
	usable := githubConnections(connections)
	if len(usable) == 0 {
		return nil, ErrNoGitHubConnection
	}

	var listed []Installation
	for _, connection := range usable {
		installations, err := authenticated.API.ListGitInstallations(ctx, connection.Id)
		if err != nil {
			return nil, classifyProvider(err, "failed to list your GitHub App installations")
		}
		for _, installation := range installations {
			repositories, err := authenticated.API.ListGitInstallationRepositories(ctx, connection.Id, installation.Id)
			if err != nil {
				return nil, classifyProvider(err, "failed to list repositories for "+installation.AccountLogin)
			}
			listed = append(listed, Installation{
				Connection:   connection,
				Installation: installation,
				Repositories: repositories,
			})
		}
	}
	return listed, nil
}
