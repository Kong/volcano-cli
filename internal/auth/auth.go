// Package auth implements the device-code login flow against the cloud API.
package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/config"
	"github.com/Kong/volcano-cli/internal/localmode"
	cliproject "github.com/Kong/volcano-cli/internal/project"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clisession "github.com/Kong/volcano-cli/internal/session"
)

const maxConsecutiveDevicePollFailures = 3

// Credentials are the platform credentials produced by a successful login.
type Credentials struct {
	Token  string
	UserID string
	// Project is the project login validated the token against, to be saved as
	// the active project. Nil when login resolved no project.
	Project *config.ProjectConfig
	// APIURL is the API that issued or validated Token, saved with it so later
	// commands keep talking to the same API.
	APIURL string
}

// Service performs Volcano authentication workflows.
type Service struct {
	deps     cliruntime.Deps
	sessions clisession.Factory
}

// NewService returns an authentication service.
func NewService(deps cliruntime.Deps) Service {
	return Service{deps: deps, sessions: clisession.NewFactory(deps)}
}

// projectOrigin names where the project came from when the user did not pass
// --project, so a failure can say which project it ran against and why.
type projectOrigin string

const (
	// projectNamed is --project, which the user typed and does not need naming.
	projectNamed projectOrigin = ""
	// projectFromEnvironment is VOLCANO_PROJECT_ID, which is deprecated.
	projectFromEnvironment projectOrigin = "VOLCANO_PROJECT_ID"
	// projectFromSelection is the project a previous `volcano use` saved, which
	// belongs to whatever credential was logged in before this one.
	projectFromSelection projectOrigin = "the project 'volcano use' last selected"
)

// LoginWithToken validates token and returns credentials to persist. project
// names the project to validate against and select, by ID or by name; it is
// optional for an account token and required for a project access token, which
// cannot list projects and so cannot tell the CLI which project it belongs to.
//
// A one-time bootstrap token is exchanged first, and the login it returns is
// saved right away: the exchange spends the token, so a failure after it must
// not throw the login away. Such a failure is a *LoginSavedError.
func (s Service) LoginWithToken(ctx context.Context, cfg *config.Config, token, project string) (Credentials, error) {
	token = strings.TrimSpace(token)
	if !config.IsBootstrapToken(token) {
		return s.loginWithToken(ctx, cfg, token, "", project)
	}
	apiURL := s.apiURL(cfg)
	exchange, err := s.exchangeBootstrapToken(ctx, apiURL, token)
	if err != nil {
		return Credentials{}, err
	}
	if err := SaveCredentials(Credentials{Token: exchange.Token, UserID: exchange.UserID, APIURL: apiURL}); err != nil {
		return Credentials{}, fmt.Errorf("failed to save credentials: %w", err)
	}
	credentials, err := s.loginWithToken(ctx, cfg, exchange.Token, exchange.UserID, project)
	if err != nil {
		return Credentials{}, &LoginSavedError{Err: err}
	}
	return credentials, nil
}

// LoginSavedError is a failure after a one-time token was exchanged and its
// login saved. The token is spent, but the saved login works without it.
type LoginSavedError struct{ Err error }

func (e *LoginSavedError) Error() string { return e.Err.Error() }

func (e *LoginSavedError) Unwrap() error { return e.Err }

// loginWithToken validates token, an account or project access token, for
// LoginWithToken. userID is the user a bootstrap exchange named, if any.
func (s Service) loginWithToken(ctx context.Context, cfg *config.Config, token, userID, project string) (Credentials, error) {
	project = strings.TrimSpace(project)
	apiURL := s.apiURL(cfg)
	client, err := s.sessions.APIClient(apiURL, token)
	if err != nil {
		return Credentials{}, err
	}

	origin := projectNamed
	if project == "" && config.IsProjectToken(token) {
		project, origin = unnamedProject(cfg)
		if project == "" {
			return Credentials{}, fmt.Errorf("a project access token (%s) is scoped to one project: pass --project <project-id> "+
				"with the project it was minted in", config.ProjectTokenPrefix)
		}
	}
	if project == "" {
		if err := client.ValidateToken(ctx); err != nil {
			if api.Status(err) == http.StatusUnauthorized {
				return Credentials{}, invalidTokenError{err}
			}
			return Credentials{}, fmt.Errorf("failed to validate token: %w", err)
		}
		return Credentials{Token: token, UserID: userID, APIURL: apiURL}, nil
	}

	selected, err := resolveLoginProject(ctx, client, token, project, origin)
	if err != nil {
		return Credentials{}, err
	}
	return Credentials{
		Token:   token,
		UserID:  userID,
		Project: &config.ProjectConfig{ID: selected.Id.String(), Name: selected.Name},
		APIURL:  apiURL,
	}, nil
}

// bootstrapClientName names the CLI to the API when it exchanges a bootstrap
// token, so the new login is listed as this CLI on this machine.
const bootstrapClientName = "volcano-cli"

// BootstrapRefusedError is a one-time bootstrap token the API would not
// exchange: expired, already used, not valid, rate limited, or not supported
// by this API.
type BootstrapRefusedError struct {
	// Reason completes "The one-time token ...", e.g. "has expired".
	Reason string
	Err    error
}

func (e *BootstrapRefusedError) Error() string {
	return "the one-time token " + e.Reason + ". Copy a new prompt from the dashboard, or sign in in the browser"
}

func (e *BootstrapRefusedError) Unwrap() error { return e.Err }

// exchangeBootstrapToken trades a vbt_ token for a normal CLI login.
func (s Service) exchangeBootstrapToken(ctx context.Context, apiURL, token string) (*api.BootstrapExchange, error) {
	client, err := s.sessions.APIClient(apiURL, "")
	if err != nil {
		return nil, err
	}
	hostname, _ := os.Hostname()
	exchange, err := client.ExchangeBootstrapToken(ctx, token, bootstrapClientName, hostname)
	if err == nil {
		return exchange, nil
	}
	switch {
	case api.Code(err) == api.BootstrapTokenExpired:
		return nil, &BootstrapRefusedError{Reason: "has expired", Err: err}
	case api.Code(err) == api.BootstrapTokenUsed:
		return nil, &BootstrapRefusedError{Reason: "was already used", Err: err}
	case api.Code(err) == api.BootstrapTokenInvalid:
		return nil, &BootstrapRefusedError{Reason: "is not valid", Err: err}
	case api.Status(err) == http.StatusTooManyRequests:
		return nil, &BootstrapRefusedError{Reason: "was refused after too many attempts from this network", Err: err}
	case api.Status(err) == http.StatusNotFound:
		// An API without the exchange endpoint.
		return nil, &BootstrapRefusedError{Reason: "can't be used with this API", Err: err}
	}
	return nil, err
}

// unnamedProject falls back to the project the CLI already has for a project
// access token logged in without --project, and reports where it came from.
func unnamedProject(cfg *config.Config) (string, projectOrigin) {
	if fromEnv := strings.TrimSpace(cfg.ProjectIDFromEnv()); fromEnv != "" {
		return fromEnv, projectFromEnvironment
	}
	if cfg.Link != nil {
		return strings.TrimSpace(cfg.Link.Project.ID), projectOrigin("this directory's project link, " + cfg.Link.Path())
	}
	return strings.TrimSpace(cfg.ProjectID()), projectFromSelection
}

// resolveLoginProject finds the project to validate the token against.
//
// A name is resolved the way `volcano use` resolves one, so `--project my-app`
// means the same thing in both. Only an account token can do that: the scan
// lists every project, which a project access token cannot, so it has to be
// given the ID.
func resolveLoginProject(
	ctx context.Context, client *api.Client, token, identifier string, origin projectOrigin,
) (*apiclient.Project, error) {
	if id, err := uuid.Parse(identifier); err == nil {
		selected, err := client.GetProject(ctx, id)
		if err != nil {
			return nil, projectTokenValidationError(identifier, origin, err)
		}
		return selected, nil
	}

	if config.IsProjectToken(token) {
		return nil, fmt.Errorf("invalid project %q: a project access token (%s) cannot look up a project by name, "+
			"so name the project it was minted in by ID", identifier, config.ProjectTokenPrefix)
	}
	return cliproject.Find(ctx, client, identifier)
}

// projectTokenValidationError separates a token the API rejects outright from
// one that is simply not this project's, which is the likely mistake when a
// project access token is paired with the wrong --project.
func projectTokenValidationError(projectID string, origin projectOrigin, err error) error {
	switch api.Status(err) {
	case http.StatusUnauthorized:
		return invalidTokenError{err}
	case http.StatusForbidden, http.StatusNotFound:
		if origin == projectNamed {
			return fmt.Errorf("token is not valid for project %s: %w", projectID, err)
		}
		// Nothing the user typed names this project, so the failure has to say
		// where it came from before "not valid for project <id>" means anything.
		return fmt.Errorf("token is not valid for project %s, taken from %s: pass --project with the project "+
			"the token was minted in: %w", projectID, origin, err)
	default:
		return fmt.Errorf("failed to validate token: %w", err)
	}
}

// invalidTokenError is a token the API refused with HTTP 401. It reads as
// "invalid token" and unwraps to the API's error, so the exit code reports a
// rejected credential.
type invalidTokenError struct{ err error }

func (e invalidTokenError) Error() string { return "invalid token" }

func (e invalidTokenError) Unwrap() error { return e.err }

// Signup routes the browser through Volcano Web's own signup page (account
// creation isn't something the device-authorization response's verification
// page handles), then to a same-origin /device path. cfg.WebURLForAPIURL
// (explicit VOLCANO_WEB_URL, else derived from apiURL, else the compiled
// default) is the signup origin: the verification response's own URI can't be
// used for this, since for Volcano's first-party CLI client it now points at
// the API's own managed-hosted-auth page (a different origin, per
// docs/api-reference/cli-authentication.md in volcano-hosting), and Volcano
// Web's signup page only accepts a same-origin relative `next` value anyway
// (isSafeInternalPath in volcano-web rejects absolute URLs).
func (s Service) Signup(ctx context.Context, cfg *config.Config, email string, w io.Writer) (Credentials, error) {
	apiURL := s.apiURL(cfg)
	clientID, err := resolveDeviceClientID(apiURL)
	if err != nil {
		return Credentials{}, err
	}
	// Fail fast on an explicitly misconfigured VOLCANO_WEB_URL before allocating a
	// device code, instead of burning a device authorization.
	if webOverride, ok := cfg.WebURLOverride(); ok {
		if _, err := api.WebSignupURL(webOverride, email, ""); err != nil {
			return Credentials{}, err
		}
	}
	client, err := s.sessions.APIClient(apiURL, "")
	if err != nil {
		return Credentials{}, err
	}

	deviceAuth, err := client.StartDeviceAuthorization(ctx, clientID)
	if err != nil {
		return Credentials{}, err
	}

	signupURL, err := api.WebSignupURL(cfg.WebURLForAPIURL(apiURL), email, deviceApprovalPath(deviceAuth))
	if err != nil {
		return Credentials{}, err
	}

	fmt.Fprintln(w, "\nInitiating browser signup...")
	credentials, err := s.completeBrowserLogin(ctx, client, clientID, deviceAuth, w, signupURL)
	if err != nil {
		return Credentials{}, err
	}
	credentials.APIURL = apiURL
	return credentials, nil
}

// LoginWithBrowser runs the OAuth device flow and opens the browser at the
// verification URI the device-authorization response returned, unmodified.
// That page (for Volcano's first-party CLI client, the API's own managed
// hosted-auth page, action=device) is self-contained: it signs the user in
// and asks them to approve the code itself, so login needs no Volcano Web
// routing of its own (see docs/api-reference/cli-authentication.md and
// docs/authentication/managed-hosted-pages.md in volcano-hosting).
func (s Service) LoginWithBrowser(ctx context.Context, cfg *config.Config, w io.Writer) (Credentials, error) {
	apiURL := s.apiURL(cfg)
	clientID, err := resolveDeviceClientID(apiURL)
	if err != nil {
		return Credentials{}, err
	}
	client, err := s.sessions.APIClient(apiURL, "")
	if err != nil {
		return Credentials{}, err
	}

	deviceAuth, err := client.StartDeviceAuthorization(ctx, clientID)
	if err != nil {
		return Credentials{}, err
	}

	fmt.Fprintln(w, "\nInitiating browser authentication...")
	verificationURL := strings.TrimSpace(deviceAuth.VerificationUriComplete)
	if verificationURL == "" {
		verificationURL = strings.TrimSpace(deviceAuth.VerificationUri)
	}
	credentials, err := s.completeBrowserLogin(ctx, client, clientID, deviceAuth, w, verificationURL)
	if err != nil {
		return Credentials{}, err
	}
	credentials.APIURL = apiURL
	return credentials, nil
}

// deviceApprovalPath is the same-origin path Volcano Web's own /device page
// lives at, used only as signup's post-signup next hop. It doesn't derive
// from the verification response (see the Signup doc comment above).
func deviceApprovalPath(deviceAuth *apiclient.DeviceAuthorizationResponse) string {
	if userCode := strings.TrimSpace(deviceAuth.UserCode); userCode != "" {
		return "/device?" + url.Values{"user_code": []string{userCode}}.Encode()
	}
	return "/device"
}

// resolveDeviceClientID returns the device OAuth client id for the login flow.
// When the CLI is pointed at a loopback address the local server only knows the
// deterministic local device client, so issue it directly; otherwise defer to
// the configured id.
func resolveDeviceClientID(apiURL string) (string, error) {
	if isLocalAPIURL(apiURL) {
		return localmode.DeviceClientID, nil
	}
	return config.FirstPartyDeviceClientID()
}

// isLocalAPIURL reports whether apiURL points at a loopback address.
func isLocalAPIURL(apiURL string) bool {
	return config.IsLoopbackAPIURL(apiURL)
}

// SaveCredentials saves a login's credentials to ~/.volcano/config.json.
func SaveCredentials(credentials Credentials) error {
	return config.Update(func(cfg *config.Config) (bool, error) {
		cfg.UserToken = credentials.Token
		cfg.UserID = credentials.UserID
		cfg.SessionAPIURL = credentials.APIURL
		if credentials.Project != nil {
			cfg.CurrentProject = credentials.Project
		}
		return true, nil
	})
}

// SelectInLink saves the project a --project login selected to cfg's link, as
// `volcano use` does inside a linked directory: the link would otherwise keep
// overriding the selection. It talks to the API with the new login.
func (s Service) SelectInLink(ctx context.Context, cfg *config.Config, credentials Credentials) (cliproject.UseResult, error) {
	id, err := uuid.Parse(credentials.Project.ID)
	if err != nil {
		return cliproject.UseResult{}, fmt.Errorf("invalid project ID %q: %w", credentials.Project.ID, err)
	}
	client, err := s.sessions.APIClient(credentials.APIURL, credentials.Token)
	if err != nil {
		return cliproject.UseResult{}, err
	}
	selected := &apiclient.Project{Id: id, Name: credentials.Project.Name}
	return cliproject.SelectInLink(ctx, client, credentials.APIURL, cfg.Link, selected)
}

// GitEmail returns `git config --global user.email`, or "" when it is unset
// or git fails. Signup offers it as the default email address.
func GitEmail(ctx context.Context, deps cliruntime.Deps) string {
	runner := deps.GitCommandRunner
	if runner == nil {
		runner = cliruntime.CommandRunnerFunc(func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).Output() //nolint:gosec // command name and args are static below
		})
	}
	out, err := runner.Run(ctx, "git", "config", "--global", "user.email")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Logout deletes local authentication state.
func (s Service) Logout() error {
	return config.Delete()
}

func (s Service) apiURL(cfg *config.Config) string {
	return s.sessions.APIURL(cfg)
}

func (s Service) completeBrowserLogin(ctx context.Context, client *api.Client, clientID string, deviceAuth *apiclient.DeviceAuthorizationResponse, w io.Writer, browserURL string) (Credentials, error) {
	fmt.Fprintf(w, "\nCode: %s\n", deviceAuth.UserCode)
	fmt.Fprintf(w, "Opening browser: %s\n", browserURL)

	if err := cliruntime.OpenBrowser(s.deps, browserURL); err != nil { //nolint:contextcheck // browser launch is fire-and-forget; auth ctx would cancel the spawned browser
		fmt.Fprintln(w, "\n(If browser didn't open, visit the URL above)")
	}

	fmt.Fprint(w, "\nWaiting for authentication")
	return s.pollDeviceLogin(ctx, client, clientID, devicePoll{
		deviceCode: deviceAuth.DeviceCode,
		expiresIn:  time.Duration(deviceAuth.ExpiresIn) * time.Second,
		interval:   time.Duration(deviceAuth.Interval) * time.Second,
	}, w)
}

// devicePoll is a device code to poll for a login, and how.
type devicePoll struct {
	deviceCode string
	// expiresIn bounds the wait; zero or less times out at once.
	expiresIn time.Duration
	// interval is the time between polls; zero or less means 5 seconds.
	interval time.Duration
	// pollNow polls once before the first interval, for a code that is
	// already approved.
	pollNow bool
}

// errDeviceLoginTimeout is a device login that was still pending when its
// wait ran out.
var errDeviceLoginTimeout = errors.New("authentication timeout")

// deviceTokenRefusal is a device token poll the API answered with an OAuth
// error that ends the login, such as expired_token or access_denied.
type deviceTokenRefusal struct {
	code        string
	description string
}

func (e *deviceTokenRefusal) Error() string {
	switch e.code {
	case "access_denied":
		return "authorization denied"
	case "expired_token":
		return "device code expired"
	}
	if e.description != "" {
		return e.description
	}
	return "authentication failed: " + e.code
}

// pollDeviceLogin polls the device token endpoint until the code is approved,
// then exchanges the token for a platform login. It prints a dot each second
// and ends the line before it returns.
func (s Service) pollDeviceLogin(ctx context.Context, client *api.Client, clientID string, poll devicePoll, w io.Writer) (Credentials, error) {
	credentials, err := s.waitForDeviceToken(ctx, client, clientID, poll, w)
	fmt.Fprintln(w)
	return credentials, err
}

func (s Service) waitForDeviceToken(ctx context.Context, client *api.Client, clientID string, poll devicePoll, w io.Writer) (Credentials, error) {
	timeout := cliruntime.NewTimer(s.deps, poll.expiresIn)
	defer timeout.Stop()
	poller := devicePoller{client: client, clientID: clientID, deviceCode: poll.deviceCode, interval: poll.interval}
	if poller.interval <= 0 {
		poller.interval = 5 * time.Second
	}
	poller.ticker = cliruntime.NewTicker(s.deps, poller.interval)
	defer poller.ticker.Stop()
	dotTicker := cliruntime.NewTicker(s.deps, time.Second)
	defer dotTicker.Stop()

	if poll.pollNow {
		if credentials, done, err := poller.poll(ctx); done {
			return credentials, err
		}
		// The API answers slow_down to a poll that comes sooner than the
		// interval after the last one, so count the interval from this poll.
		poller.ticker.Reset(poller.interval)
	}
	for {
		select {
		case <-ctx.Done():
			return Credentials{}, ctx.Err()
		case <-timeout.C():
			if poll.expiresIn <= 0 {
				return Credentials{}, errDeviceLoginTimeout
			}
			return Credentials{}, fmt.Errorf("%w (%s)", errDeviceLoginTimeout, poll.expiresIn.Round(time.Second))
		case <-dotTicker.C():
			fmt.Fprint(w, ".")
		case <-poller.ticker.C():
			if credentials, done, err := poller.poll(ctx); done {
				return credentials, err
			}
		}
	}
}

// devicePoller polls one device code and keeps the state between polls.
type devicePoller struct {
	client     *api.Client
	clientID   string
	deviceCode string
	interval   time.Duration
	ticker     cliruntime.Ticker
	failures   int
}

// poll polls the device token endpoint once. done is false while the login is
// still pending.
func (p *devicePoller) poll(ctx context.Context) (credentials Credentials, done bool, err error) {
	status, err := p.client.PollDeviceToken(ctx, p.clientID, p.deviceCode)
	if err != nil {
		p.failures++
		if p.failures >= maxConsecutiveDevicePollFailures {
			return Credentials{}, true, fmt.Errorf("failed to poll device token: %w", err)
		}
		return Credentials{}, false, nil
	}
	p.failures = 0

	if status.AccessToken != "" {
		exchange, err := p.client.ExchangePlatformToken(ctx, status.AccessToken, p.clientID)
		if err != nil {
			return Credentials{}, true, fmt.Errorf("failed to exchange platform token: %w", err)
		}
		return Credentials{Token: exchange.Token, UserID: exchange.UserId}, true, nil
	}

	switch status.Error {
	case "", "authorization_pending":
		return Credentials{}, false, nil
	case "slow_down":
		p.interval += 5 * time.Second
		p.ticker.Reset(p.interval)
		return Credentials{}, false, nil
	default:
		return Credentials{}, true, &deviceTokenRefusal{code: status.Error, description: status.ErrorDescription}
	}
}
