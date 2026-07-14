package docs

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sampleSections() []Section {
	docs := map[string]string{
		"authentication/service-keys.md": "# Service Keys\n\n## Overview\nThe service_role key bypasses RLS and must never be exposed to a browser.\n\n## Usage\nUse the service key on the backend only.",
		"authentication/anon-keys.md":    "# Anon Keys\n\n## Overview\nThe anon key is safe to embed in frontend code.",
		"functions/deploy.md":            "# Deploy Functions\n\n## Deploying\nRun volcano functions deploy --all to deploy every function.",
		"storage/buckets.md":             "# Buckets\n\n## Create\nCreate a storage bucket for your files.",
	}
	var all []Section
	for p, c := range docs {
		all = append(all, ParseDoc(p, []byte(c))...)
	}
	return all
}

func TestSearchRanksRelevantSectionFirst(t *testing.T) {
	idx := NewIndex(sampleSections())
	results := idx.Search("service key", "", 10)
	require.NotEmpty(t, results)
	assert.Contains(t, results[0].Path, "service-keys.md")
}

func TestSearchPreservesTechnicalTokens(t *testing.T) {
	idx := NewIndex(sampleSections())
	results := idx.Search("service_role", "", 10)
	require.NotEmpty(t, results)
	// The identifier only appears in the service-keys doc.
	assert.Contains(t, results[0].Path, "service-keys.md")
}

func TestSearchTopicFilter(t *testing.T) {
	idx := NewIndex(sampleSections())
	results := idx.Search("key", "storage", 10)
	for _, r := range results {
		assert.Equal(t, "storage", r.Topic)
	}
}

func TestSearchDeduplicatesBySection(t *testing.T) {
	idx := NewIndex(sampleSections())
	results := idx.Search("service key backend", "", 10)
	seen := map[string]bool{}
	for _, r := range results {
		assert.False(t, seen[r.ID], "duplicate section id %s", r.ID)
		seen[r.ID] = true
	}
}

func TestSearchFlagQuery(t *testing.T) {
	idx := NewIndex(sampleSections())
	results := idx.Search("--all deploy", "", 10)
	require.NotEmpty(t, results)
	assert.Contains(t, results[0].Path, "functions/deploy.md")
}

func TestSearchEmptyQueryReturnsNil(t *testing.T) {
	idx := NewIndex(sampleSections())
	assert.Nil(t, idx.Search("   ", "", 10))
}

func TestSnippetIsValidUTF8(t *testing.T) {
	// Multibyte runes longer than the snippet window must not be split.
	noMatch := snippet(strings.Repeat("café ", 200), []string{"zzz-nomatch"})
	assert.True(t, utf8.ValidString(noMatch))

	withMatch := snippet(strings.Repeat("naïve ", 100)+"target", []string{"target"})
	assert.True(t, utf8.ValidString(withMatch))
}
