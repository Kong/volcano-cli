package cmdutil

import (
	"errors"
	"fmt"
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
