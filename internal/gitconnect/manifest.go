package gitconnect

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/config"
)

var (
	// ErrManifestSectionsNeedAPerson refuses choosing what a push applies with
	// a project access token. Opting a section in lets anyone who can push to
	// the production branch change it, so the API takes the choice from a
	// person only.
	ErrManifestSectionsNeedAPerson = errors.New("choosing what a push applies is a person's decision: " +
		"a project access token can't")
	// ErrManifestSectionsUnavailable says this API cannot apply
	// volcano-config.yaml sections on push: a platform release older than
	// this CLI.
	ErrManifestSectionsUnavailable = errors.New("this Volcano API can't apply volcano-config.yaml sections on push yet")
)

// codePersonRequired is the API's code for a change only a person may make.
const codePersonRequired = "person_required"

// RequirePerson refuses a credential the CLI knows is a project access token,
// before anything is asked or sent. SetManifestSections still maps the API's
// refusal, for a credential without a recognizable prefix.
func (s Service) RequirePerson() error {
	authenticated, err := s.current()
	if err != nil {
		return err
	}
	if config.IsProjectToken(authenticated.Config.Token()) {
		return personRequired(authenticated.Config, config.ErrAccountTokenRequired)
	}
	return nil
}

// personRequired says how a person makes the change instead. A token in
// VOLCANO_TOKEN wins over a sign-in, so that case names the variable to unset.
// cause keeps the exit code of a refused credential.
func personRequired(cfg *config.Config, cause error) error {
	how := "Sign in with 'volcano login' and run this again"
	if cfg.TokenFromEnv() {
		how = "Unset " + config.TokenEnv + " and sign in with 'volcano login', then run this again"
	}
	return api.WithCause(fmt.Errorf("%w. %s", ErrManifestSectionsNeedAPerson, how), cause)
}

// SetManifestSections replaces the volcano-config.yaml sections a push to the
// production branch applies once its deploys succeed. No sections applies
// none.
func (s Service) SetManifestSections(ctx context.Context, sections []string) (*apiclient.ProjectGitDeploySettings, error) {
	authenticated, err := s.current()
	if err != nil {
		return nil, err
	}

	chosen := make([]apiclient.ProjectGitManifestSection, 0, len(sections))
	for _, section := range sections {
		chosen = append(chosen, apiclient.ProjectGitManifestSection(section))
	}
	settings, err := authenticated.API.SetProjectGitManifestSections(ctx, authenticated.ProjectID, chosen)
	if err == nil {
		return settings, nil
	}
	switch {
	case api.RouteMissing(err):
		return nil, api.WithCause(ErrManifestSectionsUnavailable, err)
	case api.Status(err) == http.StatusForbidden:
		return nil, manifestSectionsRefused(authenticated.Config, err)
	}
	return nil, settingsError(authenticated.ProjectID, err, "failed to set the manifest sections")
}

// manifestSectionsRefused explains a 403 on choosing the sections. A request
// that carried a project access token, or the API's words for refusing one,
// gets the CLI's own answer. Otherwise person_required keeps Volcano's words,
// which say what kind of sign-in it wants, and any other 403 is the API's.
func manifestSectionsRefused(cfg *config.Config, err error) error {
	message := api.Message(err)
	switch {
	case api.LastRefusal().ProjectAccessToken, refusesProjectToken(message):
		return personRequired(cfg, err)
	case api.Code(err) == codePersonRequired && message != "":
		return api.WithCause(fmt.Errorf("the manifest sections weren't changed: %s", message), err)
	case api.Code(err) == codePersonRequired:
		return personRequired(cfg, err)
	}
	return classify(err, "failed to set the manifest sections")
}

// refusesProjectToken reports whether a 403 message is the API refusing a
// project access token here. A read-only token is refused as read-only before
// the decision check, but a full one would be refused next, so both get the
// same answer.
func refusesProjectToken(message string) bool {
	return strings.Contains(message, "project access tokens cannot choose what a push applies") ||
		strings.Contains(message, "project access token is read-only")
}
