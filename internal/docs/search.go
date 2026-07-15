package docs

import (
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// BM25 parameters. Standard defaults; k1 controls term-frequency saturation,
// b controls length normalization.
const (
	bm25K1 = 1.5
	bm25B  = 0.75

	titleBoost   = 3 // title tokens repeated N times in the indexed stream
	headingBoost = 3
	pathBoost    = 2

	phraseBonus    = 2.0 // multiplicative when the exact query phrase appears
	technicalBonus = 1.5 // added per matched technical token (flag/identifier)

	maxChunkChars = 2000 // split exceptionally large sections for indexing
	snippetChars  = 260
)

// Result is one ranked search hit, deduplicated to the section level.
type Result struct {
	ID           string   `json:"id"`
	Rank         int      `json:"rank"`
	Score        float64  `json:"score"`
	Path         string   `json:"path"`
	Topic        string   `json:"topic"`
	Title        string   `json:"title"`
	HeadingPath  []string `json:"heading_path"`
	Anchor       string   `json:"anchor"`
	LineStart    int      `json:"line_start"`
	LineEnd      int      `json:"line_end"`
	Snippet      string   `json:"snippet"`
	MatchedTerms []string `json:"matched_terms"`
}

type chunk struct {
	section   Section
	tf        map[string]int
	length    int
	technical map[string]bool
	lowerText string
}

// Index is an in-memory BM25 index built per invocation. No index is persisted;
// the corpus is small enough to rebuild on each search.
type Index struct {
	chunks []chunk
	df     map[string]int
	avgdl  float64
	n      int
}

// NewIndex builds a BM25 index over the given sections.
func NewIndex(sections []Section) *Index {
	idx := &Index{df: map[string]int{}}
	for _, sec := range sections {
		for _, body := range splitLargeBody(sec.Body) {
			c := buildChunk(sec, body)
			idx.chunks = append(idx.chunks, c)
		}
	}
	idx.n = len(idx.chunks)
	var totalLen int
	for i := range idx.chunks {
		totalLen += idx.chunks[i].length
		seen := map[string]bool{}
		for term := range idx.chunks[i].tf {
			if !seen[term] {
				idx.df[term]++
				seen[term] = true
			}
		}
	}
	if idx.n > 0 {
		idx.avgdl = float64(totalLen) / float64(idx.n)
	}
	return idx
}

// Search returns up to limit ranked results, filtered to topic when non-empty.
func (idx *Index) Search(query, topic string, limit int) []Result {
	qWords, qTech := tokenize(query)
	qTerms := dedupe(append(append([]string{}, qWords...), qTech...))
	if len(qTerms) == 0 {
		return nil
	}
	phrase := normalizePhrase(query)

	type scored struct {
		res   Result
		score float64
	}
	best := map[string]scored{} // section ID -> best chunk

	for i := range idx.chunks {
		c := &idx.chunks[i]
		if topic != "" && !strings.EqualFold(c.section.Topic, topic) {
			continue
		}
		var score float64
		var matched []string
		for _, term := range qTerms {
			f, ok := c.tf[term]
			if !ok {
				continue
			}
			matched = append(matched, term)
			idf := idxIDF(idx.n, idx.df[term])
			num := float64(f) * (bm25K1 + 1)
			den := float64(f) + bm25K1*(1-bm25B+bm25B*float64(c.length)/idx.avgdl)
			score += idf * num / den
		}
		if score == 0 {
			continue
		}
		// Technical token bonus (flags, identifiers, paths).
		for _, t := range qTech {
			if c.technical[t] {
				score += technicalBonus
			}
		}
		// Exact phrase bonus.
		if phrase != "" && len(qTerms) > 1 && strings.Contains(c.lowerText, phrase) {
			score *= phraseBonus
		}
		id := c.section.ID()
		if existing, ok := best[id]; ok && existing.score >= score {
			continue
		}
		best[id] = scored{
			score: score,
			res: Result{
				ID:           id,
				Path:         c.section.DocPath,
				Topic:        c.section.Topic,
				Title:        c.section.Title,
				HeadingPath:  c.section.HeadingPath,
				Anchor:       c.section.Anchor,
				LineStart:    c.section.LineStart,
				LineEnd:      c.section.LineEnd,
				Snippet:      snippet(c.section.Body, qTerms),
				MatchedTerms: dedupe(matched),
			},
		}
	}

	out := make([]Result, 0, len(best))
	for _, s := range best {
		r := s.res
		r.Score = s.score
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].ID < out[j].ID
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	for i := range out {
		out[i].Rank = i + 1
	}
	return out
}

func idxIDF(n, df int) float64 {
	if df == 0 {
		return 0
	}
	return math.Log(1 + (float64(n)-float64(df)+0.5)/(float64(df)+0.5))
}

func buildChunk(sec Section, body string) chunk {
	c := chunk{section: sec, tf: map[string]int{}, technical: map[string]bool{}}
	add := func(text string, mult int) {
		w, tech := tokenize(text)
		for _, t := range w {
			c.tf[t] += mult
			c.length += mult
		}
		for _, t := range tech {
			c.tf[t] += mult
			c.length += mult
			c.technical[t] = true
		}
	}
	add(body, 1)
	add(sec.Title, titleBoost)
	add(strings.Join(sec.HeadingPath, " "), headingBoost)
	add(pathTokens(sec.DocPath), pathBoost)
	// Normalize whitespace the same way as the query phrase so the exact-phrase
	// bonus can match across newlines / multiple spaces in the source.
	c.lowerText = normalizePhrase(strings.Join(sec.HeadingPath, " ") + " " + body)
	return c
}

func pathTokens(p string) string {
	r := strings.NewReplacer("/", " ", "-", " ", "_", " ", ".", " ")
	return r.Replace(p)
}

// splitLargeBody splits an exceptionally large section into paragraph-aligned
// windows so ranking is not dominated by one huge section. Chunks share the
// section id and are deduplicated at result time.
func splitLargeBody(body string) []string {
	if len(body) <= maxChunkChars {
		return []string{body}
	}
	paras := strings.Split(body, "\n\n")
	var out []string
	var b strings.Builder
	for _, p := range paras {
		if b.Len() > 0 && b.Len()+len(p) > maxChunkChars {
			out = append(out, b.String())
			b.Reset()
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(p)
	}
	if b.Len() > 0 {
		out = append(out, b.String())
	}
	if len(out) == 0 {
		return []string{body}
	}
	return out
}

var (
	wordSplit = regexp.MustCompile(`[^a-z0-9]+`)
	// technical tokens preserve identifiers, flags, dotted names, and paths.
	techSplit = regexp.MustCompile(`[^a-z0-9_./-]+`)
)

// tokenize returns normalized word tokens and preserved technical tokens.
func tokenize(text string) (words, technical []string) {
	lower := strings.ToLower(text)
	for _, w := range wordSplit.Split(lower, -1) {
		if w == "" || isStopWord(w) {
			continue
		}
		words = append(words, w)
	}
	for _, t := range techSplit.Split(lower, -1) {
		t = strings.Trim(t, "-./_")
		// Only keep tokens that carry a technical separator; plain words are
		// already covered by the word tokenizer.
		if t == "" || !strings.ContainsAny(t, "_./-") {
			continue
		}
		technical = append(technical, t)
	}
	// Preserve leading -- flags explicitly (stripped above).
	for raw := range strings.FieldsSeq(lower) {
		if strings.HasPrefix(raw, "--") {
			f := strings.Trim(raw, ".,;:)")
			if len(f) > 2 {
				technical = append(technical, f)
			}
		}
	}
	return words, technical
}

var stopWords = map[string]bool{
	"the": true, "a": true, "an": true, "and": true, "or": true, "of": true,
	"to": true, "in": true, "is": true, "it": true, "for": true, "on": true,
	"with": true, "how": true, "do": true, "i": true, "my": true, "can": true,
}

func isStopWord(w string) bool { return stopWords[w] }

// runeFloor moves byte index i left to the nearest rune boundary so snippet
// windows never split a multi-byte character.
func runeFloor(s string, i int) int {
	if i >= len(s) {
		return len(s)
	}
	for i > 0 && !utf8.RuneStart(s[i]) {
		i--
	}
	return i
}

func normalizePhrase(query string) string {
	return strings.Join(strings.Fields(strings.ToLower(query)), " ")
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := in[:0]
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// snippet returns a short, single-line excerpt around the first query-term
// match, or the leading text when nothing matches cleanly.
func snippet(body string, terms []string) string {
	flat := strings.Join(strings.Fields(body), " ")
	if flat == "" {
		return ""
	}
	lower := strings.ToLower(flat)
	pos := -1
	for _, t := range terms {
		if i := strings.Index(lower, t); i >= 0 && (pos < 0 || i < pos) {
			pos = i
		}
	}
	if pos < 0 {
		if len(flat) > snippetChars {
			return flat[:runeFloor(flat, snippetChars)] + "…"
		}
		return flat
	}
	start := runeFloor(flat, max(pos-snippetChars/3, 0))
	end := runeFloor(flat, min(start+snippetChars, len(flat)))
	out := flat[start:end]
	if start > 0 {
		out = "…" + out
	}
	if end < len(flat) {
		out += "…"
	}
	return out
}
