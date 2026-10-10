package sandboxes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunSessionLifetime(t *testing.T) {
	t.Parallel()
	for _, local := range []bool{false, true} {
		mode := map[bool]string{false: "cloud", true: "local"}[local]
		for _, selector := range []string{"preset", "template"} {
			for _, duration := range []string{"", "-1", "0", "1", "29", "30", "300", "28800", "28801"} {
				t.Run(mode+"/"+selector+"/duration="+duration, func(t *testing.T) {
					t.Parallel()
					seconds, _ := strconv.Atoi(duration)
					valid := duration == "" || (local && seconds >= 0) || (!local && seconds >= 30 && seconds <= 28800)
					requests := 0
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						requests++
						assert.True(t, valid, "invalid duration must not reach transport")
						assert.Equal(t, http.MethodPost, r.Method)
						assert.Equal(t, "/projects/"+testProject+"/sandbox-sessions", r.URL.Path)
						var body map[string]any
						decoder := json.NewDecoder(r.Body)
						decoder.UseNumber()
						if err := decoder.Decode(&body); !assert.NoError(t, err) {
							w.WriteHeader(http.StatusBadRequest)
							return
						}
						if duration == "" {
							assert.NotContains(t, body, "max_duration_seconds", "inherit the template lifetime")
						} else {
							assert.Equal(t, json.Number(duration), body["max_duration_seconds"])
						}
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusCreated)
						_, _ = w.Write([]byte(`{"id":"` + testSession + `","expires_at":null}`))
					}))
					defer server.Close()
					deps := testDeps(server)
					cmd := New(deps)
					if local {
						// Local Sandbox APIs authenticate, so deps.LocalMode remains false.
						cmd = NewLocal(deps, deps)
					}
					value := "node22"
					if selector == "template" {
						value = testSession
					}
					args := []string{"run", "--" + selector, value}
					if duration != "" {
						args = append(args, "--duration", duration)
					}
					cmd.SetArgs(args)
					cmd.SetOut(&bytes.Buffer{})
					cmd.SetErr(&bytes.Buffer{})
					err := cmd.Execute()
					if valid {
						require.NoError(t, err)
						assert.Equal(t, 1, requests)
					} else {
						require.ErrorContains(t, err, "--duration")
						assert.Zero(t, requests)
					}
				})
			}
		}
	}
}
