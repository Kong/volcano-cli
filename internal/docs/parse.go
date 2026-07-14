package docs

import (
	"regexp"
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

var (
	atxHeading  = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*#*\s*$`)
	fenceMarker = regexp.MustCompile("^\\s*(```+|~~~+)")
)

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
	title := deriveTitle(docPath, lines)
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
	anchorCounts := map[string]int{}
	var stack []string // heading breadcrumb by level

	// Preamble section (content before the first heading) is attributed to the
	// document title with no anchor.
	cur := openSection{heading: title, level: 0, anchor: "", path: []string{title}, startLine: 1, bodyStart: 0}
	inFence := false
	var fenceTok string

	flush := func(endLine, bodyEnd int) {
		body := strings.TrimRight(strings.Join(lines[cur.bodyStart:bodyEnd], "\n"), "\n")
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
		if m := fenceMarker.FindStringSubmatch(line); m != nil {
			tok := m[1][:3]
			switch {
			case !inFence:
				inFence = true
				fenceTok = tok
			case inFence && strings.HasPrefix(strings.TrimSpace(line), fenceTok):
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

		anchor := uniqueAnchor(anchorCounts, heading)
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

func deriveTitle(docPath string, lines []string) string {
	inFence := false
	var fenceTok string
	for _, line := range lines {
		if m := fenceMarker.FindStringSubmatch(line); m != nil {
			tok := m[1][:3]
			if !inFence {
				inFence, fenceTok = true, tok
			} else if strings.HasPrefix(strings.TrimSpace(line), fenceTok) {
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
func uniqueAnchor(counts map[string]int, heading string) string {
	base := slugify(heading)
	if base == "" {
		base = "section"
	}
	n := counts[base]
	counts[base] = n + 1
	if n == 0 {
		return base
	}
	return base + "-" + itoa(n)
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
