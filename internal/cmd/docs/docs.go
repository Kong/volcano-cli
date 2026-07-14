// Package docs wires the `volcano docs` command group: sync, search, get, and
// list over the Volcano documentation corpus. Machine-readable (--json) output
// is a first-class, versioned contract for AI agents.
package docs

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/config"
	"github.com/Kong/volcano-cli/internal/docs"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// envelopeSchemaVersion versions the JSON output contract consumed by agents.
const envelopeSchemaVersion = 1

// New returns the `docs` command group.
func New(deps cliruntime.Deps) *cobra.Command {
	f := &flags{}
	group := &cobra.Command{
		Use:   "docs",
		Short: "Search and read Volcano documentation",
		Long: "docs fetches the Volcano documentation corpus from its source " +
			"(default: the Kong/volcano-hosting repository) into a local cache and " +
			"lets you search and read it offline.\n\n" +
			"It is built for AI agents and humans alike: use `docs search` to find " +
			"relevant docs, `docs get` to read a document or section, and `--json` " +
			"for machine-readable output.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	pf := group.PersistentFlags()
	pf.BoolVar(&f.jsonOut, "json", false, "Emit machine-readable JSON")
	pf.BoolVar(&f.offline, "offline", false, "Never touch the network; use only the local cache")
	pf.StringVar(&f.repo, "repo", "", "Docs source repository (owner/name)")
	pf.StringVar(&f.ref, "ref", "", "Docs source git ref (branch, tag, or commit)")
	pf.StringVar(&f.path, "path", "", "Docs source subdirectory")

	group.AddCommand(newSync(deps, f))
	group.AddCommand(newSearch(deps, f))
	group.AddCommand(newGet(deps, f))
	group.AddCommand(newList(deps, f))
	return group
}

// flags holds the persistent group flags shared by subcommands.
type flags struct {
	jsonOut bool
	offline bool
	repo    string
	ref     string
	path    string
}

func (f *flags) service(deps cliruntime.Deps) (*docs.Service, error) {
	return docs.NewService(docs.Options{
		Overrides:    docs.Overrides{Repo: f.repo, Ref: f.ref, Path: f.path},
		Config:       loadConfig(deps),
		HTTPClient:   deps.HTTPClient,
		CacheDir:     deps.DocsCacheDir,
		GitHubAPIURL: deps.DocsGitHubAPIURL,
		RawBaseURL:   deps.DocsRawBaseURL,
		Token:        strings.TrimSpace(os.Getenv("GITHUB_TOKEN")),
		Now:          deps.Now,
	})
}

func loadConfig(deps cliruntime.Deps) *config.Config {
	load := config.Load
	if deps.ConfigLoader != nil {
		load = deps.ConfigLoader
	}
	cfg, err := load()
	if err != nil || cfg == nil {
		return config.Default()
	}
	return cfg
}

// --- JSON envelope -------------------------------------------------------

type sourceMeta struct {
	Provider       string `json:"provider"`
	Repository     string `json:"repository"`
	Ref            string `json:"ref"`
	Path           string `json:"path"`
	ResolvedCommit string `json:"resolved_commit"`
}

type errMeta struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type envelope struct {
	SchemaVersion int              `json:"schema_version"`
	Command       string           `json:"command"`
	Source        sourceMeta       `json:"source"`
	Cache         *docs.CacheState `json:"cache,omitempty"`
	Data          any              `json:"data,omitempty"`
	Error         *errMeta         `json:"error,omitempty"`
}

func sourceMetaOf(svc *docs.Service, resolvedCommit string) sourceMeta {
	if svc == nil {
		return sourceMeta{Provider: docs.Provider}
	}
	src := svc.Source()
	if resolvedCommit == "" {
		resolvedCommit = svc.ResolvedCommit()
	}
	return sourceMeta{
		Provider:       docs.Provider,
		Repository:     src.Repo,
		Ref:            src.Ref,
		Path:           src.Path,
		ResolvedCommit: resolvedCommit,
	}
}

func writeJSON(w io.Writer, env envelope) error {
	env.SchemaVersion = envelopeSchemaVersion
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(env)
}

// emit writes a success payload as JSON (stdout) when requested.
func emitJSON(cmd *cobra.Command, command string, svc *docs.Service, offline bool, resolvedCommit string, data any) error {
	cs := svc.CacheState(offline)
	return writeJSON(cmd.OutOrStdout(), envelope{
		Command: command,
		Source:  sourceMetaOf(svc, resolvedCommit),
		Cache:   &cs,
		Data:    data,
	})
}

// failJSON writes an error envelope to stdout (keeping stdout JSON-only) and
// returns err so the process exits nonzero.
func failJSON(cmd *cobra.Command, command string, svc *docs.Service, offline bool, err error) error {
	code := docs.Code(err)
	if code == "" {
		code = "DOCS_ERROR"
	}
	var cs *docs.CacheState
	src := sourceMeta{Provider: docs.Provider}
	if svc != nil {
		state := svc.CacheState(offline)
		cs = &state
		src = sourceMetaOf(svc, "")
	}
	_ = writeJSON(cmd.OutOrStdout(), envelope{
		Command: command,
		Source:  src,
		Cache:   cs,
		Error:   &errMeta{Code: code, Message: err.Error()},
	})
	return err
}

// warnStale prints a stderr hint when serving a stale cache in human mode.
func warnStale(cmd *cobra.Command, svc *docs.Service, offline bool) {
	cs := svc.CacheState(offline)
	if cs.Stale && cs.SyncedAt != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), "Note: docs cache is stale; run `volcano docs sync` to refresh.")
	}
}
