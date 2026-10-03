package cmdutil

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/pflag"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// errPrivateFlagRetired explains why --private is refused rather than mapped:
// it used to let signed-in users invoke the function, which is what
// authenticated means now, so neither level is a safe guess.
var errPrivateFlagRetired = errors.New("--private is no longer accepted because the levels changed: " +
	"use --visibility private for service keys and schedulers only, " +
	"or --visibility authenticated to also let your project's signed-in users in")

// VisibilityFlags holds --visibility, its --public alias, and the retired --private.
type VisibilityFlags struct {
	value   string
	public  bool
	private bool
}

// Register adds the flags. usage describes --visibility for the command.
func (f *VisibilityFlags) Register(flags *pflag.FlagSet, usage string) {
	flags.StringVar(&f.value, "visibility", "", usage)
	flags.BoolVar(&f.public, "public", false, "Same as --visibility public")
	flags.BoolVar(&f.private, "private", false, "Retired; use --visibility private or --visibility authenticated")
	_ = flags.MarkHidden("private")
}

// Set reports whether any of the flags was given.
func (f *VisibilityFlags) Set() bool {
	return f.value != "" || f.public || f.private
}

// Visibility returns the level the flags select. ok is false when none was given.
func (f *VisibilityFlags) Visibility() (visibility apiclient.FunctionVisibility, ok bool, err error) {
	if f.private {
		return "", false, errPrivateFlagRetired
	}
	if f.public {
		if f.value != "" {
			return "", false, errors.New("--public is the same as --visibility public; use one or the other")
		}
		return apiclient.FunctionVisibilityPublic, true, nil
	}
	if f.value == "" {
		return "", false, nil
	}
	visibility = apiclient.FunctionVisibility(strings.ToLower(strings.TrimSpace(f.value)))
	switch visibility {
	case apiclient.FunctionVisibilityPrivate, apiclient.FunctionVisibilityAuthenticated, apiclient.FunctionVisibilityPublic:
		return visibility, true, nil
	default:
		return "", false, fmt.Errorf("invalid --visibility %q: use private, authenticated, or public", f.value)
	}
}

// PrivateHint is what a deploy prints about the new functions that came up
// private. A deploy does not apply the manifest's visibility, so a function
// the manifest gives another level is pointed at config deploy, and one the
// manifest keeps private gets no hint.
type PrivateHint struct {
	// Summary says who can call a private function of this kind.
	Summary string
	// Instruction introduces the commands Open returns.
	Instruction string
	// Open returns the command that lets signed-in users call the function.
	Open         func(name string) string
	ConfigDeploy string
	// Declared is the level volcano-config.yaml declares for each function.
	Declared map[string]string
}

// Print writes the hint for names, the new functions that came up private.
func (h PrivateHint) Print(out io.Writer, names []string) {
	var declared, undeclared []string
	for _, name := range names {
		switch h.Declared[name] {
		case "":
			undeclared = append(undeclared, name)
		case string(apiclient.FunctionVisibilityPrivate):
		default:
			declared = append(declared, name)
		}
	}
	if len(declared) == 0 && len(undeclared) == 0 {
		return
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, h.Summary)
	if len(declared) > 0 {
		fmt.Fprintf(out, "volcano-config.yaml declares a visibility for %s; apply it with:\n  %s\n",
			strings.Join(declared, ", "), h.ConfigDeploy)
	}
	if len(undeclared) == 0 {
		return
	}
	fmt.Fprintln(out, h.Instruction)
	for _, name := range undeclared {
		fmt.Fprintf(out, "  %s\n", h.Open(name))
	}
	fmt.Fprintf(out, "Or declare its visibility in volcano-config.yaml and run %s\n", h.ConfigDeploy)
}
