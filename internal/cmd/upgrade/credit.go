package upgrade

import (
	"fmt"
	"os"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/config"
	"github.com/Kong/volcano-cli/internal/confirm"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// CreditPromptSafeAnnotation marks a command as eligible for the interactive
// "purchase credits" prompt when the API returns a credit-gate instruction.
//
// The prompt is default-OFF: a command must explicitly opt in by setting this
// annotation to "true" (cmd.Annotations[CreditPromptSafeAnnotation] = "true").
// This is a deliberate allowlist rather than a denylist — long-running,
// streaming, or watch commands (e.g. `logs --follow`) must never block on stdin
// after they finish, and an opt-in list cannot silently regress them the way a
// drifting denylist could. Commands with no annotation still print the notice
// and the billing URL; they just do not prompt.
const CreditPromptSafeAnnotation = "volcano_credit_prompt_safe"

// terminalCheck reports whether v is an interactive terminal. It is a package
// var so tests can simulate a TTY without a real one (bytes.Buffer is never a
// terminal, which is exactly the non-interactive default we want otherwise).
var terminalCheck = func(v any) bool {
	f, ok := v.(interface{ Fd() uintptr })
	if !ok {
		return false
	}
	fd := f.Fd()
	return isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
}

// creditSeverity distinguishes the two locked credit-gate instructions.
type creditSeverity int

const (
	creditSeverityLow creditSeverity = iota
	creditSeverityNotEnough
)

// handleCreditNotice renders the designed UX for a credit-gate instruction
// (VOL-354). Behavior, per the MAGI design decision:
//
//   - Always print one concise, neutral notice to stderr followed by an
//     actionable billing URL. The notice never claims the request was blocked —
//     the instruction is observed post-execution, from a response header on a
//     command that already ran, so the CLI cannot and does not retroactively
//     block it. Real enforcement (e.g. an HTTP 402) is a server concern.
//   - It never returns an error and never changes the command's exit status.
//     not_enough_credit is non-zero only when the underlying command already
//     failed for its own reasons (handled by the caller in cmd/volcano/main.go).
//   - Only in a safe interactive context (see creditPromptAllowed) does it offer
//     to open the billing page. On consent it calls the browser opener; on
//     decline, prompt error, or browser-open failure it falls back to printing
//     the URL and leaves the exit status untouched.
func handleCreditNotice(cmd *cobra.Command, deps cliruntime.Deps, sev creditSeverity) {
	w := cmd.ErrOrStderr()
	switch sev {
	case creditSeverityNotEnough:
		fmt.Fprintln(w, "Your project does not have enough credit to complete this request.")
	default:
		fmt.Fprintln(w, "Your project is running low on credit.")
	}

	url := billingURL(deps)

	if creditPromptAllowed(cmd) {
		confirmed, err := confirm.Confirm(cmd.InOrStdin(), w, "Open the billing page to purchase credits? [y/N]: ")
		if err == nil && confirmed {
			if openErr := cliruntime.OpenBrowser(deps, url); openErr == nil {
				return
			}
			fmt.Fprintf(w, "Could not open your browser. Purchase credits at: %s\n", url)
			return
		}
	}

	fmt.Fprintf(w, "Purchase credits at: %s\n", url)
}

// creditPromptAllowed reports whether it is safe to prompt the user on stdin.
// All three guards must hold: the command opted in via CreditPromptSafeAnnotation,
// no CI environment is detected, and both stdin and stderr are terminals. Any
// automation, pipe, or unmarked command falls through to the printed URL.
func creditPromptAllowed(cmd *cobra.Command) bool {
	if cmd == nil || cmd.Annotations[CreditPromptSafeAnnotation] != "true" {
		return false
	}
	if os.Getenv("CI") != "" {
		return false
	}
	return terminalCheck(cmd.InOrStdin()) && terminalCheck(cmd.ErrOrStderr())
}

// billingURL resolves the "purchase credits" target from config.WebURL()
// (VOLCANO_WEB_URL takes precedence). If URL construction fails for any reason
// it degrades to the bare web origin so the user still gets a usable link.
func billingURL(deps cliruntime.Deps) string {
	cfg := resolveConfig(deps)
	if u, err := api.WebBillingURL(cfg.WebURL()); err == nil {
		return u
	}
	return cfg.WebURL()
}

// resolveConfig loads config through the injected loader (tests, local mode) or
// the default loader, falling back to compiled defaults on error. Only WebURL()
// is read here, which depends solely on env + compiled defaults, so a default
// config is always a safe fallback.
func resolveConfig(deps cliruntime.Deps) *config.Config {
	loader := deps.ConfigLoader
	if loader == nil {
		loader = config.Load
	}
	if cfg, err := loader(); err == nil && cfg != nil {
		return cfg
	}
	return config.Default()
}
