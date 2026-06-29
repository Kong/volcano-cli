// Package auth implements the device-code login flow against the cloud API.
package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
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

// Signup runs the device login flow but opens Volcano Web's signup page first.
func (s Service) Signup(ctx context.Context, cfg *config.Config, email string, w io.Writer) (Credentials, error) {
	apiURL := s.apiURL(cfg)
	clientID, err := resolveDeviceClientID(apiURL)
	if err != nil {
		return Credentials{}, err
	}
	// Validate the web config before allocating a device code so a misconfigured
	// VOLCANO_WEB_URL fails fast instead of burning a device authorization.
	if _, err := api.WebSignupURL(cfg.WebURL(), email, ""); err != nil {
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

	devicePath := "/device"
	if userCode := strings.TrimSpace(deviceAuth.UserCode); userCode != "" {
		devicePath = "/device?" + url.Values{"user_code": []string{userCode}}.Encode()
	}
	signupURL, err := api.WebSignupURL(cfg.WebURL(), email, devicePath)
	if err != nil {
		return Credentials{}, err
	}

	fmt.Fprintln(w, "\nInitiating browser signup...")
	return s.completeBrowserLogin(ctx, client, clientID, deviceAuth, w, signupURL)
}

// LoginWithBrowser runs the OAuth device flow and returns credentials to persist.
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
	u, err := url.Parse(strings.TrimSpace(apiURL))
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return true
	}
	return false
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
