package root

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/theme"
)

// applyHelpTheme colorizes Cobra's usage/help output to match the rest of the
// CLI: section headings in lava, command names in flame. Set on the root command
// only; Cobra walks up to the root's usage template, so every subcommand's help
// inherits it.
//
// Coloring keys off theme.On(os.Stdout) rather than the command's writer:
// Cobra's HelpTemplate renders the usage section through UsageString, which
// buffers to an in-memory writer, so the destination writer isn't visible at
// render time. os.Stdout is where help actually lands; under `go test` it isn't
// a TTY, so captured help stays plain and Contains-based tests are unaffected.
// ponytail: usage funcs read a process-wide gate; fine for a synchronous CLI,
// revisit only if help is ever rendered concurrently to mixed destinations.
func applyHelpTheme(root *cobra.Command) {
	on := func() bool { return theme.On(os.Stdout) }
	cobra.AddTemplateFunc("vHeading", func(s string) string { return theme.Title(s, on()) })
	cobra.AddTemplateFunc("vName", func(s string) string { return theme.Command(s, on()) })
	root.SetUsageTemplate(usageTemplate)
}

// usageTemplate is Cobra's default usage template with section headings wrapped
// in vHeading and command names (padded first) wrapped in vName. Flag blocks and
// the footer stay plain — coloring multi-line flag usage adds no signal.
const usageTemplate = `{{vHeading "Usage:"}}{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

{{vHeading "Aliases:"}}
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

{{vHeading "Examples:"}}
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

{{vHeading "Available Commands:"}}{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{vName (rpad .Name .NamePadding)}} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{vHeading .Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{vName (rpad .Name .NamePadding)}} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

{{vHeading "Additional Commands:"}}{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{vName (rpad .Name .NamePadding)}} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

{{vHeading "Flags:"}}
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

{{vHeading "Global Flags:"}}
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

{{vHeading "Additional help topics:"}}{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{vName (rpad .CommandPath .CommandPathPadding)}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} [command] --help" for more information about a command.{{end}}
`
