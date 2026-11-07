package project

import (
	"context"
	"fmt"
	"io"

	"github.com/Kong/volcano-cli/internal/fork"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// createFromOptions are the settings of `projects create --from`.
type createFromOptions struct {
	deps   cliruntime.Deps
	name   string
	link   string
	vars   []string
	yes    bool
	noWait bool
	in     io.Reader
	out    io.Writer
}

// checkFromFlags refuses settings that only mean something with --from, so a
// misplaced flag fails instead of being ignored.
func checkFromFlags(from string, vars []string, yes, noWait bool) error {
	if from != "" {
		return nil
	}
	for _, flag := range []struct {
		name string
		set  bool
	}{{"--var", len(vars) > 0}, {"--yes", yes}, {"--no-wait", noWait}} {
		if flag.set {
			return fmt.Errorf("%s only applies with --from", flag.name)
		}
	}
	return nil
}

// runCreateFrom forks a shared project under the name given, the way volcano
// fork does, without making the copy the active project.
func runCreateFrom(ctx context.Context, opts createFromOptions) error {
	return fork.Run(ctx, fork.Options{
		Deps:   opts.deps,
		Name:   opts.name,
		Link:   opts.link,
		Vars:   opts.vars,
		Yes:    opts.yes,
		NoWait: opts.noWait,
		In:     opts.in,
		Out:    opts.out,
		Stdout: opts.out,
	})
}
