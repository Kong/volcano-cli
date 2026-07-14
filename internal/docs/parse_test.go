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
