// Package dataplane obtains project data-plane credentials for cloud commands.
package dataplane

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clisession "github.com/Kong/volcano-cli/internal/session"
)

// CLIServiceKeyName is the reserved project service key used by cloud data-plane
// commands when the platform token cannot call the runtime route directly.
const CLIServiceKeyName = "volcano-cli-data-plane"

// serviceKeyTokenPrefix identifies a data-plane service key (mirrors the server's
// auth.ServiceKeyPrefix). A caller already holding such a key is authenticated
// for the data plane directly, so the reserved-key list/create is neither needed
// nor possible (control-plane routes reject a service key with 401).
const serviceKeyTokenPrefix = "sk-"

// cliDataPlanePermissions is the least-privilege scope requested for the reserved
// data-plane key: function invocation and storage object I/O only. It must cover
// every operation the CLI performs with this key (copy/move/set-visibility map
// to storage.upload server-side), and nothing else.
var cliDataPlanePermissions = []string{
	"functions.invoke",
	"storage.upload",
	"storage.download",
	"storage.list",
	"storage.delete",
}

// Service obtains data-plane credentials for the current cloud project.
type Service struct {
	sessions clisession.Factory
	keyName  string
}

// NewService returns a data-plane credential service.
func NewService(deps cliruntime.Deps) Service {
	return Service{
		sessions: clisession.NewFactory(deps),
		keyName:  CLIServiceKeyName,
	}
}

// ServiceKey returns the reserved service key for the current project, creating
// it when it does not already exist.
func (s Service) ServiceKey(ctx context.Context) (string, error) {
	project, err := s.sessions.CurrentProject()
	if err != nil {
		return "", err
	}
	return s.ServiceKeyForProject(ctx, project)
}

// ServiceKeyForProject returns the reserved service key for project, creating it
// when it does not already exist.
func (s Service) ServiceKeyForProject(ctx context.Context, project *clisession.ProjectSession) (string, error) {
	if project == nil {
		return "", errors.New("project session is required")
	}
	// When the session is already authenticated with a service key (e.g.
	// VOLCANO_TOKEN is a scoped data-plane key), use it directly. It is already a
	// data-plane credential, and resolving the reserved CLI key would require a
	// control-plane list/create that a service key is not permitted to make (401).
	if token := sessionServiceKey(project); token != "" {
		return token, nil
	}
	name := s.serviceKeyName()
	key, found, err := s.findServiceKey(ctx, project, name)
	if err != nil {
		return "", err
	}
	if found {
		return s.serviceKeyValue(ctx, project, key)
	}

	created, err := project.API.CreateServiceKey(ctx, project.ProjectID, name, cliDataPlanePermissions)
	if api.Status(err) == http.StatusConflict {
		return s.serviceKeyAfterCreateConflict(ctx, project, name)
	}
	if err != nil {
		return "", fmt.Errorf("failed to create CLI service key %q: %w", name, err)
	}
	return serviceKeyPlaintext(created, name)
}

func sessionServiceKey(project *clisession.ProjectSession) string {
	if project.Config == nil {
		return ""
	}
	token := strings.TrimSpace(project.Config.Token())
	if strings.HasPrefix(token, serviceKeyTokenPrefix) {
		return token
	}
	return ""
}

func (s Service) serviceKeyName() string {
	name := strings.TrimSpace(s.keyName)
	if name == "" {
		return CLIServiceKeyName
	}
	return name
}

func (s Service) serviceKeyAfterCreateConflict(ctx context.Context, project *clisession.ProjectSession, name string) (string, error) {
	key, found, err := s.findServiceKey(ctx, project, name)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("CLI service key %q already exists but could not be loaded", name)
	}
	return s.serviceKeyValue(ctx, project, key)
}

func (s Service) findServiceKey(ctx context.Context, project *clisession.ProjectSession, name string) (*apiclient.ServiceKey, bool, error) {
	for page := api.DefaultPage; ; page++ {
		keys, err := project.API.ListServiceKeys(ctx, project.ProjectID, page, api.DefaultLimit)
		if err != nil {
			return nil, false, fmt.Errorf("failed to list service keys: %w", err)
		}
		if keys == nil {
			return nil, false, nil
		}
		for i := range keys.Data {
			if strings.EqualFold(keys.Data[i].Name, name) {
				return &keys.Data[i], true, nil
			}
		}
		if !keys.HasMore {
			return nil, false, nil
		}
	}
}

func (s Service) serviceKeyValue(ctx context.Context, project *clisession.ProjectSession, key *apiclient.ServiceKey) (string, error) {
	if value, ok := serviceKeyPlaintextOK(key); ok {
		return value, nil
	}
	loaded, err := project.API.GetServiceKey(ctx, project.ProjectID, key.Id)
	if err != nil {
		return "", fmt.Errorf("failed to load CLI service key %q: %w", key.Name, err)
	}
	return serviceKeyPlaintext(loaded, key.Name)
}

func serviceKeyPlaintext(key *apiclient.ServiceKey, name string) (string, error) {
	if value, ok := serviceKeyPlaintextOK(key); ok {
		return value, nil
	}
	return "", fmt.Errorf("service key %q did not include key material", name)
}

func serviceKeyPlaintextOK(key *apiclient.ServiceKey) (string, bool) {
	if key == nil || key.KeyValue == nil {
		return "", false
	}
	value := strings.TrimSpace(*key.KeyValue)
	return value, value != ""
}
