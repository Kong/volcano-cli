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
	// LogoutNotRevoked means the revoke failed in a way that can pass, such
	// as a network error or a server error. The login stays saved, so logging
	// out again can end it. See the returned error.
	LogoutNotRevoked
	// LogoutRefused means the server answered and would not revoke the token,
	// or the CLI could not build a request for it, so trying again would get
	// the same answer. The login is removed from this machine anyway, and the
	// token stays valid until it expires. See the returned error.
	LogoutRefused
	// LogoutNoServerSession means the server keeps no sign-in to end, as in
	// local mode, so only this machine's login was removed.
	LogoutNoServerSession
)

// Logout ends the saved login: it asks the server to revoke the saved token and
// the session it came from, then removes the login, the current project, and
// function aliases from the local config. When the revoke failed in a way that
// can pass, the login stays saved and the failure is returned: removing it
// would leave a token the server still accepts and this CLI can no longer end,
// while keeping it lets logging out again finish the job. When the server
// refused, the login is removed anyway, so a server that cannot end it does not
// keep this machine logged in, and the refusal is returned after clearing. A
// VOLCANO_TOKEN in the environment is neither revoked nor unset: it was never
// saved.
func (s Service) Logout(ctx context.Context) (LogoutOutcome, error) {
	outcome, revokeErr := s.revokeSavedLogin(ctx)
	if outcome == LogoutNotRevoked {
		return outcome, revokeErr
	}
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
		return LogoutRefused, err
	}
	err = client.RevokeCurrentToken(ctx)
	switch status := api.Status(err); {
	case err == nil:
		return LogoutRevoked, nil
	case status == http.StatusUnauthorized:
		return LogoutAlreadyEnded, nil
	case status == http.StatusNotImplemented:
		return LogoutNoServerSession, nil
	case revokeCanPass(status):
		return LogoutNotRevoked, err
	default:
		return LogoutRefused, err
	}
}

// revokeCanPass reports whether a failed revoke is worth trying again: no
// answer at all (status 0, such as a network error or an interrupted command),
// a server error, or a request the server shed.
func revokeCanPass(status int) bool {
	return status == 0 || status >= http.StatusInternalServerError ||
		status == http.StatusTooManyRequests || status == http.StatusRequestTimeout
}

// savedConfig reads the local config. One that cannot be read holds no login
// that could be revoked; logging out removes it all the same.
func (s Service) savedConfig() (*config.Config, bool) {
	cfg, err := s.sessions.Config()
	return cfg, err == nil
}
