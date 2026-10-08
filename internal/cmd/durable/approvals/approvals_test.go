package approvals

import (
	"bytes"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	projectID   = "22222222-2222-4222-8222-222222222222"
	approvalID  = "77777777-7777-4777-8777-777777777777"
	functionID  = "55555555-5555-4555-8555-555555555555"
	executionID = "66666666-6666-4666-8666-666666666666"
)

const approvalsPath = "/projects/" + projectID + "/durable-approvals"

func TestApprovalsListPopulatedAndEmpty(t *testing.T) {
	for _, tc := range []struct {
		name  string
		body  map[string]any
		query string
		args  []string
		want  []string
	}{
		{
			name: "pending by default",
			body: map[string]any{
				"data": []any{withFields(approvalPayload("pending"), map[string]any{
					"expires_at": time.Now().Add(3 * time.Hour).UTC().Format(time.RFC3339),
				})},
				"has_more": true,
				"page":     2,
				"limit":    5,
				"total":    12,
			},
			query: "page=2&limit=5&status=pending",
			args:  []string{"list", "--page", "2", "--limit", "5"},
			want: []string{
				approvalID,
				"Ship order 4417?",
				"order-pipeline",
				"order-4417",
				"pending",
				"in 2h",
				"Showing 1 of 12 approval(s) (page 2, limit 5)",
				"Next page: volcano cloud durable approvals list --page 3 --limit 5",
			},
		},
		// Offset 3 of the unfiltered set is not offset 3 of the filtered one, so
		// a hint that dropped a filter would page past rows the user asked for.
		{
			name: "filtered with more pages",
			body: map[string]any{
				"data":     []any{approvalPayload("approved")},
				"has_more": true,
				"page":     1,
				"limit":    1,
				"total":    4,
			},
			query: "page=1&limit=1&function=order-pipeline",
			args:  []string{"list", "--function", "order-pipeline", "--status", "all", "--limit", "1"},
			want: []string{
				"Next page: volcano cloud durable approvals list --function order-pipeline --status all --page 2 --limit 1",
			},
		},
		{
			name: "no pending approvals",
			body: map[string]any{
				"data":     []any{},
				"has_more": false,
				"page":     1,
				"limit":    100,
				"total":    0,
			},
			query: "page=1&limit=100&status=pending",
			args:  []string{"list"},
			want:  []string{"No pending approvals", "Showing 0 of 0 approval(s)"},
		},
		{
			name: "none at all",
			body: map[string]any{
				"data":     []any{},
				"has_more": false,
				"page":     1,
				"limit":    100,
				"total":    0,
			},
			query: "page=1&limit=100",
			args:  []string{"list", "--status", "all"},
			want:  []string{"No approvals found"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setApprovalsTestHome(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, approvalsPath, r.URL.Path)
				assert.ElementsMatch(t, splitQuery(tc.query), splitQuery(r.URL.RawQuery))
				respondJSON(t, w, http.StatusOK, tc.body)
			}))
			defer server.Close()

			out, err := executeCommand(t, newApprovalsCommand(server), tc.args...)
			require.NoError(t, err)
			for _, want := range tc.want {
				assert.Contains(t, out, want)
			}
		})
	}
}

// --since is a window a person thinks in, and the API takes the instant it
// starts at. A day suffix is what approval history is read in, so it has to
// work where time.ParseDuration has none.
func TestApprovalsListSendsEveryFilter(t *testing.T) {
	setApprovalsTestHome(t)
	var query map[string][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		respondJSON(t, w, http.StatusOK, emptyPage())
	}))
	defer server.Close()

	before := time.Now()
	_, err := executeCommand(t, newApprovalsCommand(server), "list",
		"--function", "order-pipeline", "--status", "Denied", "--execution", executionID, "--since", "7d")
	require.NoError(t, err)

	assert.Equal(t, []string{"order-pipeline"}, query["function"])
	assert.Equal(t, []string{"denied"}, query["status"])
	assert.Equal(t, []string{executionID}, query["execution_id"])
	require.Len(t, query["from"], 1)
	from, err := time.Parse(time.RFC3339Nano, query["from"][0])
	require.NoError(t, err)
	assert.WithinDuration(t, before.Add(-7*24*time.Hour), from, time.Minute)
	assert.NotContains(t, query, "to", "the API ends the window at its own now")
}

func TestApprovalsListWithoutSinceSendsNoWindow(t *testing.T) {
	setApprovalsTestHome(t)
	var query map[string][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		respondJSON(t, w, http.StatusOK, emptyPage())
	}))
	defer server.Close()

	_, err := executeCommand(t, newApprovalsCommand(server), "list")
	require.NoError(t, err)

	assert.NotContains(t, query, "from")
	assert.NotContains(t, query, "to")
}

func TestApprovalsListAcceptsEveryStatusTheContractDeclares(t *testing.T) {
	for _, status := range []string{"pending", "approved", "denied", "expired", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			setApprovalsTestHome(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, status, r.URL.Query().Get("status"))
				respondJSON(t, w, http.StatusOK, emptyPage())
			}))
			defer server.Close()

			_, err := executeCommand(t, newApprovalsCommand(server), "list", "--status", status)
			require.NoError(t, err)
		})
	}
}

// A value the flag does not take must be refused before any request, with the
// values it does take, rather than reaching the API as a filter it rejects.
func TestApprovalsListRefusesInvalidFlags(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "status",
			args: []string{"list", "--status", "waiting"},
			want: []string{`unknown approval status "waiting"`, "pending, approved, denied, expired, cancelled, all"},
		},
		{
			name: "execution",
			args: []string{"list", "--execution", "order-4417"},
			want: []string{`invalid execution ID "order-4417"`},
		},
		{
			name: "since",
			args: []string{"list", "--since", "a while"},
			want: []string{`invalid --since "a while"`, "30d, 24h, or 90m"},
		},
		{
			name: "zero since",
			args: []string{"list", "--since", "0d"},
			want: []string{`invalid --since "0d"`, "longer than zero"},
		},
		{
			name: "negative since",
			args: []string{"stats", "--since", "-1h"},
			want: []string{`invalid --since "-1h"`},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setApprovalsTestHome(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				t.Error("an invalid flag must not reach the API")
				http.NotFound(w, nil)
			}))
			defer server.Close()

			_, err := executeCommand(t, newApprovalsCommand(server), tc.args...)
			require.Error(t, err)
			for _, want := range tc.want {
				assert.ErrorContains(t, err, want)
			}
		})
	}
}

// --json is for scripts and agents, so every command prints the API's own
// object and nothing else on stdout.
func TestApprovalsJSONOutput(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		reply any
		check func(t *testing.T, stdout []byte)
	}{
		{
			name:  "list",
			args:  []string{"list", "--json"},
			reply: withFields(emptyPage(), map[string]any{"data": []any{approvalPayload("pending")}, "total": 1}),
			check: func(t *testing.T, stdout []byte) {
				t.Helper()
				var page apiclient.PaginatedDurableApprovals
				require.NoError(t, json.Unmarshal(stdout, &page))
				require.Len(t, page.Data, 1)
				assert.Equal(t, "Ship order 4417?", page.Data[0].Title)
			},
		},
		{
			name:  "get",
			args:  []string{"get", approvalID, "--json"},
			reply: approvalPayload("pending"),
			check: func(t *testing.T, stdout []byte) {
				t.Helper()
				var approval apiclient.DurableApproval
				require.NoError(t, json.Unmarshal(stdout, &approval))
				assert.Equal(t, apiclient.DurableApprovalStatusPending, approval.Status)
			},
		},
		{
			name:  "stats",
			args:  []string{"stats", "--json"},
			reply: statsPayload(),
			check: func(t *testing.T, stdout []byte) {
				t.Helper()
				var stats apiclient.DurableApprovalStats
				require.NoError(t, json.Unmarshal(stdout, &stats))
				assert.Equal(t, int64(2), stats.Counts.Approved)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setApprovalsTestHome(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				respondJSON(t, w, http.StatusOK, tc.reply)
			}))
			defer server.Close()

			stdout, _, err := executeCommandSplit(t, newApprovalsCommand(server), "", tc.args...)
			require.NoError(t, err)
			tc.check(t, stdout)
		})
	}
}

func TestApprovalsGetRendersTheApproval(t *testing.T) {
	for _, tc := range []struct {
		name        string
		payload     map[string]any
		want        []string
		wantMissing []string
	}{
		{
			name:    "pending",
			payload: approvalPayload("pending"),
			want: []string{
				"Title: Ship order 4417?",
				"Name: ship-order",
				"Status: pending",
				"Workflow: order-pipeline",
				"Execution: order-4417 (" + executionID + ", running)",
				"Description: Stock is reserved for 3 days.",
				"Details: {\n  \"items\": 3,\n  \"order\": 4417\n}",
				"Expires: when its execution ends",
			},
			wantMissing: []string{"Decided By:", "(deleted)"},
		},
		{
			name: "decided",
			payload: withFields(approvalPayload("approved"), map[string]any{
				"decision": decisionPayload("owner@example.com", "Checked stock"),
			}),
			want: []string{
				"Status: approved",
				"Decided By: owner@example.com",
				"Decided At:",
				"Comment: Checked stock",
			},
		},
		// A year of history outlives the workflow, the execution, and the person
		// who decided, and each has to read as gone rather than as blank.
		{
			name: "everything it names is gone",
			payload: withFields(approvalPayload("denied"), map[string]any{
				"function":  map[string]any{"id": nil, "name": "order-pipeline"},
				"execution": map[string]any{"id": nil, "name": "order-4417", "status": nil},
				"decision": map[string]any{
					"comment":    "",
					"decided_by": nil,
					"decided_at": "2026-10-06T12:00:00Z",
				},
			}),
			want: []string{
				"Workflow: order-pipeline (deleted)",
				"Execution: order-4417 (no longer retained)",
				"Decided By: a deleted user",
			},
			wantMissing: []string{"Comment:"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setApprovalsTestHome(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, approvalsPath+"/"+approvalID, r.URL.Path)
				respondJSON(t, w, http.StatusOK, tc.payload)
			}))
			defer server.Close()

			out, err := executeCommand(t, newApprovalsCommand(server), "get", approvalID)
			require.NoError(t, err)
			for _, want := range tc.want {
				assert.Contains(t, out, want)
			}
			for _, missing := range tc.wantMissing {
				assert.NotContains(t, out, missing)
			}
		})
	}
}

// A workflow writes the title, name, description, and details, and the names
// of itself and its execution, so human output must not hand a terminal the
// bytes it acts on. --json keeps the API's text: a script is not a terminal.
func TestApprovalsHumanOutputStripsControlCharacters(t *testing.T) {
	decided := withFields(hostileApprovalPayload("approved"), map[string]any{
		"decision": decisionPayload("owner\x1b[8m@example.com", "Checked\x1b[2K stock"),
	})
	for _, tc := range []struct {
		name    string
		args    []string
		current map[string]any
		want    []string
		wantErr string
	}{
		{
			name:    "get",
			args:    []string{"get", approvalID},
			current: decided,
			want: []string{
				"Title: Ship[2J order?",
				"Name: ship]52;c;eA==-order",
				"Description: Stockis reserved31m",
				`"note": "\u001b[31mred"`,
				"Workflow: order[1m-pipeline",
				"Execution: order[0m-4417 (",
				"Decided By: owner[8m@example.com",
				"Comment: Checked[2K stock",
			},
		},
		{
			name: "list",
			args: []string{"list"},
			want: []string{"Ship[2J order?", "order[1m-pipeline", "order[0m-4417"},
		},
		{
			name:    "approve",
			args:    []string{"approve", approvalID},
			current: hostileApprovalPayload("pending"),
			want: []string{
				`"Ship[2J order?" was requested by workflow order[1m-pipeline.`,
				`Approved "Ship[2J order?"`,
				"Decided By: owner[8m@example.com",
			},
		},
		{
			name:    "conflict",
			args:    []string{"deny", approvalID},
			current: decided,
			wantErr: "was already approved by owner[8m@example.com at ",
		},
		{
			name: "stats",
			args: []string{"stats"},
			want: []string{"order[1m-pipeline"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setApprovalsTestHome(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method + " " + r.URL.Path {
				case "GET " + approvalsPath:
					respondJSON(t, w, http.StatusOK, withFields(emptyPage(), map[string]any{
						"data": []any{hostileApprovalPayload("pending")}, "total": 1,
					}))
				case "GET " + approvalsPath + "/stats":
					respondJSON(t, w, http.StatusOK, withFields(statsPayload(), map[string]any{
						"functions": []any{map[string]any{
							"function": map[string]any{"id": functionID, "name": "order\x1b[1m-pipeline"},
							"counts":   countsPayload(1, 2, 1, 0, 0),
						}},
					}))
				case "GET " + approvalsPath + "/" + approvalID:
					respondJSON(t, w, http.StatusOK, tc.current)
				case "POST " + approvalsPath + "/" + approvalID + "/approve":
					respondJSON(t, w, http.StatusOK, decided)
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			out, err := executeCommand(t, newApprovalsCommand(server), tc.args...)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				requireNoControlCharacters(t, err.Error())
			} else {
				require.NoError(t, err)
			}
			for _, want := range tc.want {
				assert.Contains(t, out, want)
			}
			requireNoControlCharacters(t, out)
		})
	}
}

func TestApprovalsJSONKeepsTheWorkflowTextAsIs(t *testing.T) {
	setApprovalsTestHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		respondJSON(t, w, http.StatusOK, hostileApprovalPayload("pending"))
	}))
	defer server.Close()

	stdout, _, err := executeCommandSplit(t, newApprovalsCommand(server), "", "get", approvalID, "--json")
	require.NoError(t, err)
	var approval apiclient.DurableApproval
	require.NoError(t, json.Unmarshal(stdout, &approval))
	assert.Equal(t, "Ship\x1b[2J order\a?", approval.Title)
	assert.Equal(t, "order\x1b[0m-4417", approval.Execution.Name)
	assert.Equal(t, map[string]any{"note": "\x1b[31mred\u009b\u007f"}, approval.Details)
}

// The API answers 404 for an id that never existed and for one in another
// project alike, so the message says which project was searched.
func TestApprovalsNotFoundNamesTheProject(t *testing.T) {
	for _, args := range [][]string{
		{"get", approvalID},
		{"approve", approvalID, "--yes"},
		{"deny", approvalID, "--yes"},
	} {
		t.Run(args[0], func(t *testing.T) {
			setApprovalsTestHome(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				respondJSON(t, w, http.StatusNotFound, map[string]any{"error": "durable approval not found"})
			}))
			defer server.Close()

			_, err := executeCommand(t, newApprovalsCommand(server), args...)
			require.EqualError(t, err, "no approval "+approvalID+" in this project")
		})
	}
}

func TestApprovalsRefuseAMalformedID(t *testing.T) {
	for _, args := range [][]string{
		{"get", "ship-order"},
		{"approve", "ship-order", "--yes"},
		{"deny", "ship-order", "--yes"},
	} {
		t.Run(args[0], func(t *testing.T) {
			setApprovalsTestHome(t)
			_, err := executeCommand(t, newApprovalsCommand(nil), args...)
			require.ErrorContains(t, err, `invalid approval ID "ship-order"`)
		})
	}
}

func TestApprovalsDecideConfirmsWithTheTitle(t *testing.T) {
	for _, tc := range []struct {
		command  string
		decision string
		comment  string
		want     []string
	}{
		{
			command:  "approve",
			decision: "approved",
			comment:  "Checked stock",
			want:     []string{"Approve it?", "Status: approved", "Comment: Checked stock", `Approved "Ship order 4417?"`},
		},
		{
			command:  "deny",
			decision: "denied",
			comment:  "Customer cancelled",
			want:     []string{"Deny it?", "Status: denied", "Comment: Customer cancelled", `Denied "Ship order 4417?"`},
		},
	} {
		t.Run(tc.command, func(t *testing.T) {
			setApprovalsTestHome(t)
			var body map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method + " " + r.URL.Path {
				case "GET " + approvalsPath + "/" + approvalID:
					respondJSON(t, w, http.StatusOK, approvalPayload("pending"))
				case "POST " + approvalsPath + "/" + approvalID + "/" + tc.command:
					require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
					respondJSON(t, w, http.StatusOK, withFields(approvalPayload(tc.decision), map[string]any{
						"decision": decisionPayload("owner@example.com", tc.comment),
					}))
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			out, err := executeCommand(t, newApprovalsCommand(server), tc.command, approvalID, "--comment", tc.comment)
			require.NoError(t, err)
			assert.Equal(t, map[string]any{"comment": tc.comment}, body)
			assert.Contains(t, out, `"Ship order 4417?" was requested by workflow order-pipeline.`)
			for _, want := range tc.want {
				assert.Contains(t, out, want)
			}
		})
	}
}

func TestApprovalsDecideDeclinedLeavesTheApprovalAlone(t *testing.T) {
	setApprovalsTestHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Error("deny must not be called when the prompt is declined")
		}
		respondJSON(t, w, http.StatusOK, approvalPayload("pending"))
	}))
	defer server.Close()

	stdout, _, err := executeCommandSplit(t, newApprovalsCommand(server), "n\n", "deny", approvalID)
	require.NoError(t, err)
	assert.Contains(t, string(stdout), "Cancelled.")
}

// --yes is how a script decides, so it must not wait on a prompt, and no
// comment means no comment rather than an empty one invented on the way.
func TestApprovalsDecideWithYesSkipsThePrompt(t *testing.T) {
	setApprovalsTestHome(t)
	var raw []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			respondJSON(t, w, http.StatusOK, approvalPayload("pending"))
			return
		}
		var err error
		raw, err = io.ReadAll(r.Body)
		require.NoError(t, err)
		respondJSON(t, w, http.StatusOK, withFields(approvalPayload("approved"), map[string]any{
			"decision": decisionPayload("owner@example.com", ""),
		}))
	}))
	defer server.Close()

	stdout, _, err := executeCommandSplit(t, newApprovalsCommand(server), "", "approve", approvalID, "--yes")
	require.NoError(t, err)
	assert.JSONEq(t, `{}`, string(raw))
	assert.NotContains(t, string(stdout), "Approve it?")
	assert.Contains(t, string(stdout), "Status: approved")
}

// Under --json the prompt goes to stderr, so stdout is the approval alone even
// when a person answered a prompt on the way.
func TestApprovalsDecideJSONKeepsThePromptOffStdout(t *testing.T) {
	setApprovalsTestHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			respondJSON(t, w, http.StatusOK, approvalPayload("pending"))
			return
		}
		respondJSON(t, w, http.StatusOK, withFields(approvalPayload("approved"), map[string]any{
			"decision": decisionPayload("owner@example.com", ""),
		}))
	}))
	defer server.Close()

	stdout, stderr, err := executeCommandSplit(t, newApprovalsCommand(server), "y\n", "approve", approvalID, "--json")
	require.NoError(t, err)
	assert.Contains(t, string(stderr), "Approve it?")
	var approval apiclient.DurableApproval
	require.NoError(t, json.Unmarshal(stdout, &approval))
	assert.Equal(t, apiclient.DurableApprovalStatusApproved, approval.Status)
}

// The API accepts the same decision again unchanged, so a retried approve is
// safe. It is reported as already done rather than asked about again.
func TestApprovalsRepeatingTheDecisionChangesNothing(t *testing.T) {
	setApprovalsTestHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Error("an approval already decided this way needs no second decision")
		}
		respondJSON(t, w, http.StatusOK, withFields(approvalPayload("approved"), map[string]any{
			"decision": decisionPayload("owner@example.com", "Checked stock"),
		}))
	}))
	defer server.Close()

	out, err := executeCommand(t, newApprovalsCommand(server), "approve", approvalID)
	require.NoError(t, err)
	assert.NotContains(t, out, "Approve it?")
	assert.Contains(t, out, "Already approved by owner@example.com at ")
	assert.Contains(t, out, "nothing changed")
}

// A decision that can no longer be made says what happened instead, and fails,
// so a script does not read a denial that never happened as done.
func TestApprovalsConflictSaysWhatHappened(t *testing.T) {
	for _, tc := range []struct {
		name    string
		command string
		current map[string]any
		want    string
	}{
		{
			name:    "already approved",
			command: "deny",
			current: withFields(approvalPayload("approved"), map[string]any{
				"decision": decisionPayload("owner@example.com", ""),
			}),
			want: "approval " + approvalID + " was already approved by owner@example.com at ",
		},
		{
			name:    "already denied",
			command: "approve",
			current: withFields(approvalPayload("denied"), map[string]any{
				"decision": decisionPayload("teammate@example.com", ""),
			}),
			want: "approval " + approvalID + " was already denied by teammate@example.com at ",
		},
		{
			name:    "expired",
			command: "approve",
			current: withFields(approvalPayload("expired"), map[string]any{"expires_at": "2026-10-06T12:00:00Z"}),
			want:    "approval " + approvalID + " expired at ",
		},
		{
			name:    "cancelled",
			command: "deny",
			current: approvalPayload("cancelled"),
			want:    "approval " + approvalID + " was cancelled: its execution ended before anyone decided",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setApprovalsTestHome(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Error("a decision that can no longer be made must not be sent")
				}
				respondJSON(t, w, http.StatusOK, tc.current)
			}))
			defer server.Close()

			out, err := executeCommand(t, newApprovalsCommand(server), tc.command, approvalID)
			require.ErrorContains(t, err, tc.want)
			assert.NotContains(t, out, "it?")
		})
	}
}

// Someone else can decide between the read and the write. The API's 409 does
// not say who, so the approval is read again to tell the person what won.
func TestApprovalsConflictFromTheAPIReadsTheApprovalAgain(t *testing.T) {
	setApprovalsTestHome(t)
	reads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			respondJSON(t, w, http.StatusConflict, map[string]any{
				"error": "approval already decided", "code": "approval_decided",
			})
			return
		}
		reads++
		if reads == 1 {
			respondJSON(t, w, http.StatusOK, approvalPayload("pending"))
			return
		}
		respondJSON(t, w, http.StatusOK, withFields(approvalPayload("denied"), map[string]any{
			"decision": decisionPayload("teammate@example.com", ""),
		}))
	}))
	defer server.Close()

	_, err := executeCommand(t, newApprovalsCommand(server), "approve", approvalID, "--yes")
	require.ErrorContains(t, err, "was already denied by teammate@example.com at ")
	assert.Equal(t, 2, reads)
}

// The deadline can pass while the person reads the prompt. The approval then
// still reads pending, and the API's 409 is answered as the expiry it is.
func TestApprovalsDeadlinePassingBeforeTheDecisionReadsAsExpired(t *testing.T) {
	setApprovalsTestHome(t)
	reads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			respondJSON(t, w, http.StatusConflict, map[string]any{
				"error": "approval expired", "code": "approval_expired",
			})
			return
		}
		reads++
		expiresAt := time.Now().Add(time.Hour)
		if reads > 1 {
			expiresAt = time.Now().Add(-time.Second)
		}
		respondJSON(t, w, http.StatusOK, withFields(approvalPayload("pending"), map[string]any{
			"expires_at": expiresAt.UTC().Format(time.RFC3339Nano),
		}))
	}))
	defer server.Close()

	out, err := executeCommand(t, newApprovalsCommand(server), "deny", approvalID)
	require.ErrorContains(t, err, "approval "+approvalID+" expired at ")
	assert.NotContains(t, err.Error(), "409")
	assert.Contains(t, out, "Deny it?")
	assert.Equal(t, 2, reads)
}

// A pending approval whose deadline this machine's clock has passed is still
// the API's to refuse: a clock running fast must not turn away a decision the
// API would take.
func TestApprovalsPendingPastTheLocalDeadlineIsLeftToTheAPI(t *testing.T) {
	pastDeadline := withFields(approvalPayload("pending"), map[string]any{
		"expires_at": time.Now().Add(-time.Minute).UTC().Format(time.RFC3339),
	})
	for _, tc := range []struct {
		name    string
		status  int
		reply   map[string]any
		wantErr string
		want    string
	}{
		{
			name:   "the API takes it",
			status: http.StatusOK,
			reply: withFields(approvalPayload("approved"), map[string]any{
				"decision": decisionPayload("owner@example.com", ""),
			}),
			want: `Approved "Ship order 4417?"`,
		},
		{
			name:    "the API refuses it as expired",
			status:  http.StatusConflict,
			reply:   map[string]any{"error": "approval expired", "code": "approval_expired"},
			wantErr: "approval " + approvalID + " expired at ",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setApprovalsTestHome(t)
			decisions := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method + " " + r.URL.Path {
				case "GET " + approvalsPath + "/" + approvalID:
					respondJSON(t, w, http.StatusOK, pastDeadline)
				case "POST " + approvalsPath + "/" + approvalID + "/approve":
					decisions++
					respondJSON(t, w, tc.status, tc.reply)
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			out, err := executeCommand(t, newApprovalsCommand(server), "approve", approvalID, "--yes")
			assert.Equal(t, 1, decisions, "the decision must reach the API")
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				assert.NotContains(t, err.Error(), "409")
				return
			}
			require.NoError(t, err)
			assert.Contains(t, out, tc.want)
		})
	}
}

// The API's own refusals, word for word. A read-only token is refused before
// the decision check gets to it.
const (
	refusedDecision = "project access tokens cannot decide durable approvals; " +
		"a person decides in the dashboard or with a platform token"
	refusedReadOnly = "project access token is read-only"
)

// Only a person decides. A project access token is refused by the API, and the
// person reading the refusal is told how to decide instead. The message alone
// is enough, for a credential the CLI does not recognize as a project token.
func TestApprovalsDecideRefusesAProjectAccessToken(t *testing.T) {
	for _, tc := range []struct {
		name    string
		token   string
		message string
	}{
		{name: "full-scope project token", token: "pt-approvals-test", message: refusedDecision},
		{name: "read-only project token", token: "pt-approvals-test", message: refusedReadOnly},
		{name: "decision refusal on any credential", token: "", message: refusedDecision},
		{name: "read-only refusal on any credential", token: "", message: refusedReadOnly},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setApprovalsTestHome(t)
			api.ResetLastRefusalForTest()
			t.Cleanup(api.ResetLastRefusalForTest)
			if tc.token != "" {
				t.Setenv("VOLCANO_TOKEN", tc.token)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					respondJSON(t, w, http.StatusOK, approvalPayload("pending"))
					return
				}
				respondJSON(t, w, http.StatusForbidden, map[string]any{"error": tc.message})
			}))
			defer server.Close()

			_, err := executeCommand(t, newApprovalsCommand(server), "approve", approvalID, "--yes")
			require.EqualError(t, err, needsAPerson)
		})
	}
}

// A credential the CLI knows is a project access token is refused before the
// prompt: confirming a decision the API will refuse wastes the person's yes,
// and sends an agent after --yes first.
func TestApprovalsDecideRefusesAProjectTokenBeforeConfirming(t *testing.T) {
	for _, args := range [][]string{{"approve", approvalID}, {"deny", approvalID, "--yes"}} {
		t.Run(args[0], func(t *testing.T) {
			setApprovalsTestHome(t)
			t.Setenv("VOLCANO_TOKEN", "PT-approvals-test")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Error("a project access token's decision must not reach the API")
				}
				respondJSON(t, w, http.StatusOK, approvalPayload("pending"))
			}))
			defer server.Close()

			out, err := executeCommand(t, newApprovalsCommand(server), args...)
			require.EqualError(t, err, needsAPerson)
			assert.NotContains(t, out, "Approve it?")
		})
	}
}

const needsAPerson = "approvals are decided by a person. " +
	"Run `volcano login` with VOLCANO_TOKEN unset, or decide in the dashboard"

// Any other refusal keeps the API's own reason: telling a person who is
// already logged in to log in would send them after the wrong thing.
func TestApprovalsDecideKeepsOtherRefusals(t *testing.T) {
	setApprovalsTestHome(t)
	api.ResetLastRefusalForTest()
	t.Cleanup(api.ResetLastRefusalForTest)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			respondJSON(t, w, http.StatusOK, approvalPayload("pending"))
			return
		}
		respondJSON(t, w, http.StatusForbidden, map[string]any{"error": "forbidden"})
	}))
	defer server.Close()

	_, err := executeCommand(t, newApprovalsCommand(server), "deny", approvalID, "--yes")
	require.ErrorContains(t, err, "failed to deny approval "+approvalID+": HTTP 403: forbidden")
}

func TestApprovalsStatsRendersTheWindow(t *testing.T) {
	setApprovalsTestHome(t)
	var query map[string][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, approvalsPath+"/stats", r.URL.Path)
		query = r.URL.Query()
		respondJSON(t, w, http.StatusOK, statsPayload())
	}))
	defer server.Close()

	before := time.Now()
	out, err := executeCommand(t, newApprovalsCommand(server), "stats", "--function", "order-pipeline")
	require.NoError(t, err)

	assert.Equal(t, []string{"order-pipeline"}, query["function"])
	from, to := windowSent(t, query)
	assert.WithinDuration(t, before, to, time.Minute)
	assert.Equal(t, 30*24*time.Hour, to.Sub(from), "--since defaults to 30d")

	for _, want := range []string{
		"Requested: 5",
		"Pending: 1",
		"Approved: 2",
		"Denied: 1",
		"Expired: 1",
		"Cancelled: 0",
		"Approval Rate: 67%",
		"Median Time to Decision: 13s",
		"order-pipeline",
		"(other workflows)",
	} {
		assert.Contains(t, out, want)
	}
}

// The API refuses a window over 366 days and measures a missing end from its
// own clock, which runs later than the one the start was measured from. The
// window is sent with both ends so the longest one the API takes still fits.
func TestApprovalsStatsSendsTheLongestWindowWhole(t *testing.T) {
	setApprovalsTestHome(t)
	var query map[string][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		respondJSON(t, w, http.StatusOK, statsPayload())
	}))
	defer server.Close()

	_, err := executeCommand(t, newApprovalsCommand(server), "stats", "--since", "366d")
	require.NoError(t, err)
	from, to := windowSent(t, query)
	assert.Equal(t, 366*24*time.Hour, to.Sub(from))
}

// The API owns the longest window it counts, so a longer one is its refusal
// to explain, and a CLI already released follows when the limit changes.
func TestApprovalsStatsLeavesTheWindowLimitToTheAPI(t *testing.T) {
	setApprovalsTestHome(t)
	var query map[string][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		respondJSON(t, w, http.StatusBadRequest, map[string]any{"error": "the window can span at most 366 days"})
	}))
	defer server.Close()

	_, err := executeCommand(t, newApprovalsCommand(server), "stats", "--since", "367d")
	require.ErrorContains(t, err, "the window can span at most 366 days")
	from, to := windowSent(t, query)
	assert.Equal(t, 367*24*time.Hour, to.Sub(from))
}

// Nothing decided in the window is not a rate of zero, and must not read as
// one.
func TestApprovalsStatsWithNothingDecided(t *testing.T) {
	setApprovalsTestHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		respondJSON(t, w, http.StatusOK, withFields(statsPayload(), map[string]any{
			"approval_rate":              nil,
			"median_seconds_to_decision": nil,
			"p90_seconds_to_decision":    nil,
			"functions":                  []any{},
			"other_functions":            countsPayload(0, 0, 0, 0, 0),
		}))
	}))
	defer server.Close()

	out, err := executeCommand(t, newApprovalsCommand(server), "stats", "--since", "1h")
	require.NoError(t, err)
	assert.Contains(t, out, "Approval Rate: -")
	assert.Contains(t, out, "Median Time to Decision: -")
	assert.NotContains(t, out, "Workflow ")
}

func windowSent(t *testing.T, query map[string][]string) (time.Time, time.Time) {
	t.Helper()
	require.Len(t, query["from"], 1)
	require.Len(t, query["to"], 1)
	from, err := time.Parse(time.RFC3339Nano, query["from"][0])
	require.NoError(t, err)
	to, err := time.Parse(time.RFC3339Nano, query["to"][0])
	require.NoError(t, err)
	return from, to
}

func newApprovalsCommand(server *httptest.Server) *cobra.Command {
	deps := cliruntime.Deps{CommandPathPrefix: "volcano cloud"}
	if server != nil {
		deps.HTTPClient = server.Client()
		deps.APIBaseURL = server.URL
	}
	return New(deps)
}

func executeCommand(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(bytes.NewBufferString("y\n"))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func executeCommandSplit(t *testing.T, cmd *cobra.Command, stdin string, args ...string) ([]byte, []byte, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetIn(bytes.NewBufferString(stdin))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.Bytes(), stderr.Bytes(), err
}

func setApprovalsTestHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "")
	t.Setenv("VOLCANO_PROJECT_ID", "")
	t.Setenv("VOLCANO_API_URL", "")
	t.Setenv("VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID", "")

	cfg := &cliconfig.Config{
		UserToken:      "token",
		CurrentProject: &cliconfig.ProjectConfig{ID: projectID, Name: "Beta"},
	}
	require.NoError(t, cfg.Save())
}

func respondJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	require.NoError(t, json.NewEncoder(w).Encode(value))
}

func approvalPayload(status string) map[string]any {
	return map[string]any{
		"id":           approvalID,
		"status":       status,
		"name":         "ship-order",
		"title":        "Ship order 4417?",
		"description":  "Stock is reserved for 3 days.",
		"details":      map[string]any{"order": 4417, "items": 3},
		"function":     map[string]any{"id": functionID, "name": "order-pipeline"},
		"execution":    map[string]any{"id": executionID, "name": "order-4417", "status": "running"},
		"requested_at": "2026-10-06T11:00:00Z",
		"expires_at":   nil,
		"decision":     nil,
	}
}

// hostileApprovalPayload carries terminal escapes in every field a workflow
// writes: C0 sequences, a C1 CSI, DEL, and a line break.
func hostileApprovalPayload(status string) map[string]any {
	return withFields(approvalPayload(status), map[string]any{
		"title":       "Ship\x1b[2J order\a?",
		"name":        "ship\x1b]52;c;eA==\x07-order",
		"description": "Stock\r\nis reserved\u009b31m",
		"details":     map[string]any{"note": "\x1b[31mred\u009b\u007f"},
		"function":    map[string]any{"id": functionID, "name": "order\x1b[1m-pipeline"},
		"execution":   map[string]any{"id": executionID, "name": "order\x1b[0m-4417", "status": "running"},
	})
}

// requireNoControlCharacters allows only the line breaks the renderer writes.
func requireNoControlCharacters(t *testing.T, text string) {
	t.Helper()
	for _, r := range text {
		if r != '\n' && (r < 0x20 || (r >= 0x7f && r <= 0x9f)) {
			require.Failf(t, "control character in human output", "%U in %q", r, text)
		}
	}
}

func decisionPayload(email, comment string) map[string]any {
	return map[string]any{
		"comment":    comment,
		"decided_by": map[string]any{"id": "user-1", "email": email},
		"decided_at": "2026-10-06T12:00:00Z",
	}
}

func statsPayload() map[string]any {
	return map[string]any{
		"from":                       "2026-09-06T12:00:00Z",
		"to":                         "2026-10-06T12:00:00Z",
		"counts":                     countsPayload(1, 2, 1, 1, 0),
		"approval_rate":              2.0 / 3.0,
		"median_seconds_to_decision": 12.5,
		"p90_seconds_to_decision":    30,
		"functions": []any{map[string]any{
			"function": map[string]any{"id": functionID, "name": "order-pipeline"},
			"counts":   countsPayload(1, 2, 1, 0, 0),
		}},
		"other_functions": countsPayload(0, 0, 0, 1, 0),
		"daily":           []any{},
	}
}

func countsPayload(pending, approved, denied, expired, cancelled int) map[string]any {
	return map[string]any{
		"requested": pending + approved + denied + expired + cancelled,
		"pending":   pending,
		"approved":  approved,
		"denied":    denied,
		"expired":   expired,
		"cancelled": cancelled,
	}
}

func emptyPage() map[string]any {
	return map[string]any{"data": []any{}, "page": 1, "limit": 100, "total": 0, "has_more": false}
}

func withFields(payload, fields map[string]any) map[string]any {
	maps.Copy(payload, fields)
	return payload
}

func splitQuery(query string) []string {
	if query == "" {
		return nil
	}
	return strings.Split(query, "&")
}
