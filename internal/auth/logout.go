package auth

import (
	"context"
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
	// as a network error, a server error, or a request that couldn't be built. The login stays saved, so logging
	// out again can end it. See the returned error.
	LogoutNotRevoked
	// LogoutRefused means the server answered and would not revoke the token,
	// so trying again would get the same answer. The login is removed from this machine anyway, and the
	// token stays valid until it expires. See the returned error.
	LogoutRefused
	// LogoutNoServerSession means the server keeps no sign-in to end, as in
	// local mode, so only this machine's login was removed.
	LogoutNoServerSession
	// LogoutNotAsked means the caller chose not to reach the server: only this
	// machine's login was removed, and its token stays valid until it expires.
	LogoutNotAsked
)

// LogoutResult says what logging out did on the server and on this machine.
type LogoutResult struct {
	Outcome LogoutOutcome
	// RevokeErr is why the server did not end the login, for LogoutNotRevoked
	// and LogoutRefused.
	RevokeErr error
	// ClearErr is why the login could not be removed from this machine. The
	// login is not removed for LogoutNotRevoked, so it is always nil there.
	ClearErr error
	// EnvTokenEnded reports that VOLCANO_TOKEN holds the saved token, which the
	// server no longer accepts, so commands that use it now fail.
	EnvTokenEnded bool
	// Replaced reports that another login was saved while logout reached the
	// server, so it was kept rather than cleared.
	Replaced bool
	// AccountToken reports that the saved token carries the account-token
	// prefix, so a refusal can't mean it was a project access token.
	AccountToken bool
}

// Logout ends the saved login: it asks the server to revoke the saved token and
// the session it came from, then removes the login, the current project, and
// function aliases from the local config. When the revoke failed in a way that
// can pass, the login stays saved: removing it would leave a token the server
// still accepts and this CLI can no longer end, while keeping it lets logging
// out again finish the job. When the server refused, the login is removed
// anyway, so a server that cannot end it does not keep this machine logged in.
// A VOLCANO_TOKEN in the environment is never unset, and revoked only when it
// is the saved token.
func (s Service) Logout(ctx context.Context) LogoutResult {
	outcome, token, revokeErr := s.revokeSavedLogin(ctx)
	result := LogoutResult{Outcome: outcome, RevokeErr: revokeErr}
	if outcome == LogoutNotRevoked {
		return result
	}
	if outcome == LogoutRevoked || outcome == LogoutAlreadyEnded {
		env := &config.Config{}
		result.EnvTokenEnded = env.TokenFromEnv() && env.Token() == token
	}
	result.AccountToken = strings.HasPrefix(strings.ToLower(token), config.AccountTokenPrefix)
	if outcome == LogoutUnreadable {
		result.ClearErr = config.ClearLogin()
		return result
	}
	// Only the login logout read is cleared: one saved in another terminal
	// while the server answered is a different login, still live there.
	cleared, err := config.ClearLoginOf(token)
	result.ClearErr = err
	result.Replaced = err == nil && !cleared
	return result
}

// LogoutWithoutRevoke removes this machine's login without reaching the
// server, for when it can't be reached. The token stays valid until it
// expires.
func (s Service) LogoutWithoutRevoke() LogoutResult {
	return LogoutResult{Outcome: LogoutNotAsked, ClearErr: config.ClearLogin()}
}

// revokeSavedLogin asks the server to revoke the saved token, and returns the
// token it found, if any.
func (s Service) revokeSavedLogin(ctx context.Context) (LogoutOutcome, string, error) {
	cfg, readable := s.savedConfig()
	if !readable {
		return LogoutUnreadable, "", nil
	}
	token := strings.TrimSpace(cfg.UserToken)
	switch {
	case token == "":
		return LogoutNothingSaved, "", nil
	case config.IsProjectToken(token):
		return LogoutProjectToken, token, nil
	}

	client, err := s.sessions.APIClient(s.apiURL(cfg), token)
	if err != nil {
		// A request that can't be built, such as for a malformed API URL,
		// can be sent once the user fixes the cause, so keep the login.
		return LogoutNotRevoked, token, err
	}
	err = client.RevokeCurrentToken(ctx)
	switch status := api.Status(err); {
	case err == nil:
		return LogoutRevoked, token, nil
	case status == http.StatusUnauthorized:
		return LogoutAlreadyEnded, token, nil
	case status == http.StatusNotImplemented:
		return LogoutNoServerSession, token, nil
	case revokeCanPass(status):
		return LogoutNotRevoked, token, err
	default:
		return LogoutRefused, token, err
	}
}

// revokeCanPass reports whether a failed revoke is worth trying again: no
// answer at all (status 0, such as a network error or an interrupted command),
// a server error, a request the server shed, or a CLI too old for the server,
// which works again after an upgrade.
func revokeCanPass(status int) bool {
	return status == 0 || status >= http.StatusInternalServerError ||
		status == http.StatusTooManyRequests || status == http.StatusRequestTimeout ||
		status == http.StatusUpgradeRequired
}

// savedConfig reads the local config. One that cannot be read holds no login
// that could be revoked; logging out removes it all the same. Logout never uses
// the working directory's project link, so a broken one must not hide the
// saved login: it is read without the link unless a loader is set.
func (s Service) savedConfig() (*config.Config, bool) {
	load := s.sessions.Config
	if s.deps.ConfigLoader == nil {
		load = config.LoadWithoutLink
	}
	cfg, err := load()
	return cfg, err == nil
}
