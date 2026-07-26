package docs

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDocHeadingsAndAnchors(t *testing.T) {
	md := strings.Join([]string{
		"# Password Reset",
		"",
		"Intro text.",
		"",
		"## How It Works",
		"body one",
		"",
		"## How It Works",
		"duplicate heading body",
	}, "\n")

	secs := ParseDoc("authentication/password-reset.md", []byte(md))
	require.GreaterOrEqual(t, len(secs), 3)

	// Title comes from the H1.
	assert.Equal(t, "Password Reset", secs[0].Title)
	assert.Equal(t, "authentication", secs[0].Topic)

	// Duplicate headings get GitHub-style -N anchor suffixes.
	var anchors []string
	for _, s := range secs {
		if s.Heading == "How It Works" {
			anchors = append(anchors, s.Anchor)
		}
	}
	require.Len(t, anchors, 2)
	assert.Equal(t, "how-it-works", anchors[0])
	assert.Equal(t, "how-it-works-1", anchors[1])

	// Breadcrumb does not duplicate the title when the H1 equals it.
	for _, s := range secs {
		if s.Anchor == "how-it-works" {
			assert.Equal(t, []string{"Password Reset", "How It Works"}, s.HeadingPath)
		}
	}
}

func TestParseDocIgnoresHeadingsInFencedCode(t *testing.T) {
	md := strings.Join([]string{
		"# Title",
		"",
		"```bash",
		"# this is a shell comment, not a heading",
		"volcano docs sync",
		"```",
		"",
		"## Real Heading",
		"content",
	}, "\n")

	secs := ParseDoc("guide.md", []byte(md))
	var headings []string
	for _, s := range secs {
		headings = append(headings, s.Heading)
	}
	assert.Contains(t, headings, "Real Heading")
	assert.NotContains(t, headings, "this is a shell comment, not a heading")
}

func TestParseDocLineRanges(t *testing.T) {
	md := strings.Join([]string{
		"# Title", // line 1
		"intro",   // line 2
		"## A",    // line 3
		"a body",  // line 4
		"## B",    // line 5
		"b body",  // line 6
	}, "\n")
	secs := ParseDoc("d.md", []byte(md))
	var a Section
	for _, s := range secs {
		if s.Heading == "A" {
			a = s
		}
	}
	assert.Equal(t, 3, a.LineStart)
	assert.Equal(t, 4, a.LineEnd)
}

func TestDeriveTitleFallsBackToFileName(t *testing.T) {
	secs := ParseDoc("getting-started/quick-start.md", []byte("no heading here\njust text"))
	assert.Equal(t, "quick start", secs[0].Title)
}

func TestParseDocTrailingNewlineLineRanges(t *testing.T) {
	// Trailing newline (as every GitHub-fetched markdown file has) must not
	// inflate the last section's LineEnd or emit an inverted preamble range.
	md := "# Title\nintro\n\n## A\nalpha body\n"
	secs := ParseDoc("d.md", []byte(md))

	var a Section
	for _, s := range secs {
		assert.GreaterOrEqual(t, s.LineEnd, s.LineStart, "no inverted range for %q", s.Heading)
		if s.Heading == "A" {
			a = s
		}
	}
	// Lines: 1=# Title, 2=intro, 3=blank, 4=## A, 5=alpha body.
	assert.Equal(t, 4, a.LineStart)
	assert.Equal(t, 5, a.LineEnd)
	// A doc opening with its H1 yields no empty preamble section.
	assert.Equal(t, "Title", secs[0].Heading)
}

func TestParseDocNestedFenceRequiresEqualLengthClose(t *testing.T) {
	md := strings.Join([]string{
		"# T",
		"````markdown", // 4-backtick opener
		"```",          // inner 3-backtick line — not a valid close
		"## Not A Heading",
		"```",
		"````", // 4-backtick close
		"## Real",
		"real body",
	}, "\n")
	secs := ParseDoc("d.md", []byte(md))
	var headings []string
	for _, s := range secs {
		headings = append(headings, s.Heading)
	}
	assert.Contains(t, headings, "Real")
	assert.NotContains(t, headings, "Not A Heading")
}

func TestParseDocHeadingKeepsTrailingHashWithoutSpace(t *testing.T) {
	// `# C#` — the trailing # is part of the title, not a closing marker.
	secs := ParseDoc("d.md", []byte("# C#\nbody"))
	assert.Equal(t, "C#", secs[0].Title)
	assert.Equal(t, "C#", secs[0].Heading)
	// A real closing sequence (space-preceded) is still stripped.
	secs2 := ParseDoc("e.md", []byte("## Title ###\nbody"))
	assert.Equal(t, "Title", secs2[len(secs2)-1].Heading)
}

func TestParseDocAnchorsUniqueOnSuffixCollision(t *testing.T) {
	md := "# T\n## Foo\na\n## Foo\nb\n## Foo-1\nc"
	secs := ParseDoc("d.md", []byte(md))
	seen := map[string]bool{}
	var anchors []string
	for _, s := range secs {
		if s.Anchor == "" {
			continue
		}
		assert.False(t, seen[s.Anchor], "duplicate anchor %q", s.Anchor)
		seen[s.Anchor] = true
		anchors = append(anchors, s.Anchor)
	}
	assert.Equal(t, []string{"t", "foo", "foo-1", "foo-1-1"}, anchors)
}

func TestParseDocFrontmatter(t *testing.T) {
	md := strings.Join([]string{
		"---",
		`title: "Functions"`,
		`description: "Serverless code that runs on demand."`,
		"---",
		"",
		"Functions are serverless code.",
		"",
		"## Creating functions",
		"body",
	}, "\n")

	secs := ParseDoc("cli/functions.md", []byte(md))
	require.NotEmpty(t, secs)

	// Title comes from frontmatter, not an H1 (there is none).
	assert.Equal(t, "Functions", secs[0].Title)

	// Frontmatter keys must never leak into any searchable section body.
	for _, s := range secs {
		assert.NotContains(t, s.Body, "description:")
		assert.NotContains(t, s.Body, "---")
	}

	// The preamble body starts after the frontmatter block, and line numbers
	// stay true to the source file (frontmatter occupies lines 1-4).
	assert.Contains(t, secs[0].Body, "Functions are serverless code.")
	assert.Equal(t, 5, secs[0].LineStart)
}

func TestParseDocNoFrontmatterStillUsesH1(t *testing.T) {
	md := "# Legacy Title\n\nBody without frontmatter.\n"
	secs := ParseDoc("legacy.md", []byte(md))
	require.NotEmpty(t, secs)
	assert.Equal(t, "Legacy Title", secs[0].Title)
}

func TestUnquoteScalarSingleQuoteEscape(t *testing.T) {
	// YAML single-quoted scalars escape an apostrophe by doubling it.
	assert.Equal(t, "Developer's guide", unquoteScalar(`'Developer''s guide'`))
	assert.Equal(t, "plain", unquoteScalar("plain"))
	assert.Equal(t, `has "quotes"`, unquoteScalar(`"has \"quotes\""`))
}

func TestDocTitleStripsControlChars(t *testing.T) {
	// A frontmatter title with an escaped OSC-52 sequence must not survive as
	// live terminal control bytes.
	md := strings.Join([]string{"---", `title: "safe\u001b]52;c;x\u0007end"`, "---", "", "body"}, "\n")
	title := docTitle("x.md", strings.Split(md, "\n"))
	assert.Equal(t, "safe]52;c;xend", title)
	for _, r := range title {
		assert.False(t, r < 0x20 || (r >= 0x7f && r <= 0x9f), "control char leaked: %q", r)
	}
}

func TestDocTitleFrontmatterElseH1(t *testing.T) {
	fm := strings.Split("---\ntitle: \"From FM\"\n---\n\n## body", "\n")
	assert.Equal(t, "From FM", docTitle("a.md", fm))
	h1 := strings.Split("# From H1\n\nbody", "\n")
	assert.Equal(t, "From H1", docTitle("a.md", h1))
	none := strings.Split("no heading here\n", "\n")
	assert.Equal(t, "my doc", docTitle("my-doc.md", none))
}
