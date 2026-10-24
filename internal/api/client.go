package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

const (
	defaultTimeout = 60 * time.Second
	// functionDeployTimeout outlasts the local-mode server, which answers a
	// deploy only after waiting for the project lock (up to 30 s) and installing
	// the function's dependencies (up to five minutes; functionDepInstallTimeout
	// in volcano-hosting's internal/localmode). Raise it if either bound grows.
	functionDeployTimeout = 6 * time.Minute

	// DefaultPage is the first page fetched by paginated API callers.
	DefaultPage = 1
	// DefaultLimit is the default number of resources fetched per page.
	DefaultLimit = 100
)

type options struct {
	httpClient       apiclient.HttpRequestDoer
	deployHTTPClient apiclient.HttpRequestDoer
	streamHTTPClient apiclient.HttpRequestDoer
}

// Option configures a cloud API client.
type Option func(*options)

// WithHTTPClient injects the HTTP client used by the generated API client.
func WithHTTPClient(httpClient apiclient.HttpRequestDoer) Option {
	return func(opts *options) {
		opts.httpClient = httpClient
		opts.deployHTTPClient = httpClient
		opts.streamHTTPClient = httpClient
	}
}

// Client is a thin wrapper around the generated OpenAPI client.
type Client struct {
	client           *apiclient.ClientWithResponses
	baseURL          string
	token            string
	streamHTTPClient apiclient.HttpRequestDoer
	// deployClient sends function source uploads, which can outlast defaultTimeout.
	deployClient *apiclient.ClientWithResponses
}

// NewClient constructs a generated client with auth and error-normalization helpers.
func NewClient(apiURL, token string, opts ...Option) (*Client, error) {
	apiURL = strings.TrimRight(strings.TrimSpace(apiURL), "/")
	if apiURL == "" {
		return nil, errors.New("api url cannot be empty")
	}
	parsed, err := url.Parse(apiURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse api url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("api url must use http:// or https:// scheme")
	}

	cfg := options{}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.httpClient == nil {
		cfg.httpClient = &http.Client{Timeout: defaultTimeout}
	}
	if cfg.deployHTTPClient == nil {
		cfg.deployHTTPClient = &http.Client{Timeout: functionDeployTimeout}
	}
	if cfg.streamHTTPClient == nil {
		cfg.streamHTTPClient = &http.Client{}
	}

	baseURL := generatedClientBaseURL(parsed)
	generated, err := newGeneratedClient(baseURL, token, wrapDoer(apiURL, cfg.httpClient))
	if err != nil {
		return nil, err
	}
	deployGenerated, err := newGeneratedClient(baseURL, token, wrapDoer(apiURL, cfg.deployHTTPClient))
	if err != nil {
		return nil, err
	}
	return &Client{
		client:           generated,
		baseURL:          baseURL,
		token:            token,
		streamHTTPClient: wrapDoer(apiURL, cfg.streamHTTPClient),
		deployClient:     deployGenerated,
	}, nil
}

// WrapDoer returns doer with the behavior every API request shares, for a
// request made outside the generated client: the CLI version protocol
// (version and agent headers, and the response's instruction headers recorded
// for LastInstructions, such as a reauth or upgrade notice), --debug tracing,
// refusal recording, and the message naming apiURL when the server cannot be
// reached.
func WrapDoer(apiURL string, doer apiclient.HttpRequestDoer) apiclient.HttpRequestDoer {
	return wrapDoer(strings.TrimRight(strings.TrimSpace(apiURL), "/"), doer)
}

// wrapDoer applies the behavior every API request shares to one transport.
func wrapDoer(apiURL string, doer apiclient.HttpRequestDoer) apiclient.HttpRequestDoer {
	// Wrap with the VOL-180 version protocol: every request (including
	// unauthenticated ones, e.g. device/token exchange) reports this CLI's
	// version/identity, and every response's instruction headers are recorded
	// for LastInstructions regardless of which call observed them. debugDoer
	// sits closest to the wire (inside the version-protocol wrapper) so its trace
	// reflects the final request, including the version headers and any
	// Authorization header. It's a cheap passthrough unless VOLCANO_DEBUG / the
	// --debug flag turned tracing on.
	doer = versionProtocolDoer{next: debugDoer{next: doer}}

	// Record what a refused request carried, so a caller explaining a 403 can
	// describe the request rather than re-derive it from configuration that may
	// have chosen a different project (see Refusal).
	doer = refusalDoer{next: doer}

	// Outermost wrapper: rewrite opaque transport failures (connection refused,
	// DNS, timeout) into an actionable message naming the API URL. Wraps the
	// whole chain so it sees the final transport error from any request.
	return networkErrorDoer{apiURL: apiURL, next: doer}
}

func newGeneratedClient(baseURL, token string, doer apiclient.HttpRequestDoer) (*apiclient.ClientWithResponses, error) {
	generated, err := apiclient.NewClientWithResponses(baseURL,
		apiclient.WithHTTPClient(doer),
		apiclient.WithRequestEditorFn(authorizationEditor(token)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create api client: %w", err)
	}
	return generated, nil
}

func generatedClientBaseURL(parsed *url.URL) string {
	base := *parsed
	base.Path = strings.TrimRight(base.Path, "/") + "/"
	return base.String()
}

func authorizationEditor(token string) apiclient.RequestEditorFn {
	return func(_ context.Context, req *http.Request) error {
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		return nil
	}
}
