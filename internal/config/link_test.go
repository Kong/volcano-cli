package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindLinkWalksUpToTheLinkedDirectory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	_, err := WriteLink(root, ProjectConfig{ID: "project-1", Name: "my-app"})
	require.NoError(t, err)
	nested := filepath.Join(root, "web", "app")
	require.NoError(t, os.MkdirAll(nested, 0o755))

	link, err := FindLink(nested)
	require.NoError(t, err)
	require.NotNil(t, link)
	assert.Equal(t, root, link.Dir)
	assert.Equal(t, ProjectConfig{ID: "project-1", Name: "my-app"}, link.Project)
	assert.Equal(t, filepath.Join(root, ".volcano", "project.json"), link.Path())
}

func TestFindLinkStopsAtTheRepositoryRoot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	parent := t.TempDir()
	_, err := WriteLink(parent, ProjectConfig{ID: "someone-elses-project"})
	require.NoError(t, err)
	repo := filepath.Join(parent, "repo")
	require.NoError(t, os.MkdirAll(filepath.Join(repo, ".git"), 0o755))

	link, err := FindLink(repo)
	require.NoError(t, err)
	assert.Nil(t, link, "a link above the repository must not apply inside it")
}

func TestFindLinkRejectsALinkWithoutAProject(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".volcano"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".volcano", "project.json"), []byte(`{}`), 0o644))

	_, err := FindLink(dir)
	require.ErrorContains(t, err, "has no project_id")
}

func TestLinkedProjectPrecedesSavedSelection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(envProjectID, "")
	require.NoError(t, (&Config{CurrentProject: &ProjectConfig{ID: "global", Name: "Global"}}).Save())

	dir := t.TempDir()
	_, err := WriteLink(dir, ProjectConfig{ID: "linked", Name: "Linked"})
	require.NoError(t, err)
	t.Chdir(dir)

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "linked", cfg.ProjectID())
	assert.Equal(t, "Linked", cfg.Project().Name)

	t.Setenv(envProjectID, "from-env")
	assert.Equal(t, "from-env", cfg.ProjectID(), "VOLCANO_PROJECT_ID still wins")

	require.NoError(t, cfg.Save())
	data, err := os.ReadFile(filepath.Join(home, ".volcano", "config.json"))
	require.NoError(t, err)
	assert.NotContains(t, string(data), "linked", "the link is never written to the global config")
}
