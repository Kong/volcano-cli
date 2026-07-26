package docs

import (
	"regexp"
	"strconv"
	"strings"
)

// Section is a heading-delimited slice of a document. Sections are the unit of
// search and retrieval: each has a stable id (docPath#anchor), a heading
// breadcrumb for context, and source line bounds.
type Section struct {
	DocPath     string   `json:"path"`
	Topic       string   `json:"topic"`
	Title       string   `json:"title"`
	Heading     string   `json:"heading"`
	HeadingPath []string `json:"heading_path"`
	Anchor      string   `json:"anchor"`
	Level       int      `json:"-"`
	LineStart   int      `json:"line_start"`
	LineEnd     int      `json:"line_end"`
	Body        string   `json:"-"`
}

// ID returns the stable section identifier docPath#anchor. A section with no
// anchor (document preamble) is identified by its bare doc path.
func (s Section) ID() string {
	if s.Anchor == "" {
		return s.DocPath
	}
	return s.DocPath + "#" + s.Anchor
}

// atxHeading matches an ATX heading. A trailing #-sequence is only a closing
// marker when whitespace precedes it (CommonMark), so `# C#` keeps its `#`.
var atxHeading = regexp.MustCompile(`^(#{1,6})\s+(.+?)(?:\s+#+)?\s*$`)

// fenceDelim reports the fence character and run length when line begins a
// code fence (>=3 backticks or tildes), else n==0.
func fenceDelim(line string) (ch byte, n int) {
	t := strings.TrimLeft(line, " \t")
	if t == "" {
		return 0, 0
	}
	c := t[0]
	if c != '`' && c != '~' {
		return 0, 0
	}
	for n < len(t) && t[n] == c {
		n++
	}
	if n < 3 {
		return 0, 0
	}
	return c, n
}

// isFenceClose reports whether line closes a fence opened with openChar/openLen.
// Per CommonMark the closing fence must use the same character, be at least as
// long as the opening fence, and contain nothing but the fence and trailing
// spaces (so an opener like ```go never closes another block).
func isFenceClose(line string, openChar byte, openLen int) bool {
	ch, n := fenceDelim(line)
	if ch != openChar || n < openLen {
		return false
	}
	rest := strings.TrimLeft(line, " \t")
	return strings.TrimRight(rest[n:], " \t") == ""
}

// Topic returns the first path segment of a doc path, e.g. "authentication"
// for "authentication/overview.md", or "" for a top-level file.
func topicOf(docPath string) string {
	if top, _, ok := strings.Cut(docPath, "/"); ok {
		return top
	}
	return ""
}

// ParseDoc splits raw markdown into sections. It is fence-aware so ATX-style
// "#" lines inside code blocks are never treated as headings. The document
// title is the first level-1 heading, falling back to a prettified file name.
func ParseDoc(docPath string, content []byte) []Section {
	lines := strings.Split(string(content), "\n")
	// A terminating newline yields a phantom trailing empty element; drop it so
	// line ranges reflect real content (otherwise the last section's LineEnd is
	// one past its content).
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	// Strip a leading YAML frontmatter block so its keys never become searchable
	// text; docTitle prefers the frontmatter title over the first H1.
	fmLen, _ := splitFrontmatter(lines)
	title := docTitle(docPath, lines)
	topic := topicOf(docPath)

	type openSection struct {
		heading   string
		level     int
		anchor    string
		path      []string
		startLine int
		bodyStart int // index into lines where body begins
	}

	var sections []Section
	usedAnchors := map[string]bool{}
	var stack []string // heading breadcrumb by level

	// Preamble section (content before the first heading) is attributed to the
	// document title with no anchor.
	cur := openSection{heading: title, level: 0, anchor: "", path: []string{title}, startLine: fmLen + 1, bodyStart: fmLen}
	inFence := false
	var fenceChar byte
	var fenceLen int

	flush := func(endLine, bodyEnd int) {
		body := strings.TrimRight(strings.Join(lines[cur.bodyStart:bodyEnd], "\n"), "\n")
		// Skip an empty synthetic preamble (e.g. a doc that opens with its H1),
		// which would otherwise emit an inverted [1,0] range.
		if cur.level == 0 && strings.TrimSpace(body) == "" {
			return
		}
		sections = append(sections, Section{
			DocPath:     docPath,
			Topic:       topic,
			Title:       title,
			Heading:     cur.heading,
			HeadingPath: append([]string(nil), cur.path...),
			Anchor:      cur.anchor,
			Level:       cur.level,
			LineStart:   cur.startLine,
			LineEnd:     endLine,
			Body:        body,
		})
	}

	for i, line := range lines {
		if i < fmLen {
			continue // inside the stripped frontmatter block
		}
		if ch, n := fenceDelim(line); ch != 0 {
			if !inFence {
				inFence, fenceChar, fenceLen = true, ch, n
			} else if isFenceClose(line, fenceChar, fenceLen) {
				inFence = false
			}
			continue
		}
		if inFence {
			continue
		}
		hm := atxHeading.FindStringSubmatch(line)
		if hm == nil {
			continue
		}
		level := len(hm[1])
		heading := strings.TrimSpace(hm[2])

		// Close the current section at the line before this heading.
		flush(i, i)

		// Update the breadcrumb stack for this level.
		if level-1 < len(stack) {
			stack = stack[:level-1]
		}
		for len(stack) < level-1 {
			stack = append(stack, "")
		}
		stack = append(stack, heading)
		crumb := nonEmpty(stack)
		if len(crumb) == 0 || !strings.EqualFold(crumb[0], title) {
			crumb = append([]string{title}, crumb...)
		}

		anchor := uniqueAnchor(usedAnchors, heading)
		cur = openSection{
			heading:   heading,
			level:     level,
			anchor:    anchor,
			path:      crumb,
			startLine: i + 1,
			bodyStart: i,
		}
	}
	flush(len(lines), len(lines))

	return sections
}

func nonEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// splitFrontmatter detects a leading YAML frontmatter block delimited by lines
// of exactly "---". It returns the number of lines the block occupies (0 when
// there is none or the block is unterminated) and the value of its "title"
// field if present. Callers strip those lines from section parsing so the YAML
// never becomes searchable text.
func splitFrontmatter(lines []string) (n int, title string) {
	if len(lines) == 0 || strings.TrimRight(lines[0], " \t") != "---" {
		return 0, ""
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], " \t") == "---" {
			for _, l := range lines[1:i] {
				key, val, ok := strings.Cut(l, ":")
				if ok && strings.TrimSpace(key) == "title" {
					title = unquoteScalar(strings.TrimSpace(val))
				}
			}
			return i + 1, title
		}
	}
	return 0, "" // no closing delimiter: treat as ordinary content
}

// unquoteScalar removes surrounding quotes from a YAML scalar. Double-quoted
// values (as emitted by the migration tool) are unescaped via strconv.Unquote;
// single-quoted values are stripped literally.
func unquoteScalar(s string) string {
	if len(s) >= 2 && s[0] == '"' {
		if u, err := strconv.Unquote(s); err == nil {
			return u
		}
	}
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		// YAML single-quoted scalars escape an apostrophe by doubling it.
		return strings.ReplaceAll(s[1:len(s)-1], "''", "'")
	}
	return s
}

// docTitle is the single source of a document's title for every consumer
// (ParseDoc, Service.List, Service.Get): the frontmatter title if present,
// else the first H1, else a prettified file name. Control characters are
// stripped so a title from a user-configured/untrusted corpus cannot inject
// terminal escape sequences when rendered by `docs search`/`docs list`.
func docTitle(docPath string, lines []string) string {
	fmLen, title := splitFrontmatter(lines)
	if title == "" {
		title = deriveTitle(docPath, lines[fmLen:])
	}
	return stripControl(title)
}

// stripControl drops C0/C1 control characters (turning tabs into spaces) so
// decoded frontmatter titles are safe to print to a terminal.
func stripControl(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' {
			return ' '
		}
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return -1
		}
		return r
	}, s)
}

func deriveTitle(docPath string, lines []string) string {
	inFence := false
	var fenceChar byte
	var fenceLen int
	for _, line := range lines {
		if ch, n := fenceDelim(line); ch != 0 {
			if !inFence {
				inFence, fenceChar, fenceLen = true, ch, n
			} else if isFenceClose(line, fenceChar, fenceLen) {
				inFence = false
			}
			continue
		}
		if inFence {
			continue
		}
		if m := atxHeading.FindStringSubmatch(line); len(m) > 1 && len(m[1]) == 1 {
			return strings.TrimSpace(m[2])
		}
	}
	return prettifyFileName(docPath)
}

func prettifyFileName(docPath string) string {
	name := docPath
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSuffix(name, ".md")
	name = strings.ReplaceAll(name, "-", " ")
	name = strings.ReplaceAll(name, "_", " ")
	return strings.TrimSpace(name)
}

var anchorStrip = regexp.MustCompile(`[^a-z0-9 -]+`)

// slugify mirrors GitHub's heading anchor algorithm closely enough for
// round-tripping search ids back to get: lowercase, drop punctuation, collapse
// spaces to hyphens.
func slugify(heading string) string {
	s := strings.ToLower(strings.TrimSpace(heading))
	s = strings.ReplaceAll(s, "`", "")
	s = anchorStrip.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, " ", "-")
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return strings.Trim(s, "-")
}

// uniqueAnchor returns a GitHub-compatible anchor, appending -1, -2, ... for
// duplicate headings within a document.
// uniqueAnchor returns a GitHub-compatible anchor, appending -1, -2, … for
// duplicates. It tracks every emitted anchor (not just per-base counts) so an
// explicit heading that collides with a generated suffix — e.g. `Foo`, `Foo`,
// `Foo-1` → `foo`, `foo-1`, `foo-1-1` — still yields unique, addressable ids.
func uniqueAnchor(used map[string]bool, heading string) string {
	base := slugify(heading)
	if base == "" {
		base = "section"
	}
	cand := base
	for i := 1; used[cand]; i++ {
		cand = base + "-" + itoa(i)
	}
	used[cand] = true
	return cand
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
