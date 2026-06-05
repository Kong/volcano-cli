package local

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	localmodecore "github.com/Kong/volcano-cli/internal/localmode"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type resetOptions struct {
	deps cliruntime.Deps
	yes  bool
	in   io.Reader
	out  io.Writer
}

func newReset(deps cliruntime.Deps) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Reset local development data in-place",
		Long: `WARNING: This will delete ALL local database data.

This command:
1. Drops all local project databases
2. Clears local platform data
3. Re-provisions default local metadata
4. Keeps containers running

After reset, redeploy your project migrations with:
  volcano local migrations deploy --all -d app`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runReset(cmd.Context(), resetOptions{
				deps: deps,
				yes:  yes,
				in:   cmd.InOrStdin(),
				out:  cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}

func runReset(ctx context.Context, opts resetOptions) error {
	if !opts.yes {
		confirmed, err := confirmReset(opts.in, opts.out)
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	}

	return localmodecore.NewService(opts.deps).Reset(ctx, opts.out)
}

func confirmReset(r io.Reader, w io.Writer) (bool, error) {
	fmt.Fprintln(w, "WARNING: This will DELETE ALL LOCAL DATABASE DATA!")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "This will:")
	fmt.Fprintln(w, "  - Drop all local project databases")
	fmt.Fprintln(w, "  - Clear all platform tables in the volcano database")
	fmt.Fprintln(w, "  - Recreate default local metadata and the app database")
	fmt.Fprintln(w, "  - Keep containers running")
	fmt.Fprintln(w)
	fmt.Fprint(w, "Type 'y' or 'yes' to continue: ")

	input, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}

	normalized := strings.ToLower(strings.TrimSpace(input))
	confirmed := normalized == "y" || normalized == "yes"
	if !confirmed {
		fmt.Fprintln(w, "Reset cancelled.")
	}
	return confirmed, nil
}
