package localmode

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	composeProjectName = "volcano"
	redisContainerName = "volcano-redis"
	defaultLocalAPIURL = "http://localhost:8000"
	postgresAddress    = "localhost:8002"

	defaultWaitTimeout = 60 * time.Second
	defaultPoll        = time.Second
	defaultInfoTimeout = 10 * time.Second
	healthTimeout      = 2 * time.Second
)

// defaultVolcanoImage is the local-mode server image used when the user sets no
// --image / VOLCANO_IMAGE / .env.local override. It is a var, not a const, so it
// can be overridden at build time via -X (Makefile DEFAULT_LOCAL_IMAGE). It
// defaults to kong/volcano:local-nightly, the local-mode image volcano-hosting
// publishes.
var defaultVolcanoImage = "kong/volcano:local-nightly"

// Service performs local-mode environment workflows.
type Service struct {
	runner        DockerRunner
	healthClient  apiclient.HttpRequestDoer
	apiHTTPClient apiclient.HttpRequestDoer
	healthURL     string
	waitTimeout   time.Duration
	pollInterval  time.Duration
	infoTimeout   time.Duration
	dialTCP       func(context.Context, string) error
	environ       func() []string
	getenv        func(string) string
	tempDir       string
	image         string
}

// Option configures a Service.
type Option func(*Service)

// WithDockerRunner replaces the Docker command runner.
func WithDockerRunner(runner DockerRunner) Option {
	return func(s *Service) {
		if runner != nil {
			s.runner = runner
		}
	}
}

// WithHealthURL replaces the health URL used while waiting for startup.
func WithHealthURL(rawURL string) Option {
	return func(s *Service) {
		if strings.TrimSpace(rawURL) != "" {
			s.healthURL = strings.TrimRight(strings.TrimSpace(rawURL), "/") + "/health"
		}
	}
}

// WithTiming replaces local-mode wait durations.
func WithTiming(waitTimeout, pollInterval, infoTimeout time.Duration) Option {
	return func(s *Service) {
		if waitTimeout > 0 {
			s.waitTimeout = waitTimeout
		}
		if pollInterval > 0 {
			s.pollInterval = pollInterval
		}
		if infoTimeout > 0 {
			s.infoTimeout = infoTimeout
		}
	}
}

// WithDialTCP replaces TCP service probing.
func WithDialTCP(dialTCP func(context.Context, string) error) Option {
	return func(s *Service) {
		if dialTCP != nil {
			s.dialTCP = dialTCP
		}
	}
}

// WithEnvironment replaces process environment access.
func WithEnvironment(environ func() []string, getenv func(string) string) Option {
	return func(s *Service) {
		if environ != nil {
			s.environ = environ
		}
		if getenv != nil {
			s.getenv = getenv
		}
	}
}

// WithTempDir replaces the temporary directory used for the Compose file.
func WithTempDir(tempDir string) Option {
	return func(s *Service) {
		s.tempDir = tempDir
	}
}

// WithImage sets an explicit local-mode server image, taking precedence over the
// VOLCANO_IMAGE environment variable, any project .env.local value, and the
// bundled default. An empty value is ignored. An explicitly selected image is
// never pulled: it must already exist locally (see Start's pre-flight check).
func WithImage(image string) Option {
	return func(s *Service) {
		s.image = strings.TrimSpace(image)
	}
}

// NewService returns a local-mode environment service.
func NewService(deps cliruntime.Deps, opts ...Option) Service {
	healthClient := deps.HTTPClient
	if healthClient == nil {
		healthClient = &http.Client{Timeout: healthTimeout}
	}

	s := Service{
		runner:        newDockerRunner(deps),
		healthClient:  healthClient,
		apiHTTPClient: deps.HTTPClient,
		healthURL:     defaultLocalAPIURL + "/health",
		waitTimeout:   defaultWaitTimeout,
		pollInterval:  defaultPoll,
		infoTimeout:   defaultInfoTimeout,
		dialTCP:       defaultDialTCP,
		environ:       os.Environ,
		getenv:        os.Getenv,
	}
	for _, opt := range opts {
		opt(&s)
	}
	return s
}

// Start starts the local Volcano Docker stack.
func (s Service) Start(ctx context.Context, w io.Writer) error {
	fmt.Fprintln(w, "Starting Volcano local development environment...")
	fmt.Fprintln(w)

	if s.serverRunning(ctx) {
		fmt.Fprintln(w, "Volcano is already running.")
		fmt.Fprintln(w)
		if err := s.waitForServer(ctx, w); err != nil {
			return fmt.Errorf("server is running but not ready: %w", err)
		}
		output.Success(w, "Server is ready")
		info, err := s.prepareRunningServer(ctx, w)
		if err != nil {
			return err
		}
		s.printStatusDetailsWithInfo(ctx, w, info)
		return nil
	}

	if err := s.checkDocker(ctx); err != nil {
		output.LocalModeDockerUnavailable(w)
		return fmt.Errorf("docker not found: %w", err)
	}
	output.Success(w, "Docker is available")

	// When the image is an explicit override (--image / VOLCANO_IMAGE / .env.local)
	// it must already exist locally before we announce or start it. Fail fast here
	// (Restart calls this before tearing the environment down) instead of letting
	// `docker compose up` emit a confusing registry-pull error.
	if err := s.ensureCustomImageAvailable(ctx); err != nil {
		return err
	}

	composeEnv, image, err := s.composeEnvironment()
	if err != nil {
		return err
	}
	_, customImage := s.resolveImage()

	fmt.Fprintf(w, "Using Docker image: %s\n", image)
	if customImage {
		output.Success(w, "Using local image %q (not pulled)", image)
	}

	if err := s.startDockerServices(ctx, w, composeEnv); err != nil {
		return fmt.Errorf("failed to start Docker services: %w", err)
	}
	output.Success(w, "Docker services started")

	if err := s.waitForServer(ctx, w); err != nil {
		s.printContainerLogs(ctx, w, serverContainerName, 200)
		if downErr := s.composeDown(ctx, false); downErr != nil {
			output.Warning(w, "failed to stop Docker services after startup failure: %v", downErr)
		}
		return fmt.Errorf("server failed to start: %w", err)
	}
	output.Success(w, "Server is ready")

	info, err := s.prepareRunningServer(ctx, w)
	if err != nil {
		return err
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Volcano is ready for local development.")
	fmt.Fprintln(w)
	s.printStatusDetailsWithInfo(ctx, w, info)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Run 'volcano stop' when you're done.")
	return nil
}

func (s Service) prepareRunningServer(ctx context.Context, w io.Writer) (Info, error) {
	info, err := s.fetchInfo(ctx)
	if err != nil {
		return Info{}, fmt.Errorf("failed to fetch local metadata: %w", err)
	}
	if err := s.ensureDefaultDatabase(ctx, info); err != nil {
		return Info{}, fmt.Errorf("failed to create default database: %w", err)
	}
	output.Success(w, "Default database '%s' ready", info.DefaultDatabaseName)

	if err := saveDevState(info); err != nil {
		return Info{}, fmt.Errorf("failed to save dev state: %w", err)
	}
	output.Success(w, "Dev state saved")
	return info, nil
}

// Status displays the local Volcano Docker stack status.
func (s Service) Status(ctx context.Context, w io.Writer) error {
	if !s.serverRunning(ctx) {
		output.LocalModeNotRunning(w)
		return nil
	}
	return s.printStatusDetails(ctx, w)
}

// Stop stops the local Volcano Docker stack.
func (s Service) Stop(ctx context.Context, w io.Writer, clean bool) error {
	fmt.Fprintln(w, "Stopping Volcano local development environment...")
	fmt.Fprintln(w)

	if !s.serverRunning(ctx) {
		if !clean && !s.composeProjectHasContainers(ctx) {
			fmt.Fprintln(w, "Volcano is not running")
			return nil
		}
	}

	fmt.Fprintln(w, "Stopping Docker services...")
	if err := s.composeDown(ctx, clean); err != nil {
		return fmt.Errorf("failed to stop Docker services: %w", err)
	}

	if clean {
		output.Success(w, "All Docker services stopped and data removed")
		if err := deleteDevState(); err != nil {
			return fmt.Errorf("failed to delete dev state: %w", err)
		}
	} else {
		output.Success(w, "All Docker services stopped (data volumes preserved)")
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Volcano stopped successfully")
	return nil
}

// Restart restarts the local Volcano Docker stack while preserving data.
func (s Service) Restart(ctx context.Context, w io.Writer) error {
	// Validate a custom image before tearing the environment down, so a bad
	// --image leaves the running stack intact instead of stopped.
	if err := s.ensureCustomImageAvailable(ctx); err != nil {
		return err
	}
	if err := s.Stop(ctx, w, false); err != nil {
		return err
	}
	fmt.Fprintln(w)
	return s.Start(ctx, w)
}
