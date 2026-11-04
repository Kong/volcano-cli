// Package issues performs the workflows behind the volcano issues
// subcommands. Volcano groups the runtime errors a project's functions and
// frontends log into issues, and people leave notes on an issue that
// LavaFlow's next run on it reads.
package issues

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clisession "github.com/Kong/volcano-cli/internal/session"
)

// ErrUnavailable reports an API without the issues routes: a platform
// release older than issues, which answers its router's bare 404.
var ErrUnavailable = errors.New("issues aren't available on this Volcano API")

// ErrNotesUnavailable reports an API without the issue notes routes.
var ErrNotesUnavailable = errors.New("issue notes aren't available on this Volcano API")

// ErrNotAvailable reports a route's feature_unavailable: issues aren't
// available in this environment yet.
var ErrNotAvailable = errors.New("issues aren't available in this Volcano environment yet")

// ErrWritesNotAvailable reports a note write's feature_unavailable: the
// issues.notes capability is unavailable, so notes can be read and deleted
// but not written.
var ErrWritesNotAvailable = errors.New("writing issue notes isn't available in this Volcano environment yet")

// ErrPersonWrites refuses writing a note with a project access token before
// anything is sent. A note is input to LavaFlow's next run, so only a person
// writes one.
var ErrPersonWrites = errors.New("writing issue notes needs a person's session: a project access token can't")

// NextRunReads is what adding or editing a note on an issue says, in the
// words every surface that writes notes uses.
const NextRunReads = "LavaFlow reads this note on the issue's next run."

// RunNoteNotRead is what adding or editing a note on a run says instead.
const RunNoteNotRead = "Later LavaFlow runs don't read notes on a run."

// Limits the API sets.
const (
	DefaultLimit = 25
	MaxLimit     = 100
)

// Codes of the API's refusals.
const (
	codeFeatureUnavailable = "feature_unavailable"
	codePersonRequired     = "person_required"
	codeNoteAuthorRequired = "note_author_required"
	codeNoteLimitReached   = "note_limit_reached"
	codeSize               = "size"
)

// textRules says what each of the API's text checks refused a note for. The
// API names the rule as the code and never quotes the text.
var textRules = map[string]string{
	"characters": "it holds a control, format, private-use or other invisible character",
	"size":       "it is over 2,048 bytes once normalized to Unicode NFKC",
	"credential": "it holds what looks like a credential, such as an API key or a token",
	"secret":     "it holds one of the project's secret values",
	"sanitizer":  "it holds text Volcano masks in logs",
	"link": "it holds a link or a line reference: a URL, a host name with //, user info, a port, a path " +
		"or a query, or a line such as cart.ts:42 or a stack frame. Name a line in words, such as \"cart.ts, line 42\"",
}

// ListQuery picks a page of issues. Zero values leave a filter out.
type ListQuery struct {
	Limit  int
	Cursor string
	Status string
}

// NotesQuery picks a page of an issue's notes. Zero values leave a filter
// out.
type NotesQuery struct {
	Limit  int
	Cursor string
	// Kind is issue for notes on the issue, which LavaFlow's runs read, or
	// run for notes on its runs.
	Kind string
}

// Service performs issue workflows against the current project.
type Service struct {
	deps     cliruntime.Deps
	sessions clisession.Factory
}

// NewService returns an issues service.
func NewService(deps cliruntime.Deps) Service {
	return Service{deps: deps, sessions: clisession.NewFactory(deps)}
}

// List returns one page of the current project's issues, most recently seen
// first.
func (s Service) List(ctx context.Context, query ListQuery) (*apiclient.PaginatedIssues, error) {
	if err := checkLimit(query.Limit); err != nil {
		return nil, err
	}
	params := apiclient.ListIssuesParams{Limit: &query.Limit}
	if query.Status != "" {
		status := apiclient.IssueStatus(query.Status)
		if !status.Valid() {
			return nil, fmt.Errorf("invalid --status %q: use open, resolved or ignored", query.Status)
		}
		params.Status = &status
	}
	if query.Cursor != "" {
		params.Cursor = &query.Cursor
	}
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	page, err := authenticated.API.ListIssues(ctx, authenticated.ProjectID, params)
	if err != nil {
		return nil, unavailable(fmt.Errorf("failed to list issues: %w", err), ErrUnavailable)
	}
	return page, nil
}

// Get returns one of the current project's issues.
func (s Service) Get(ctx context.Context, issueID string) (*apiclient.Issue, error) {
	id, err := s.issueID(issueID)
	if err != nil {
		return nil, err
	}
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	issue, err := authenticated.API.GetIssue(ctx, authenticated.ProjectID, id)
	if err != nil {
		return nil, unavailable(fmt.Errorf("failed to get issue %s: %w", issueID, err), ErrUnavailable)
	}
	return issue, nil
}

// Notes returns one page of an issue's notes and its runs' notes, newest
// first.
func (s Service) Notes(ctx context.Context, issueID string, query NotesQuery) (*apiclient.PaginatedIssueNotes, error) {
	id, err := s.issueID(issueID)
	if err != nil {
		return nil, err
	}
	if err := checkLimit(query.Limit); err != nil {
		return nil, err
	}
	params := apiclient.ListIssueNotesParams{IssueId: &id, Limit: &query.Limit}
	if query.Kind != "" {
		kind := apiclient.ListIssueNotesParamsKind(query.Kind)
		if !kind.Valid() {
			return nil, fmt.Errorf("invalid --kind %q: use issue or run", query.Kind)
		}
		params.Kind = &kind
	}
	if query.Cursor != "" {
		params.Cursor = &query.Cursor
	}
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	page, err := authenticated.API.ListIssueNotes(ctx, authenticated.ProjectID, params)
	if err != nil {
		return nil, unavailable(fmt.Errorf("failed to list the notes on issue %s: %w", issueID, err), ErrNotesUnavailable)
	}
	return page, nil
}

// AddNote writes a note on an issue, or with runID on one LavaFlow run of
// it. A project access token is refused before anything is sent.
func (s Service) AddNote(ctx context.Context, issueID, runID, text string) (*apiclient.IssueNote, error) {
	id, err := s.issueID(issueID)
	if err != nil {
		return nil, err
	}
	body := apiclient.IssueNoteCreate{IssueId: id, Body: text}
	if runID != "" {
		run, err := uuid.Parse(runID)
		if err != nil {
			return nil, fmt.Errorf("invalid --run %q: use a run ID from '%s'", runID,
				cliruntime.CommandPath(s.deps, "lavaflow list"))
		}
		body.RunId = &run
	}
	if err := checkText(text); err != nil {
		return nil, err
	}
	authenticated, err := s.person()
	if err != nil {
		return nil, err
	}
	note, err := authenticated.API.CreateIssueNote(ctx, authenticated.ProjectID, body)
	if err != nil {
		return nil, refused(authenticated.Config, "saved", fmt.Errorf("failed to add a note to issue %s: %w", issueID, err))
	}
	return note, nil
}

// EditNote replaces a note's text, which only its author can do. A project
// access token is refused before anything is sent.
func (s Service) EditNote(ctx context.Context, noteID, text string) (*apiclient.IssueNote, error) {
	id, err := s.noteID(noteID)
	if err != nil {
		return nil, err
	}
	if err := checkText(text); err != nil {
		return nil, err
	}
	authenticated, err := s.person()
	if err != nil {
		return nil, err
	}
	note, err := authenticated.API.UpdateIssueNote(ctx, authenticated.ProjectID, id, apiclient.IssueNoteUpdate{Body: text})
	if err != nil {
		return nil, refused(authenticated.Config, "saved", fmt.Errorf("failed to edit note %s: %w", noteID, err))
	}
	return note, nil
}

// DeleteNote deletes a note for good, which its author or the project's
// owner can do. A project access token is refused before confirm runs; an
// error from confirm stops the delete and is returned as is.
func (s Service) DeleteNote(ctx context.Context, noteID string, confirm func() error) error {
	id, err := s.noteID(noteID)
	if err != nil {
		return err
	}
	authenticated, err := s.person()
	if err != nil {
		return err
	}
	if err := confirm(); err != nil {
		return err
	}
	if err := authenticated.API.DeleteIssueNote(ctx, authenticated.ProjectID, id); err != nil {
		return refused(authenticated.Config, "deleted", fmt.Errorf("failed to delete note %s: %w", noteID, err))
	}
	return nil
}

func (s Service) issueID(issueID string) (uuid.UUID, error) {
	id, err := uuid.Parse(issueID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid issue ID %q: use an ID from '%s'", issueID,
			cliruntime.CommandPath(s.deps, "issues list"))
	}
	return id, nil
}

func (s Service) noteID(noteID string) (uuid.UUID, error) {
	id, err := uuid.Parse(noteID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid note ID %q: use an ID from '%s'", noteID,
			cliruntime.CommandPath(s.deps, "issues notes <issue-id>"))
	}
	return id, nil
}

// person returns the current project's session for a note write, refusing a
// project access token, which the API refuses too.
func (s Service) person() (*clisession.ProjectSession, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	if config.IsProjectToken(authenticated.Config.Token()) {
		how := "Sign in with 'volcano login' and run this again"
		if authenticated.Config.TokenFromEnv() {
			how = "Unset " + config.TokenEnv + " and sign in with 'volcano login', then run this again"
		}
		return nil, api.WithCause(fmt.Errorf("%w. %s", ErrPersonWrites, how), config.ErrAccountTokenRequired)
	}
	return authenticated, nil
}

func checkLimit(limit int) error {
	if limit < 1 || limit > MaxLimit {
		return fmt.Errorf("--limit must be between 1 and %d", MaxLimit)
	}
	return nil
}

// checkText refuses an empty note, which the API refuses too. Only the API
// can tell a note's size, which it counts once the text is normalized.
func checkText(text string) error {
	if strings.TrimSpace(text) == "" {
		return errors.New("the note is empty: pass its text with --text, or on stdin")
	}
	return nil
}

// refused says why the API refused a note write, by its code, keeping the
// refusal as the cause for the exit code. outcome is what didn't happen to
// the note: saved or deleted.
func refused(cfg *config.Config, outcome string, err error) error {
	code := api.Code(err)
	status := api.Status(err)
	if status == http.StatusRequestEntityTooLarge {
		// A request this large holds a note far over the size check's limit.
		code = codeSize
	}
	if reason, ok := textRules[code]; ok && (status == http.StatusBadRequest || code == codeSize) {
		return api.WithCause(fmt.Errorf("the note wasn't saved (%s): %s. Rephrase it and send it again", code, reason), err)
	}
	notDone := "the note wasn't " + outcome
	switch {
	case code == codePersonRequired:
		// Volcano says why in its own words, such as a token that never
		// expires, and how to write as a person instead.
		message := api.Message(err)
		if message == "" {
			message = "only the project's owner writes issue notes, as a person: " +
				"signed in with 'volcano login', or with a platform token that expires"
		}
		refusal := fmt.Errorf("%s: %s", notDone, message)
		if cfg.TokenFromEnv() {
			refusal = fmt.Errorf("%w. %s is set, and its token is used instead of a 'volcano login' sign-in "+
				"until you unset it", refusal, config.TokenEnv)
		}
		return api.WithCause(refusal, err)
	case code == codeNoteAuthorRequired:
		return api.WithCause(fmt.Errorf("%s: only the note's author can edit it, "+
			"and only its author or the project's owner can delete it", notDone), err)
	case code == codeNoteLimitReached:
		return api.WithCause(fmt.Errorf("%s: the issue or the project holds as many notes as it can; "+
			"delete notes to write more", notDone), err)
	case status == http.StatusForbidden && code == "":
		return api.WithCause(fmt.Errorf("%s: a read-only account or access token can't change notes", notDone), err)
	case status == http.StatusTooManyRequests:
		return rateLimited(time.Now(), notDone, err)
	}
	if code == codeFeatureUnavailable {
		return api.WithCause(ErrWritesNotAvailable, err)
	}
	return unavailable(err, ErrNotesUnavailable)
}

// rateLimited says the project wrote too many notes, and when to try again.
func rateLimited(now time.Time, notDone string, err error) error {
	refusal := notDone + ": the project wrote 30 notes or edits in the last minute"
	if at, ok := api.RetryAt(err, now); ok {
		if wait := at.Sub(now).Round(time.Second); wait > 0 {
			return api.WithCause(fmt.Errorf("%s. Try again in %s", refusal, wait), err)
		}
	}
	return api.WithCause(fmt.Errorf("%s. Try again in a minute", refusal), err)
}

// unavailable replaces a refusal that says issues aren't available with the
// error that says why, keeping the refusal for the exit code. missing is what
// a bare 404 from an API without the routes means.
func unavailable(err, missing error) error {
	switch {
	case api.Code(err) == codeFeatureUnavailable:
		return api.WithCause(ErrNotAvailable, err)
	case api.RouteMissing(err):
		return api.WithCause(missing, err)
	}
	return err
}
