package agent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func env(vars map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		v, ok := vars[name]
		return v, ok
	}
}

func TestDetect(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want Result
	}{
		{
			name: "nothing set",
			env:  map[string]string{},
			want: Result{},
		},
		{
			name: "AI_AGENT with version is normalized and raw is kept",
			env:  map[string]string{"AI_AGENT": "claude-code_2-1-299_agent", "CLAUDECODE": "1"},
			want: Result{Name: "claude-code", Raw: "claude-code_2-1-299_agent", Source: SourceAIAgent, Markers: []string{"claude-code"}},
		},
		{
			name: "AI_AGENT unknown name is reported lowercased",
			env:  map[string]string{"AI_AGENT": "MyBot"},
			want: Result{Name: "mybot", Raw: "MyBot", Source: SourceAIAgent},
		},
		{
			name: "AI_AGENT alias maps to canonical name",
			env:  map[string]string{"AI_AGENT": "github-copilot-cli"},
			want: Result{Name: "github-copilot", Raw: "github-copilot-cli", Source: SourceAIAgent},
		},
		{
			name: "AI_AGENT wins over AGENT",
			env:  map[string]string{"AI_AGENT": "codex", "AGENT": "goose"},
			want: Result{Name: "codex", Raw: "codex", Source: SourceAIAgent},
		},
		{
			name: "AGENT names the agent",
			env:  map[string]string{"AGENT": "amp"},
			want: Result{Name: "amp", Raw: "amp", Source: SourceAgent},
		},
		{
			name: "AGENT=1 defers to a marker for the name",
			env:  map[string]string{"AGENT": "1", "GEMINI_CLI": "1"},
			want: Result{Name: "gemini-cli", Source: SourceMarker, Markers: []string{"gemini-cli"}},
		},
		{
			name: "AGENT=1 with nothing else is unknown",
			env:  map[string]string{"AGENT": "true"},
			want: Result{Name: Unknown, Source: SourceAgent},
		},
		{
			name: "AI_AGENT=1 with nothing else is unknown from ai_agent",
			env:  map[string]string{"AI_AGENT": "1", "AGENT": "yes"},
			want: Result{Name: Unknown, Source: SourceAIAgent},
		},
		{
			name: "AGENT=1 defers to VOLCANO_AGENT for the name",
			env:  map[string]string{"AGENT": "1", "VOLCANO_AGENT": "windsurf"},
			want: Result{Name: "windsurf", Raw: "windsurf", Source: SourceVolcanoAgent},
		},
		{
			name: "falsy AGENT is ignored",
			env:  map[string]string{"AGENT": "off"},
			want: Result{},
		},
		{
			name: "falsy AI_AGENT falls through to AGENT",
			env:  map[string]string{"AI_AGENT": "0", "AGENT": "goose"},
			want: Result{Name: "goose", Raw: "goose", Source: SourceAgent},
		},
		{
			name: "Codex thread ID",
			env:  map[string]string{"CODEX_THREAD_ID": "019a-abc"},
			want: Result{Name: "codex", Source: SourceMarker, Markers: []string{"codex"}},
		},
		{
			name: "CODEX_AGENT is not a Codex marker",
			env:  map[string]string{"CODEX_AGENT": "/Users/me/.config/codex/agents.md"},
			want: Result{},
		},
		{
			name: "Cowork wins over Claude Code",
			env:  map[string]string{"CLAUDECODE": "1", "CLAUDE_CODE_IS_COWORK": "1"},
			want: Result{Name: "cowork", Source: SourceMarker, Markers: []string{"cowork", "claude-code"}},
		},
		{
			name: "Cursor terminal alone is a host signal",
			env:  map[string]string{"CURSOR_TRACE_ID": "abc"},
			want: Result{Name: "cursor", Source: SourceHost, Markers: []string{"cursor"}},
		},
		{
			name: "VOLCANO_AGENT wins over a host marker, which stays in markers",
			env:  map[string]string{"CURSOR_TRACE_ID": "abc", "VOLCANO_AGENT": "cline"},
			want: Result{Name: "cline", Raw: "cline", Source: SourceVolcanoAgent, Markers: []string{"cursor"}},
		},
		{
			name: "AGENT=1 in a Replit shell is unknown, host kept in markers",
			env:  map[string]string{"AGENT": "1", "REPL_ID": "r1"},
			want: Result{Name: Unknown, Source: SourceAgent, Markers: []string{"replit"}},
		},
		{
			name: "AI_AGENT name keeps host markers",
			env:  map[string]string{"AI_AGENT": "goose", "CURSOR_TRACE_ID": "abc"},
			want: Result{Name: "goose", Raw: "goose", Source: SourceAIAgent, Markers: []string{"cursor"}},
		},
		{
			name: "Claude Code in a Cursor terminal reports Claude Code and both markers",
			env:  map[string]string{"CLAUDECODE": "1", "CURSOR_TRACE_ID": "abc"},
			want: Result{Name: "claude-code", Source: SourceMarker, Markers: []string{"claude-code", "cursor"}},
		},
		{
			name: "Cursor host role must equal agent-exec",
			env:  map[string]string{"CURSOR_EXTENSION_HOST_ROLE": "user"},
			want: Result{},
		},
		{
			name: "Cursor agent exec",
			env:  map[string]string{"CURSOR_EXTENSION_HOST_ROLE": "agent-exec"},
			want: Result{Name: "cursor", Source: SourceMarker, Markers: []string{"cursor"}},
		},
		{
			name: "marker wins over VOLCANO_AGENT",
			env:  map[string]string{"OPENCODE_CLIENT": "cli", "VOLCANO_AGENT": "windsurf"},
			want: Result{Name: "opencode", Source: SourceMarker, Markers: []string{"opencode"}},
		},
		{
			name: "VOLCANO_AGENT when nothing else is set",
			env:  map[string]string{"VOLCANO_AGENT": "Claude"},
			want: Result{Name: "claude-code", Raw: "Claude", Source: SourceVolcanoAgent},
		},
		{
			name: "COPILOT_GITHUB_TOKEN alone is not an agent",
			env:  map[string]string{"COPILOT_GITHUB_TOKEN": "ghp_x"},
			want: Result{},
		},
		{
			name: "blank values are ignored",
			env:  map[string]string{"AI_AGENT": "  ", "CLAUDECODE": ""},
			want: Result{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, Detect(env(tc.env)))
		})
	}
}

func TestDetect_EveryMarkerIsRecognized(t *testing.T) {
	for _, m := range markers {
		value := "1"
		if m.value != "" {
			value = m.value
		}
		got := Detect(env(map[string]string{m.env: value}))
		want := SourceMarker
		if m.host {
			want = SourceHost
		}
		assert.Equal(t, m.agent, got.Name, m.env)
		assert.Equal(t, want, got.Source, m.env)
	}
}

func TestDetect_SanitizesFreeText(t *testing.T) {
	got := Detect(env(map[string]string{"AI_AGENT": "evil\r\nX-Injected: 1 " + strings.Repeat("a", 200)}))
	assert.NotContains(t, got.Raw, "\r")
	assert.NotContains(t, got.Raw, "\n")
	assert.NotContains(t, got.Raw, " ")
	assert.LessOrEqual(t, len(got.Raw), maxValueLen)
	assert.True(t, strings.HasPrefix(got.Raw, "evilX-Injected:1"), got.Raw)
}

func TestDetect_AllCharactersDroppedIsNotAName(t *testing.T) {
	got := Detect(env(map[string]string{"AI_AGENT": "\t!!!", "CODEX_CI": "1"}))
	assert.Equal(t, Result{Name: "codex", Source: SourceMarker, Markers: []string{"codex"}}, got)
}

func TestCanonical(t *testing.T) {
	cases := map[string]string{
		"claude":                    "claude-code",
		"claude-code":               "claude-code",
		"claude-code_2-1-299_agent": "claude-code",
		"claudette":                 "claudette",
		"cursor-cli":                "cursor-cli",
		"cursor-cli/1.2":            "cursor-cli",
		"cursor":                    "cursor",
		"Gemini":                    "gemini-cli",
		"amp@0.1":                   "amp",
		"ampersand":                 "ampersand",
	}
	for in, want := range cases {
		assert.Equal(t, want, canonical(in), in)
	}
}
