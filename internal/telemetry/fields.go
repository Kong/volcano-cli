package telemetry

import (
	"maps"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/sensitive"
)

// resourcePlaceholders are the usage-line placeholders (`get <database>
// <branch>`) whose positional arguments name or identify a resource. Anything
// else, such as paths, queries, URLs, or commands run in a sandbox, is never
// sent.
var resourcePlaceholders = map[string]bool{
	"alias": true, "approval-id": true, "backup": true, "branch": true,
	"bucket": true, "database": true, "deployment-id": true, "domain": true,
	"execution-id": true, "frontend": true, "function": true, "function-id": true,
	"key-id": true, "name": true, "name-or-id": true, "new-name": true,
	"project-id": true, "project-name-or-id": true, "restore-id": true,
	"scheduler-id": true, "session-id": true, "template-id": true, "token-id": true,
}

// resourceFlags are the flags whose values name a resource.
var resourceFlags = []string{"bucket", "database", "function", "name", "project", "topic"}

// maxResources bounds the resources one event carries.
const maxResources = 10

// resourceValue is the shape of a resource name or ID the CLI sends. It admits
// no "/" so a path given where a name is expected, such as
// `functions invoke ./src/hello.js`, is never sent.
var resourceValue = regexp.MustCompile(`^[A-Za-z0-9 ._\-:@]{1,100}$`)

// resources returns the resource names and IDs cmd was given: each positional
// argument under a placeholder in resourcePlaceholders, keyed by the
// placeholder, then each resourceFlags flag the user set, keyed by the flag
// name. Values that do not look like a name, or that redact would change, are
// dropped. Nil when there are none.
func resources(cmd *cobra.Command) map[string]string {
	if cmd == nil {
		return nil
	}
	found := map[string]string{}
	add := func(key, value string) {
		value = strings.TrimSpace(value)
		if _, ok := found[key]; ok || !resourceValue.MatchString(value) || redact(value) != value {
			return
		}
		found[key] = value
	}
	args := cmd.Flags().Args()
	dash := cmd.ArgsLenAtDash()
	switch {
	case dash >= 0 && dash <= len(args):
		args = args[:dash]
	case slices.Contains(strings.Fields(cmd.Use), "--"):
		// Without the "--" the usage line expects, positions no longer line
		// up: `exec "echo hi"` would put a sandbox command under session-id.
		args = nil
	}
	for i, placeholder := range placeholders(cmd.Use) {
		if i >= len(args) {
			break
		}
		if resourcePlaceholders[placeholder] {
			add(placeholder, args[i])
		}
	}
	for _, name := range resourceFlags {
		if f := cmd.Flags().Lookup(name); f != nil && f.Changed && f.Annotations[sensitive.FlagAnnotation] == nil {
			add(name, f.Value.String())
		}
	}
	if len(found) == 0 {
		return nil
	}
	keys := slices.Sorted(maps.Keys(found))
	for _, k := range keys[min(len(keys), maxResources):] {
		delete(found, k)
	}
	return found
}

// placeholders returns the argument placeholders of a usage line, in order,
// without their brackets: "get <database> [branch]" gives database, branch. It
// stops at "--" or a repeated argument, after which positions no longer line
// up.
func placeholders(use string) []string {
	fields := strings.Fields(use)
	if len(fields) < 2 {
		return nil
	}
	var names []string
	for _, field := range fields[1:] {
		if field == "--" || strings.Contains(field, "...") {
			break
		}
		names = append(names, strings.Trim(field, "<>[]"))
	}
	return names
}

// maxErrorMessageRunes bounds the error message one event carries.
const maxErrorMessageRunes = 500

// ansiEscape matches a terminal escape sequence, such as a color code.
var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;?]*[\x20-\x2f]*[\x40-\x7e]`)

// errorMessage is err's text as sent: sensitive parts replaced with
// "[redacted]", the home directory replaced with "~",
// terminal escapes and control characters removed, whitespace collapsed,
// secrets redacted, and cut to maxErrorMessageRunes. Whitespace is collapsed
// before redacting so a Unicode space cannot hide a secret from the patterns.
func errorMessage(err error, home string) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	// Text a parser quoted from a file never leaves the machine. If the outer
	// message does not contain it verbatim, send nothing of it.
	if text := sensitive.Text(err); text != "" {
		if !strings.Contains(msg, text) {
			return redacted
		}
		msg = strings.ReplaceAll(msg, text, redacted)
	}
	if len(home) > 1 {
		msg = strings.ReplaceAll(msg, home, "~")
	}
	msg = ansiEscape.ReplaceAllString(msg, "")
	msg = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, msg)
	msg = redact(strings.Join(strings.Fields(msg), " "))
	if runes := []rune(msg); len(runes) > maxErrorMessageRunes {
		msg = string(runes[:maxErrorMessageRunes])
	}
	return msg
}

const redacted = "[redacted]"

// Redaction rules, applied in order. volcano-hosting applies the same rules
// again before the event reaches analytics; its
// docs/internal/guides/cli-telemetry.md is the wire contract both sides follow.
var (
	bearerPattern = regexp.MustCompile(`(?i)\bbearer\s+\S+`)
	jwtPattern    = regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]*`)
	keyPattern    = regexp.MustCompile(`\b(?:pk|pt|sk|ak)-[A-Za-z0-9_-]{16,}`)
	awsKeyPattern = regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)
	userPattern   = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^\s/?#]*@`)
	queryPattern  = regexp.MustCompile(`(https?://[^\s?#]+)\?[^\s#]*`)
	pairPattern   = regexp.MustCompile(`(?i)\b([A-Za-z0-9_]*(?:password|passwd|pwd|secret|token|api[_-]?key|access[_-]?key))=(?:"(?:[^"\\]|\\.)*"|'[^']*'|\\"(?:[^"\\]|\\[^"])*\\"|\S+)`)
	// A JSON field, also inside a %q-quoted string where its quotes are
	// escaped. The value runs to the next unescaped quote, so an escaped
	// quote inside it, or the rest of an escaped object, is removed too.
	jsonPattern = regexp.MustCompile(`(?i)(\\*"[A-Za-z0-9_-]*(?:password|passwd|pwd|secret|token|api[_-]?key|access[_-]?key)\\*"\s*:\s*\\*")(?:[^"\\]|\\.)*`)
	// Long opaque runs. "/" is left out so file paths survive.
	opaquePattern = regexp.MustCompile(`[A-Za-z0-9+=_-]{40,}`)
)

// redact replaces credentials and other secret-shaped text in s.
func redact(s string) string {
	s = bearerPattern.ReplaceAllString(s, "Bearer "+redacted)
	s = jwtPattern.ReplaceAllString(s, redacted)
	s = keyPattern.ReplaceAllString(s, redacted)
	s = awsKeyPattern.ReplaceAllString(s, redacted)
	s = userPattern.ReplaceAllString(s, "${1}"+redacted+"@")
	s = queryPattern.ReplaceAllString(s, "${1}?"+redacted)
	s = pairPattern.ReplaceAllString(s, "${1}="+redacted)
	s = jsonPattern.ReplaceAllString(s, "${1}"+redacted)
	return opaquePattern.ReplaceAllString(s, redacted)
}
