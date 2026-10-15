// Package agent works out which coding agent, if any, ran this CLI process.
//
// Detection reads environment variables only. Coding agents ("harnesses")
// set marker variables in the shells they run commands in, and the CLI
// inherits that shell. Order of precedence:
//
//  1. AI_AGENT, then AGENT: the generic cross-tool convention
//     (https://github.com/agentsmd/agents.md/issues/136). A named value wins.
//     A bare truthy value ("1", "true") says an agent is present but not which.
//  2. Harness-specific markers (see markers), for harnesses that set neither.
//  3. VOLCANO_AGENT: set by the Volcano skills when the harness sets nothing
//     the CLI recognizes.
//  4. A bare truthy AI_AGENT or AGENT: reported as Unknown.
//  5. Weak (host) markers: variables that show the terminal or workspace the
//     command ran in, or agent settings people export in their own profile.
//     A person typing there sets them too, and any agent started there
//     inherits them, so they are the weakest signal.
//
// The result is advisory telemetry. Any process can set these variables, so
// nothing may use it to grant or deny anything.
package agent

import (
	"slices"
	"strings"
)

// Source says which rule produced the agent name.
type Source string

// Sources, one per rule in the package doc.
const (
	SourceAIAgent      Source = "ai_agent"
	SourceAgent        Source = "agent"
	SourceMarker       Source = "marker"
	SourceVolcanoAgent Source = "volcano_agent"
	SourceHost         Source = "host"
)

// Unknown is the name reported when AI_AGENT or AGENT says an agent is
// present without naming it, and no other rule names it.
const Unknown = "unknown"

// maxValueLen caps any env-provided value before it is reported.
const maxValueLen = 64

// Result is the outcome of Detect. The zero value means no agent was detected.
type Result struct {
	// Name is the canonical agent name, e.g. "claude-code". Empty when no
	// agent was detected.
	Name string
	// Raw is the sanitized value of the variable that named the agent, when
	// that variable carried free text (AI_AGENT, AGENT, VOLCANO_AGENT). It can
	// carry more than Name, e.g. a version: "claude-code_2-1-299_agent".
	Raw string
	// Source is the rule that produced Name.
	Source Source
	// Markers lists the canonical agent name of every marker present, host
	// markers included, deduplicated, in table order. It is filled whatever
	// rule produced Name. More than one entry means agents are nested (e.g.
	// Claude Code started from a Cursor terminal).
	Markers []string
}

// marker maps one harness-specific environment variable to an agent. When
// value is set, the variable must equal it; otherwise any non-empty value
// matches. Names are matched exactly, never by prefix: a user's own
// CODEX_AGENT variable must not read as Codex. host marks a weak marker: a
// variable that shows the terminal or workspace, or a setting people export
// themselves, rather than that an agent ran the command.
type marker struct {
	env   string
	value string
	agent string
	host  bool
}

// markers is ordered so the first match is the best guess for the agent
// that ran this command. Host markers go last.
//
// Keep this list in step with the harnesses' release notes. Cross-check with
// other public detectors (Vercel detect-agent, Laravel agent-detector,
// huggingface_hub, the is-ai-agent crate) when adding entries.
var markers = []marker{
	// Claude Cowork runs on Claude Code and sets CLAUDECODE too, so it must
	// come first.
	{env: "CLAUDE_CODE_IS_COWORK", agent: "cowork"},
	{env: "CLAUDECODE", agent: "claude-code"},
	{env: "CLAUDE_CODE_ENTRYPOINT", agent: "claude-code"},
	{env: "CODEX_THREAD_ID", agent: "codex"},
	{env: "CODEX_SANDBOX", agent: "codex"},
	{env: "CODEX_CI", agent: "codex"},
	{env: "GEMINI_CLI", agent: "gemini-cli"},
	{env: "CURSOR_AGENT", agent: "cursor-cli"},
	{env: "CURSOR_EXTENSION_HOST_ROLE", value: "agent-exec", agent: "cursor"},
	{env: "OPENCODE_CLIENT", agent: "opencode"},
	{env: "AUGMENT_AGENT", agent: "augment"},
	{env: "ANTIGRAVITY_AGENT", agent: "antigravity"},

	// Weak markers (host: true). The terminal or workspace only:
	{env: "CURSOR_TRACE_ID", agent: "cursor", host: true},
	{env: "REPL_ID", agent: "replit", host: true},
	// Copilot CLI settings people can export in their own shell profile, so
	// they show the user has Copilot, not that Copilot ran this command.
	// COPILOT_GITHUB_TOKEN is left out entirely for the same reason.
	{env: "COPILOT_MODEL", agent: "github-copilot", host: true},
	{env: "COPILOT_ALLOW_ALL", agent: "github-copilot", host: true},
}

// aliases maps lowercase AI_AGENT / AGENT / VOLCANO_AGENT values (or their
// leading word) to a canonical name, so "claude-code_2-1-299_agent" and
// "claude" both report as "claude-code". Longer aliases are tried first.
//
// The API forwards only names on its own allowlist to analytics and reports
// any other name as "other" (volcano-hosting
// internal/productanalytics/allowlist.go allowedAgents). Keep the canonical
// names here, that allowlist, and the harness list in the volcano-skills
// AGENTS.md in step.
var aliases = map[string]string{
	"claude-code":        "claude-code",
	"claude":             "claude-code",
	"cowork":             "cowork",
	"codex":              "codex",
	"gemini-cli":         "gemini-cli",
	"gemini":             "gemini-cli",
	"cursor-cli":         "cursor-cli",
	"cursor":             "cursor",
	"opencode":           "opencode",
	"augment-cli":        "augment",
	"augment":            "augment",
	"antigravity":        "antigravity",
	"github-copilot-cli": "github-copilot",
	"github-copilot":     "github-copilot",
	"copilot":            "github-copilot",
	"replit":             "replit",
	"goose":              "goose",
	"amp":                "amp",
	"windsurf":           "windsurf",
	"cline":              "cline",
	"roo-code":           "roo-code",
	"roo":                "roo-code",
	"kilo-code":          "kilo-code",
	"kilo":               "kilo-code",
	"kiro":               "kiro",
	"zed":                "zed",
	"aider":              "aider",
	"warp":               "warp",
	"junie":              "junie",
	"devin":              "devin",
	"v0":                 "v0",
	"pi":                 "pi",
}

// Detect reports the agent that ran this process. lookup is os.LookupEnv in
// production; tests pass a fixed map so the test's own shell (which may be
// an agent's) does not leak in.
func Detect(lookup func(string) (string, bool)) Result {
	get := func(name string) string {
		v, _ := lookup(name)
		return strings.TrimSpace(v)
	}

	var found []string
	var first, firstHost string
	for _, m := range markers {
		v := get(m.env)
		if v == "" || (m.value != "" && v != m.value) {
			continue
		}
		switch {
		case m.host && firstHost == "":
			firstHost = m.agent
		case !m.host && first == "":
			first = m.agent
		}
		if !slices.Contains(found, m.agent) {
			found = append(found, m.agent)
		}
	}

	// presence is the source of a bare truthy AI_AGENT or AGENT, if any.
	var presence Source
	for _, g := range []struct {
		env    string
		source Source
	}{{"AI_AGENT", SourceAIAgent}, {"AGENT", SourceAgent}} {
		v := get(g.env)
		switch {
		case v == "" || isFalsy(v):
			continue
		case isTruthy(v):
			if presence == "" {
				presence = g.source
			}
			continue
		}
		if raw := sanitize(v); raw != "" {
			return Result{Name: canonical(raw), Raw: raw, Source: g.source, Markers: found}
		}
	}

	if first != "" {
		return Result{Name: first, Source: SourceMarker, Markers: found}
	}

	if raw := sanitize(get("VOLCANO_AGENT")); raw != "" {
		return Result{Name: canonical(raw), Raw: raw, Source: SourceVolcanoAgent, Markers: found}
	}

	if presence != "" {
		return Result{Name: Unknown, Source: presence, Markers: found}
	}

	if firstHost != "" {
		return Result{Name: firstHost, Source: SourceHost, Markers: found}
	}
	return Result{}
}

// canonical maps a sanitized free-text value to a known agent name when its
// leading word matches an alias, and otherwise returns it lowercased.
func canonical(raw string) string {
	v := strings.ToLower(raw)
	best := ""
	for alias := range aliases {
		if len(alias) <= len(best) {
			continue
		}
		if v == alias || (strings.HasPrefix(v, alias) && isSeparator(v[len(alias)])) {
			best = alias
		}
	}
	if best != "" {
		return aliases[best]
	}
	return v
}

// sanitize keeps a value safe to put in an HTTP header and a dashboard: a
// conservative character set, capped in length.
func sanitize(v string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(v) {
		if b.Len() >= maxValueLen {
			break
		}
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case strings.ContainsRune("._-/@+:", r):
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isSeparator(c byte) bool {
	return (c < 'a' || c > 'z') && (c < '0' || c > '9')
}

func isTruthy(v string) bool {
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func isFalsy(v string) bool {
	switch strings.ToLower(v) {
	case "0", "false", "no", "off":
		return true
	}
	return false
}
