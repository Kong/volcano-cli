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

	// DefaultPage is the first page fetched by paginated API callers.
	DefaultPage = 1
	// DefaultLimit is the default number of resources fetched per page.
	DefaultLimit = 100
)

type options struct {
	httpClient apiclient.HttpRequestDoer
}

// Option configures a cloud API client.
type Option func(*options)

// WithHTTPClient injects the HTTP client used by the generated API client.
func WithHTTPClient(httpClient apiclient.HttpRequestDoer) Option {
	return func(opts *options) {
		opts.httpClient = httpClient
	}
}

// Client is a thin wrapper around the generated OpenAPI client.
type Client struct {
	client *apiclient.ClientWithResponses
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

	cfg := options{
		httpClient: &http.Client{Timeout: defaultTimeout},
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	clientOpts := []apiclient.ClientOption{
		apiclient.WithHTTPClient(cfg.httpClient),
		apiclient.WithRequestEditorFn(authorizationEditor(token)),
	}
	generated, err := apiclient.NewClientWithResponses(generatedClientBaseURL(parsed), clientOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create api client: %w", err)
	}
	return &Client{client: generated}, nil
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
