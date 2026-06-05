package localmode

import "context"

func (s Service) fetchInfo(ctx context.Context) (Info, error) {
	ctx, cancel := context.WithTimeout(ctx, s.infoTimeout)
	defer cancel()

	return FetchInfo(ctx, localModeCommandRunner{runner: s.runner})
}

type localModeCommandRunner struct {
	runner DockerRunner
}

func (r localModeCommandRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return r.runner.Run(ctx, Command{Name: name, Args: append([]string{}, args...)})
}
