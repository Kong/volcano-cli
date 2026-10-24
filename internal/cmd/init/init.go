// Package initcmd wires the volcano init command.
package initcmd

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/projectinit"
)

type options struct {
	force   bool
	example string
	starter string
	out     io.Writer
}

// New returns the init command.
func New() *cobra.Command {
	var force bool
	var example string
	cmd := &cobra.Command{
		Use:   "init [template]",
		Short: "Create local Volcano project scaffold",
		Long: `Create Volcano project scaffold files in the current directory.

The base scaffold creates a volcano/ directory with environment files,
a migrations directory, and a README. The selected template adds
language-specific function files (and a volcano-config.yaml for JavaScript).
Existing files are left unchanged when their contents still match the
selected template.

Templates:
  volcano init                 Create base scaffold only (no language template)
  volcano init nextjs          Base scaffold plus a minimal Next.js app
                               (aliases: next, next.js, next-js)
  volcano init javascript      Base scaffold plus JavaScript function and config
                               (aliases: js, node, nodejs)
  volcano init python          Base scaffold plus Python function setup
                               (aliases: py)
  volcano init ruby            Base scaffold plus Ruby function setup
                               (aliases: rb)

Add --example to create an example project for the selected template:
  volcano init nextjs --example notes          Notes app with dashboard and migration
  volcano init javascript --example hello-world
  volcano init python --example hello-world
  volcano init ruby --example hello-world

By default, changed managed files are treated as conflicts so init cannot
accidentally overwrite local work. Use --force to overwrite changed managed
scaffold files with the current templates.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			starter := ""
			if len(args) > 0 {
				starter = args[0]
			}
			return run(options{
				force:   force,
				example: example,
				starter: starter,
				out:     cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Overwrite changed managed scaffold files")
	cmd.Flags().StringVar(&example, "example", "", "Create an example project for the selected template")
	return cmd
}

func run(opts options) error {
	starter, err := ResolveStarter(opts.starter, opts.example)
	if err != nil {
		return err
	}
	result, err := projectinit.RunStarter(starter, opts.force)
	if err != nil {
		if strings.Contains(err.Error(), "unknown starter") {
			return fmt.Errorf("unknown template %q (supported: nextjs, javascript, python, ruby)", opts.starter)
		}
		if conflicts, ok := projectinit.ConflictMessages(err); ok {
			printConflicts(opts.out, conflicts, projectinit.ConflictsCanBeForced(err))
		}
		return err
	}

	printResult(opts.out, result, starter)
	return nil
}

// ResolveStarter turns a template name or alias, and an optional example, into
// the starter name projectinit takes. Empty means the base scaffold only.
func ResolveStarter(raw, example string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	example = normalizeStarterName(example)
	if value == "" {
		if example != "" {
			return "", errors.New("--example requires a template (supported: nextjs, javascript, python, ruby)")
		}
		return "", nil
	}

	starter := normalizeStarterName(value)
	if alias, ok := starterAliases[starter]; ok {
		starter = alias
	}
	if example != "" {
		starter += "-" + example
	}
	return starter, nil
}

func normalizeStarterName(value string) string {
	return strings.Trim(strings.ReplaceAll(strings.ToLower(strings.TrimSpace(value)), "_", "-"), "-")
}

var starterAliases = map[string]string{
	"next":       "nextjs",
	"next.js":    "nextjs",
	"next-js":    "nextjs",
	"nextjs":     "nextjs",
	"js":         "javascript",
	"javascript": "javascript",
	"node":       "javascript",
	"nodejs":     "javascript",
	"py":         "python",
	"python":     "python",
	"rb":         "ruby",
	"ruby":       "ruby",
}

func printConflicts(w io.Writer, conflicts []string, canForce bool) {
	fmt.Fprintln(w, "Volcano init found conflicting managed paths.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Conflicts:")
	for _, conflict := range conflicts {
		fmt.Fprintf(w, "  - %s\n", conflict)
	}
	fmt.Fprintln(w)
	if canForce {
		fmt.Fprintln(w, "Re-run with --force to overwrite changed managed files.")
		return
	}
	fmt.Fprintln(w, "Remove or rename incompatible paths, then run init again.")
}

func printResult(w io.Writer, result *projectinit.Result, starter string) {
	fmt.Fprintln(w, "Volcano project initialized.")
	printList(w, "Created", result.Created())
	printList(w, "Unchanged", result.Unchanged())
	printList(w, "Overwritten", result.Overwritten())

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Next steps:")
	fmt.Fprintln(w, "  1. Run: volcano start")
	fmt.Fprintln(w, "  2. Run: volcano deploy --with-migrations")
	if strings.HasPrefix(starter, "nextjs") {
		fmt.Fprintln(w, "  3. Open the frontend URL the deploy prints")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "To edit with hot reload:")
		fmt.Fprintln(w, "  - Run: npm install && npm run dev    (or: yarn install && yarn dev)")
		fmt.Fprintln(w, "  - Open: http://localhost:3000")
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Cloud deployment:")
	fmt.Fprintln(w, "  - Run: volcano login    (new to Volcano? run: volcano signup)")
	fmt.Fprintln(w, "  - Run: volcano use <project-id-or-name>")
	fmt.Fprintln(w, "  - Put the cloud project's variables in volcano/cloud.env (volcano/volcano.env holds the local server's)")
	if resultHasMigrations(result) {
		fmt.Fprintln(w, "  - Run: volcano cloud deploy --env-file volcano/cloud.env --with-migrations -d <database>")
	} else {
		fmt.Fprintln(w, "  - Run: volcano cloud deploy --env-file volcano/cloud.env")
	}
}

func resultHasMigrations(result *projectinit.Result) bool {
	if result == nil {
		return false
	}
	for _, paths := range [][]string{result.Created(), result.Unchanged(), result.Overwritten()} {
		for _, path := range paths {
			if strings.HasPrefix(path, "volcano/migrations/") && strings.HasSuffix(path, ".sql") {
				return true
			}
		}
	}
	return false
}

func printList(w io.Writer, label string, paths []string) {
	if len(paths) == 0 {
		return
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "%s:\n", label)
	for _, path := range paths {
		fmt.Fprintf(w, "  - %s\n", path)
	}
}
