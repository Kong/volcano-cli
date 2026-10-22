package callbacks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/cmd/cmdutil"
	clidurable "github.com/Kong/volcano-cli/internal/durable"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type succeedOptions struct {
	deps       cliruntime.Deps
	token      string
	result     json.RawMessage
	jsonOutput bool
	out        io.Writer
}

func newSucceed(deps cliruntime.Deps) *cobra.Command {
	opts := succeedOptions{}
	var result, resultFile string
	cmd := &cobra.Command{
		Use:   "succeed <token>",
		Short: "Resume a waiting step with a result",
		Long: `Succeed one callback. The step waiting on the token resumes, and
ctx.waitForCallback returns the result.

--result is the result's JSON value, and --result-file reads it from a file.
One of them is required. The result can be any JSON value other than null, up
to 64 KiB: a wait that times out resolves with null, so null is refused.

The command returns once the execution has the result. If handing it over takes
longer than the request waits, the command says Volcano is still delivering it;
Volcano finishes on its own, so do not send it again.

--json prints the outcome as the API returns it.`,
		Example: fmt.Sprintf(`  %s
  %s`,
			cliruntime.CommandPath(deps,
				`durable callbacks succeed vcb_0000000000000000000000000000000000000000000 --result '{"charge_id":"ch_1"}'`),
			"printf '%s' \"$CALLBACK_TOKEN\" | "+cliruntime.CommandPath(deps,
				"durable callbacks succeed - --result-file charge.json")),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var err error
			opts.result, err = resultValue(cmd, result, resultFile)
			if err != nil {
				return err
			}
			opts.token, err = readToken(args[0], cmd.InOrStdin())
			if err != nil {
				return err
			}
			opts.deps = deps
			opts.out = cmd.OutOrStdout()
			return runSucceed(cmd.Context(), opts)
		},
	}
	cmd.Flags().StringVar(&result, "result", "", "The result's JSON value")
	cmd.Flags().StringVar(&resultFile, "result-file", "", "Path to a file holding the result's JSON value")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	cmd.MarkFlagsMutuallyExclusive("result", "result-file")
	cmd.MarkFlagsOneRequired("result", "result-file")
	return cmd
}

// resultValue reads the flag that carries the result and refuses null here,
// where the message can say why.
func resultValue(cmd *cobra.Command, result, resultFile string) (json.RawMessage, error) {
	var value json.RawMessage
	var err error
	if cmd.Flags().Changed("result") {
		value, err = cmdutil.ParseJSONValue("result", []byte(result))
	} else {
		if strings.TrimSpace(resultFile) == "" {
			return nil, errors.New("--result-file needs a path")
		}
		var contents []byte
		contents, err = cmdutil.ReadJSONFile("result", resultFile)
		if err != nil {
			return nil, err
		}
		value, err = cmdutil.ParseJSONValue("result", contents)
	}
	if err != nil {
		return nil, err
	}
	if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return nil, errors.New("result cannot be null: a wait that times out resolves with null, " +
			"so the workflow could not tell the two apart")
	}
	return value, nil
}

func runSucceed(ctx context.Context, opts succeedOptions) error {
	outcome, err := clidurable.NewService(opts.deps).SucceedCallback(ctx, opts.token, opts.result)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return writeJSON(opts.out, outcome)
	}
	reportAnswer(opts.out, outcome, "Succeeded the callback; the execution resumes with the result")
	return nil
}
