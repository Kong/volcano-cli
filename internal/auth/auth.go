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

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/config"
	"github.com/Kong/volcano-cli/internal/localmode"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clisession "github.com/Kong/volcano-cli/internal/session"
)

const maxConsecutiveDevicePollFailures = 3

// Credentials are the platform credentials produced by a successful login.
type Credentials struct {
	Token  string
	UserID string
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

// LoginWithToken validates token and returns credentials to persist.
func (s Service) LoginWithToken(ctx context.Context, cfg *config.Config, token string) (Credentials, error) {
	token = strings.TrimSpace(token)
	client, err := s.sessions.APIClient(s.apiURL(cfg), token)
	if err != nil {
		return Credentials{}, err
	}

	if err := client.ValidateToken(ctx); err != nil {
		if api.Status(err) == http.StatusUnauthorized {
			return Credentials{}, errors.New("invalid token")
		}
		return Credentials{}, fmt.Errorf("failed to validate token: %w", err)
	}

	return Credentials{Token: token}, nil
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
