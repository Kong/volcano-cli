package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/config"
)

// LogoutOutcome says what logging out did on the server.
type LogoutOutcome int

const (
	// LogoutRevoked means the server revoked the saved token and the session
	// it came from.
	LogoutRevoked LogoutOutcome = iota
	// LogoutAlreadyEnded means the server no longer accepted the saved token:
	// it was revoked elsewhere or expired, so there was nothing left to revoke.
	LogoutAlreadyEnded
	// LogoutNothingSaved means no login was saved, so nothing was revoked.
	LogoutNothingSaved
	// LogoutUnreadable means the saved login could not be read, so nothing was
	// revoked. The file is removed anyway.
	LogoutUnreadable
	// LogoutProjectToken means the saved login was a project access token,
	// which revoking the calling token does not cover. It stays valid.
	LogoutProjectToken
	// LogoutNotRevoked means the server was asked and did not revoke the
	// token, which stays valid until it expires. See the returned error.
	LogoutNotRevoked
)

// Logout ends the saved login: it asks the server to revoke the saved token and
// the session it came from, then removes the login, the current project, and
// function aliases from the local config. The local config is cleared whatever
// the server answers, so a failed revoke still logs this machine out; that
// failure is returned after clearing. A VOLCANO_TOKEN in the environment is
// neither revoked nor unset: it was never saved.
func (s Service) Logout(ctx context.Context) (LogoutOutcome, error) {
	outcome, revokeErr := s.revokeSavedLogin(ctx)
	if err := config.ClearLogin(); err != nil {
		return outcome, errors.Join(revokeErr, err)
	}
	return outcome, revokeErr
}

func (s Service) revokeSavedLogin(ctx context.Context) (LogoutOutcome, error) {
	cfg, readable := s.savedConfig()
	if !readable {
		return LogoutUnreadable, nil
	}
	token := strings.TrimSpace(cfg.UserToken)
	switch {
	case token == "":
		return LogoutNothingSaved, nil
	case config.IsProjectToken(token):
		return LogoutProjectToken, nil
	}

	client, err := s.sessions.APIClient(s.apiURL(cfg), token)
	if err != nil {
		return LogoutNotRevoked, err
	}
	err = client.RevokeCurrentToken(ctx)
	switch {
	case err == nil:
		return LogoutRevoked, nil
	case api.Status(err) == http.StatusUnauthorized:
		return LogoutAlreadyEnded, nil
	default:
		return LogoutNotRevoked, err
	}
}

// savedConfig reads the local config. One that cannot be read holds no login
// that could be revoked; logging out removes it all the same.
func (s Service) savedConfig() (*config.Config, bool) {
	cfg, err := s.sessions.Config()
	return cfg, err == nil
}
