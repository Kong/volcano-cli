package output

import (
	"bytes"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/config"
)

const outputProjectID = "11111111-1111-4111-8111-111111111111"

func TestSuccess(t *testing.T) {
	var out bytes.Buffer
	Success(&out, "Saved %s", "project")
	assert.Equal(t, "✓ Saved project\n", out.String())
}

func TestWarning(t *testing.T) {
	var out bytes.Buffer
	Warning(&out, "failed to save %s", "project")
	assert.Equal(t, "Warning: failed to save project\n", out.String())
}

func TestNote(t *testing.T) {
	var out bytes.Buffer
	Note(&out, "skipped %s", "database")
	assert.Equal(t, "Note: skipped database\n", out.String())
}

func TestProjectsOutput(t *testing.T) {
	projectID, err := uuid.Parse(outputProjectID)
	require.NoError(t, err)
	var out bytes.Buffer
	plan := apiclient.ProjectPlan("FREE")

	Projects(&out, &config.Config{
		CurrentProject: &config.ProjectConfig{
			ID:   outputProjectID,
			Name: "Alpha",
		},
	}, &apiclient.PaginatedProjects{
		Data: []apiclient.Project{
			{
				Id:        projectID,
				Name:      "Alpha",
				Status:    apiclient.ProjectStatusActive,
				Plan:      &plan,
				CreatedAt: time.Now().Add(-2 * time.Hour),
				UpdatedAt: time.Now().Add(-30 * time.Minute),
			},
		},
		HasMore: true,
		Page:    2,
		Limit:   25,
		Total:   51,
	})

	for _, want := range []string{"ID", "Name", "Status", "Plan", outputProjectID, "Alpha", "active", "FREE", "Showing 1 of 51 project(s) (page 2, limit 25)", "Next page: volcano projects --page 3 --limit 25", "Current project: Alpha (" + outputProjectID + ")"} {
		assert.Contains(t, out.String(), want)
	}
}

func TestProjectsEmptyOutput(t *testing.T) {
	var out bytes.Buffer
	Projects(&out, config.Default(), &apiclient.PaginatedProjects{
		Page:  1,
		Limit: 100,
		Total: 0,
	})
	assert.Contains(t, out.String(), "No projects found")
	assert.Contains(t, out.String(), "https://volcano.dev")
	assert.Contains(t, out.String(), "Showing 0 of 0 project(s) (page 1, limit 100)")
}

func TestProjectOutput(t *testing.T) {
	projectID, err := uuid.Parse(outputProjectID)
	require.NoError(t, err)
	var out bytes.Buffer
	plan := apiclient.ProjectPlan("PRO")

	Project(&out, &apiclient.Project{
		Id:     projectID,
		Name:   "Alpha",
		Status: apiclient.ProjectStatusActive,
		Plan:   &plan,
	})

	assert.Equal(t, "ID:     "+outputProjectID+"\nName:   Alpha\nStatus: active\nPlan:   PRO\nCreated: -\nUpdated: -\n", out.String())
}

func TestDatabasesHideConnectionStringsByDefault(t *testing.T) {
	databaseID, err := uuid.Parse("33333333-3333-4333-8333-333333333333")
	require.NoError(t, err)
	connectionString := "postgres://example"
	var out bytes.Buffer

	Databases(&out, &apiclient.PaginatedDatabases{
		Data: []apiclient.Database{
			{
				Id:               databaseID,
				Name:             "app",
				Status:           apiclient.DatabaseStatusActive,
				ConnectionString: &connectionString,
			},
		},
		Page:  1,
		Limit: 100,
		Total: 1,
	}, false)

	assert.Contains(t, out.String(), "app")
	assert.NotContains(t, out.String(), connectionString)
}

func TestDatabasesShowConnectionStringsWhenRequested(t *testing.T) {
	databaseID, err := uuid.Parse("33333333-3333-4333-8333-333333333333")
	require.NoError(t, err)
	connectionString := "postgres://example"
	var out bytes.Buffer

	Databases(&out, &apiclient.PaginatedDatabases{
		Data: []apiclient.Database{
			{
				Id:               databaseID,
				Name:             "app",
				Status:           apiclient.DatabaseStatusActive,
				ConnectionString: &connectionString,
			},
		},
		Page:  1,
		Limit: 100,
		Total: 1,
	}, true)

	assert.Contains(t, out.String(), connectionString)
}

func TestDatabaseOutputHonorsConnectionStringFlag(t *testing.T) {
	databaseID, err := uuid.Parse("33333333-3333-4333-8333-333333333333")
	require.NoError(t, err)
	connectionString := "postgres://example"
	database := &apiclient.Database{
		Id:               databaseID,
		Name:             "app",
		Status:           apiclient.DatabaseStatusActive,
		ConnectionString: &connectionString,
	}

	var hidden bytes.Buffer
	Database(&hidden, database, false)
	assert.NotContains(t, hidden.String(), connectionString)

	var shown bytes.Buffer
	Database(&shown, database, true)
	assert.Contains(t, shown.String(), connectionString)
}

func TestVariablesOutputDoesNotPrintValues(t *testing.T) {
	variableID, err := uuid.Parse("33333333-3333-4333-8333-333333333333")
	require.NoError(t, err)
	projectID, err := uuid.Parse(outputProjectID)
	require.NoError(t, err)
	status := apiclient.VariableStatusActive
	var out bytes.Buffer

	Variables(&out, &apiclient.PaginatedVariables{
		Data: []apiclient.Variable{
			{
				Id:        variableID,
				ProjectId: projectID,
				Name:      "API_KEY",
				Value:     "abcdefghijklmnopqrstuvwxyz",
				Status:    &status,
			},
		},
		HasMore: true,
		Page:    2,
		Limit:   25,
		Total:   51,
	})

	assert.Contains(t, out.String(), "API_KEY")
	assert.Contains(t, out.String(), "active")
	assert.NotContains(t, out.String(), "Value")
	assert.NotContains(t, out.String(), "abcdefghijklmnopq...")
	assert.NotContains(t, out.String(), "abcdefghijklmnopqrstuvwxyz")
	assert.Contains(t, out.String(), "Showing 1 of 51 variable(s) (page 2, limit 25)")
	assert.Contains(t, out.String(), "Next page: volcano variables list --page 3 --limit 25")
}

func TestVariableOutputDoesNotPrintValue(t *testing.T) {
	variableID, err := uuid.Parse("33333333-3333-4333-8333-333333333333")
	require.NoError(t, err)
	var out bytes.Buffer

	Variable(&out, &apiclient.Variable{
		Id:    variableID,
		Name:  "API_KEY",
		Value: "secret-value",
	})

	assert.Contains(t, out.String(), "ID: 33333333-3333-4333-8333-333333333333")
	assert.Contains(t, out.String(), "Name: API_KEY")
	assert.NotContains(t, out.String(), "Value:")
	assert.NotContains(t, out.String(), "secret-value")
}

func TestTruncate(t *testing.T) {
	assert.Equal(t, "short", Truncate("short", 20))
	assert.Equal(t, "seventeen-char...", Truncate("seventeen-character-name", 17))
	assert.Equal(t, "ab", Truncate("abcd", 2))
}

// A queued deployment runs after the one in flight, so the detail views name it
// rather than leaving the resource looking idle. It only exists while something
// is waiting, which is why it is not a permanent row.
func TestFunctionShowsQueuedDeploymentOnlyWhenOneIsWaiting(t *testing.T) {
	functionID, err := uuid.Parse("44444444-4444-4444-8444-444444444444")
	require.NoError(t, err)
	pendingID, err := uuid.Parse("55555555-5555-4555-8555-555555555555")
	require.NoError(t, err)
	fn := apiclient.Function{
		Id:                  functionID,
		Name:                "worker",
		Status:              apiclient.FunctionStatusActive,
		PendingDeploymentId: &pendingID,
	}

	var queued bytes.Buffer
	Function(&queued, &fn)
	assert.Contains(t, queued.String(), "Pending Deployment: "+pendingID.String())

	fn.PendingDeploymentId = nil
	var idle bytes.Buffer
	Function(&idle, &fn)
	assert.NotContains(t, idle.String(), "Pending Deployment")
}

func TestFrontendShowsQueuedDeploymentOnlyWhenOneIsWaiting(t *testing.T) {
	frontendID, err := uuid.Parse("66666666-6666-4666-8666-666666666666")
	require.NoError(t, err)
	pendingID, err := uuid.Parse("77777777-7777-4777-8777-777777777777")
	require.NoError(t, err)
	fe := apiclient.Frontend{
		Id:                  frontendID,
		Name:                "site",
		Framework:           apiclient.FrontendFramework("nextjs"),
		Status:              apiclient.FrontendStatusActive,
		PendingDeploymentId: &pendingID,
	}

	var queued bytes.Buffer
	Frontend(&queued, &fe)
	assert.Contains(t, queued.String(), "Pending Deployment: "+pendingID.String())

	fe.PendingDeploymentId = nil
	var idle bytes.Buffer
	Frontend(&idle, &fe)
	assert.NotContains(t, idle.String(), "Pending Deployment")
}
