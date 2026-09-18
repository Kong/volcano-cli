// Package auth implements the device-code login flow against the cloud API.
package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	// projectFromEnvironment is VOLCANO_PROJECT_ID.
	projectFromEnvironment projectOrigin = "VOLCANO_PROJECT_ID"
	// projectFromSelection is the project a previous `volcano use` saved, which
	// belongs to whatever credential was logged in before this one.
	projectFromSelection projectOrigin = "the project 'volcano use' last selected"
)

// LoginWithToken validates token and returns credentials to persist. project
// names the project to validate against and select, by ID or by name; it is
// optional for an account token and required for a project access token, which
// cannot list projects and so cannot tell the CLI which project it belongs to.
func (s Service) LoginWithToken(ctx context.Context, cfg *config.Config, token, project string) (Credentials, error) {
	token = strings.TrimSpace(token)
	project = strings.TrimSpace(project)
	client, err := s.sessions.APIClient(s.apiURL(cfg), token)
	if err != nil {
		return Credentials{}, err
	}

	origin := projectNamed
	if project == "" && config.IsProjectToken(token) {
		project, origin = unnamedProject(cfg)
		if project == "" {
			return Credentials{}, fmt.Errorf("a project access token (%s) is scoped to one project: pass --project <project-id> "+
				"with the project it was minted in, or set VOLCANO_PROJECT_ID", config.ProjectTokenPrefix)
		}
	}
	if project == "" {
		if err := client.ValidateToken(ctx); err != nil {
			if api.Status(err) == http.StatusUnauthorized {
				return Credentials{}, errors.New("invalid token")
			}
			return Credentials{}, fmt.Errorf("failed to validate token: %w", err)
		}
		return Credentials{Token: token}, nil
	}

	selected, err := resolveLoginProject(ctx, client, token, project, origin)
	if err != nil {
		return Credentials{}, err
	}
	return Credentials{
		Token:   token,
		Project: &config.ProjectConfig{ID: selected.Id.String(), Name: selected.Name},
	}, nil
}

// unnamedProject falls back to the project the CLI already has for a project
// access token logged in without --project, and reports where it came from.
func unnamedProject(cfg *config.Config) (string, projectOrigin) {
	if fromEnv := strings.TrimSpace(cfg.ProjectIDFromEnv()); fromEnv != "" {
		return fromEnv, projectFromEnvironment
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
		return errors.New("invalid token")
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
	return s.completeBrowserLogin(ctx, client, clientID, deviceAuth, w, signupURL)
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
	return s.completeBrowserLogin(ctx, client, clientID, deviceAuth, w, verificationURL)
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

	authTimeout := time.Duration(deviceAuth.ExpiresIn) * time.Second
	timeout := cliruntime.NewTimer(s.deps, authTimeout)
	defer timeout.Stop()
	pollInterval := time.Duration(deviceAuth.Interval) * time.Second
	if pollInterval <= 0 {
		pollInterval = 5 * time.Second
	}
	pollTicker := cliruntime.NewTicker(s.deps, pollInterval)
	defer pollTicker.Stop()
	dotTicker := cliruntime.NewTicker(s.deps, time.Second)
	defer dotTicker.Stop()

	consecutivePollFailures := 0
	for {
		select {
		case <-ctx.Done():
			fmt.Fprintln(w)
			return Credentials{}, ctx.Err()
		case <-timeout.C():
			fmt.Fprintln(w)
			if authTimeout <= 0 {
				return Credentials{}, errors.New("authentication timeout")
			}
			return Credentials{}, fmt.Errorf("authentication timeout (%s)", authTimeout.Round(time.Second))
		case <-dotTicker.C():
			fmt.Fprint(w, ".")
		case <-pollTicker.C():
			status, err := client.PollDeviceToken(ctx, clientID, deviceAuth.DeviceCode)
			if err != nil {
				consecutivePollFailures++
				if consecutivePollFailures >= maxConsecutiveDevicePollFailures {
					fmt.Fprintln(w)
					return Credentials{}, fmt.Errorf("failed to poll device token: %w", err)
				}
				continue
			}
			consecutivePollFailures = 0

			if status.AccessToken != "" {
				exchange, err := client.ExchangePlatformToken(ctx, status.AccessToken, clientID)
				if err != nil {
					fmt.Fprintln(w)
					return Credentials{}, fmt.Errorf("failed to exchange platform token: %w", err)
				}
				fmt.Fprintln(w)
				return Credentials{Token: exchange.Token, UserID: exchange.UserId}, nil
			}

			switch status.Error {
			case "authorization_pending":
				continue
			case "slow_down":
				pollInterval += 5 * time.Second
				pollTicker.Reset(pollInterval)
				continue
			case "access_denied":
				fmt.Fprintln(w)
				return Credentials{}, errors.New("authorization denied")
			case "expired_token":
				fmt.Fprintln(w)
				return Credentials{}, errors.New("device code expired")
			case "":
				continue
			default:
				fmt.Fprintln(w)
				if status.ErrorDescription != "" {
					return Credentials{}, errors.New(status.ErrorDescription)
				}
				return Credentials{}, fmt.Errorf("authentication failed: %s", status.Error)
			}
		}
	}
}
