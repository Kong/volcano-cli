package api

import (
	"net/http"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/config"
)

// Refusal describes the request behind the most recent 403 in this process.
//
// It is read from the request as it goes out rather than resolved from the
// configuration afterwards, because the two can disagree: a command that takes
// a project ID as an argument addresses a project the configuration never
// selected, and a local-mode command sends no credential at all. A caller
// explaining a refusal has to describe what was sent.
//
// Recorded process-wide instead of carried on Error for the same reason
// LastInstructions is: the error is built from a status and a body, neither of
// which names the request, and a CLI process runs exactly one command — so
// "most recent" is "this command's".
type Refusal struct {
	// ProjectID is the project the refused request addressed, empty when its
	// route was not project-scoped.
	ProjectID string
	// ProjectAccessToken reports whether the refused request carried a pt-
	// credential.
	ProjectAccessToken bool
}

var (
	refusalMu   sync.RWMutex
	lastRefusal Refusal
)

// LastRefusal returns the request behind the most recent 403 observed on any
// API response in this process. It is the zero value until one is refused.
func LastRefusal() Refusal {
	refusalMu.RLock()
	defer refusalMu.RUnlock()
	return lastRefusal
}

// ResetLastRefusalForTest clears the process-global refusal state. Test-only:
// each CLI invocation is a fresh process.
func ResetLastRefusalForTest() {
	refusalMu.Lock()
	lastRefusal = Refusal{}
	refusalMu.Unlock()
}

func recordRefusal(req *http.Request) {
	refusal := Refusal{
		ProjectID:          requestProjectID(req.URL.Path),
		ProjectAccessToken: config.IsProjectToken(bearerToken(req.Header.Get("Authorization"))),
	}

	refusalMu.Lock()
	lastRefusal = refusal
	refusalMu.Unlock()
}

// requestProjectID returns the project a /projects/{id}/... path addresses.
// The ID has to parse as a UUID so the project collection itself, and the
// routes that take a name, are not mistaken for one.
func requestProjectID(path string) string {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	for i, segment := range segments {
		if segment != "projects" || i+1 >= len(segments) {
			continue
		}
		if id, err := uuid.Parse(segments[i+1]); err == nil {
			return id.String()
		}
	}
	return ""
}

func bearerToken(authorization string) string {
	token, ok := strings.CutPrefix(strings.TrimSpace(authorization), "Bearer ")
	if !ok {
		return ""
	}
	return strings.TrimSpace(token)
}

// refusalDoer records the request behind every 403 the API returns, so a
// caller explaining one can describe the request instead of guessing at it.
type refusalDoer struct {
	next apiclient.HttpRequestDoer
}

func (d refusalDoer) Do(req *http.Request) (*http.Response, error) {
	resp, err := d.next.Do(req)
	if resp != nil && resp.StatusCode == http.StatusForbidden {
		recordRefusal(req)
	}
	return resp, err
}
