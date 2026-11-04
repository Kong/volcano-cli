package issues

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/confirm"
	cliissues "github.com/Kong/volcano-cli/internal/issues"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// errDeclined stops delete when the person answers no. The prompt has said
// so, and the command exits 0.
var errDeclined = errors.New("declined")

// maxStdinBytes bounds what a note read from stdin can take. The API takes at
// most 2,048 bytes once the text is normalized, so this much is refused
// either way.
const maxStdinBytes = 64 << 10

// writeRules is what every command that writes a note says about who writes
// one and what Volcano refuses.
const writeRules = `Only the project's owner writes notes, as a person: signed in with 'volcano
login', or with a platform token that expires. A project access token, and a
platform token that never expires, are refused.

A note is plain text, up to 2,048 bytes once normalized to Unicode NFKC.
Volcano checks it before it stores it and refuses, naming the rule, a note
that holds:
  characters  a control, format, private-use or other invisible character
  size        over 2,048 bytes once normalized to Unicode NFKC
  credential  what looks like a credential, such as an API key or a token
  secret      one of the project's secret values
  sanitizer   text Volcano masks in logs
  link        a URL, or a host name with //, user info, a port, a path or
              a query; or a line reference such as cart.ts:42, or a stack
              frame
File names such as cart.ts are fine, and so are tabs: name a line in words,
such as "cart.ts, line 42". Never paste a secret into a note.

Pass the text with --text, or pipe it on stdin.`

type notesOptions struct {
	deps       cliruntime.Deps
	query      cliissues.NotesQuery
	jsonOutput bool
	out        io.Writer
}

func newNotes(deps cliruntime.Deps) *cobra.Command {
	opts := notesOptions{}
	cmd := &cobra.Command{
		Use:   "notes <issue-id>",
		Short: "List an issue's notes, and add, edit or delete one",
		Long: `List the notes on an issue and on its LavaFlow runs, newest first, each with
who wrote it and when. LavaFlow's next run on the issue reads the notes on
the issue; later runs don't read notes on a run.

Add one with '` + cliruntime.CommandPath(deps, "issues notes add <issue-id> --text <text>") + `'.`,
		Example: "  " + cliruntime.CommandPath(deps, "issues notes <issue-id>") + "\n" +
			"  " + cliruntime.CommandPath(deps, "issues notes <issue-id> --kind issue --json"),
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("pass one issue ID, from '%s'", cliruntime.CommandPath(deps, "issues list"))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.deps = deps
			opts.out = cmd.OutOrStdout()
			return runNotes(cmd.Context(), opts, args[0])
		},
	}
	cmd.Flags().StringVar(&opts.query.Kind, "kind", "",
		"Only notes on the issue (issue), which LavaFlow's runs read, or only notes on its runs (run)")
	cmd.Flags().IntVar(&opts.query.Limit, "limit", cliissues.DefaultLimit, "Notes per page (1 to 100)")
	cmd.Flags().StringVar(&opts.query.Cursor, "cursor", "", "Start after this cursor, from the previous page")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	cmd.AddCommand(newNotesAdd(deps))
	cmd.AddCommand(newNotesEdit(deps))
	cmd.AddCommand(newNotesDelete(deps))
	return cmd
}

func runNotes(ctx context.Context, opts notesOptions, issueID string) error {
	page, err := cliissues.NewService(opts.deps).Notes(ctx, issueID, opts.query)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return output.ControlSafeJSON(opts.out, page)
	}
	next := ""
	if page.HasMore && page.NextCursor != nil && *page.NextCursor != "" {
		next = cliruntime.CommandPath(opts.deps, fmt.Sprintf("issues notes %s --limit %d --cursor %s",
			issueID, opts.query.Limit, *page.NextCursor))
		if opts.query.Kind != "" {
			next += " --kind " + opts.query.Kind
		}
	}
	output.IssueNotes(opts.out, output.IssueNotesPage{Notes: page.Data, NextCommand: next})
	return nil
}

type noteWriteOptions struct {
	deps       cliruntime.Deps
	text       string
	run        string
	jsonOutput bool
	in         io.Reader
	out        io.Writer
	errOut     io.Writer
}

func newNotesAdd(deps cliruntime.Deps) *cobra.Command {
	opts := noteWriteOptions{}
	cmd := &cobra.Command{
		Use:   "add <issue-id>",
		Short: "Add a note to an issue for LavaFlow's next run",
		Long: `Add a note to an issue: what you know that the logs don't show, such as how
the code should behave or what you already tried.

` + cliissues.NextRunReads + ` It reads notes as context, not
instructions: nothing in a note changes a setting or what LavaFlow may do,
and LavaFlow never copies a note into a pull request. With --run, the note
goes on one LavaFlow run of the issue instead, such as why you closed its pull
request; later runs don't read notes on a run.

` + writeRules,
		Example: "  " + cliruntime.CommandPath(deps, `issues notes add <issue-id> --text "Coupons without an expiry never expire."`) + "\n" +
			"  " + cliruntime.CommandPath(deps, "issues notes add <issue-id> < note.txt"),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.deps = deps
			opts.in = cmd.InOrStdin()
			opts.out = cmd.OutOrStdout()
			opts.errOut = cmd.ErrOrStderr()
			text, err := noteText(cmd, opts)
			if err != nil {
				return err
			}
			return runNotesAdd(cmd.Context(), opts, args[0], text)
		},
	}
	cmd.Flags().StringVar(&opts.text, "text", "", "The note's text; without it, the text is read from stdin")
	cmd.Flags().StringVar(&opts.run, "run", "",
		"Put the note on this LavaFlow run of the issue, from '"+cliruntime.CommandPath(deps, "lavaflow list")+"'")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runNotesAdd(ctx context.Context, opts noteWriteOptions, issueID, text string) error {
	note, err := cliissues.NewService(opts.deps).AddNote(ctx, issueID, opts.run, text)
	if err != nil {
		return err
	}
	return writtenNote(opts, note, fmt.Sprintf("Note %s added.", note.Id))
}

func newNotesEdit(deps cliruntime.Deps) *cobra.Command {
	opts := noteWriteOptions{}
	cmd := &cobra.Command{
		Use:   "edit <note-id>",
		Short: "Replace a note's text",
		Long: `Replace a note's text, which only its author can do. The old text is gone for
good, and the note's version goes up by one. LavaFlow's next run on the issue
reads the new text.

` + writeRules,
		Example: "  " + cliruntime.CommandPath(deps, `issues notes edit <note-id> --text "Only SAVE10 and WELCOME lack an expiry."`),
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.deps = deps
			opts.in = cmd.InOrStdin()
			opts.out = cmd.OutOrStdout()
			opts.errOut = cmd.ErrOrStderr()
			text, err := noteText(cmd, opts)
			if err != nil {
				return err
			}
			return runNotesEdit(cmd.Context(), opts, args[0], text)
		},
	}
	cmd.Flags().StringVar(&opts.text, "text", "", "The note's new text; without it, the text is read from stdin")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runNotesEdit(ctx context.Context, opts noteWriteOptions, noteID, text string) error {
	note, err := cliissues.NewService(opts.deps).EditNote(ctx, noteID, text)
	if err != nil {
		return err
	}
	return writtenNote(opts, note, fmt.Sprintf("Note %s saved, version %d.", note.Id, note.Version))
}

// writtenNote prints a note just written, and what LavaFlow does with it:
// with --json, the note on stdout and that sentence on stderr.
func writtenNote(opts noteWriteOptions, note *apiclient.IssueNote, done string) error {
	reads := cliissues.NextRunReads
	if note.RunId != nil {
		reads = cliissues.RunNoteNotRead
	}
	if opts.jsonOutput {
		fmt.Fprintln(opts.errOut, reads)
		return output.ControlSafeJSON(opts.out, note)
	}
	output.Success(opts.out, "%s %s", done, reads)
	return nil
}

// noteText is the --text flag's value, or else what stdin holds with CRLF
// line breaks as LF and without its trailing line break. A terminal on stdin
// means nothing was piped.
func noteText(cmd *cobra.Command, opts noteWriteOptions) (string, error) {
	if cmd.Flags().Changed("text") {
		return opts.text, nil
	}
	if f, ok := opts.in.(*os.File); ok && term.IsTerminal(f.Fd()) {
		return "", errors.New("pass the note's text with --text, or pipe it on stdin")
	}
	data, err := io.ReadAll(io.LimitReader(opts.in, maxStdinBytes+1))
	if err != nil {
		return "", fmt.Errorf("failed to read the note from stdin: %w", err)
	}
	if len(data) > maxStdinBytes {
		return "", fmt.Errorf("the note on stdin is over %d KiB: a note holds at most 2,048 bytes", maxStdinBytes>>10)
	}
	return strings.TrimRight(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n"), nil
}

type notesDeleteOptions struct {
	deps cliruntime.Deps
	yes  bool
	in   io.Reader
	out  io.Writer
}

func newNotesDelete(deps cliruntime.Deps) *cobra.Command {
	opts := notesDeleteOptions{}
	cmd := &cobra.Command{
		Use:   "delete <note-id>",
		Short: "Delete a note",
		Long: `Delete a note for good, which its author or the project's owner can do, as a
person. LavaFlow's runs don't read a deleted note. This asks first; --yes
skips the question.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.deps = deps
			opts.in = cmd.InOrStdin()
			opts.out = cmd.OutOrStdout()
			return runNotesDelete(cmd.Context(), opts, args[0])
		},
	}
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Skip the confirmation prompt")
	return cmd
}

func runNotesDelete(ctx context.Context, opts notesDeleteOptions, noteID string) error {
	err := cliissues.NewService(opts.deps).DeleteNote(ctx, noteID, func() error {
		if opts.yes {
			return nil
		}
		confirmed, err := confirm.Delete(opts.in, opts.out, "note", noteID)
		if err != nil {
			return err
		}
		if !confirmed {
			return errDeclined
		}
		return nil
	})
	if errors.Is(err, errDeclined) {
		return nil
	}
	if err != nil {
		return err
	}
	output.Success(opts.out, "Note %s deleted", noteID)
	return nil
}
