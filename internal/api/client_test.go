package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPollDeviceTokenUnexpectedStatusReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/auth/device/token", r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", WithHTTPClient(server.Client()))
	require.NoError(t, err)

	status, err := client.PollDeviceToken(context.Background(), "device-client", "device-code")
	require.Nil(t, status)
	var apiErr *Error
	require.ErrorAs(t, err, &apiErr, "got error %T: %v", err, err)
	assert.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
	assert.Equal(t, http.StatusText(http.StatusInternalServerError), apiErr.Message)
}

func TestStartDeviceAuthorizationNormalizesOAuthError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/auth/device/authorize", r.URL.Path)
		writeAPIJSON(t, w, http.StatusBadRequest, map[string]string{
			"error":             "invalid_request",
			"error_description": "client_id is required",
		})
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", WithHTTPClient(server.Client()))
	require.NoError(t, err)

	deviceAuth, err := client.StartDeviceAuthorization(context.Background(), " ")
	require.Nil(t, deviceAuth)
	require.ErrorContains(t, err, "HTTP 400: client_id is required")
}

func TestWebSignupURL(t *testing.T) {
	signupURL, err := WebSignupURL("http://localhost:3000", " ted@example.com ")
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:3000/signup?email=ted%40example.com&source=cli", signupURL)
}

func TestNewClientPreservesAPIURLPathPrefix(t *testing.T) {
	var sawPath string
	var sawQuery string
	var sawAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.Path
		sawQuery = r.URL.RawQuery
		sawAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[],"has_more":false,"page":1,"limit":100,"total":0}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/volcano-api", "token", WithHTTPClient(server.Client()))
	require.NoError(t, err)

	projects, err := client.ListProjects(context.Background(), DefaultPage, DefaultLimit)
	require.NoError(t, err)
	assert.Empty(t, projects.Data)
	assert.Equal(t, "/volcano-api/projects", sawPath)
	assert.Equal(t, "page=1&limit=100", sawQuery)
	assert.Equal(t, "Bearer token", sawAuth)
}

func TestListProjectsReturnsRequestedPage(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/projects", r.URL.Path)
		assert.Equal(t, "page=3&limit=25", r.URL.RawQuery)
		requests++
		writeAPIJSON(t, w, http.StatusOK, map[string]any{
			"data": []any{
				projectResponse("11111111-1111-4111-8111-111111111111", "Project"),
			},
			"has_more": false,
			"page":     3,
			"limit":    25,
			"total":    51,
		})
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", WithHTTPClient(server.Client()))
	require.NoError(t, err)

	projects, err := client.ListProjects(context.Background(), 3, 25)
	require.NoError(t, err)
	require.Len(t, projects.Data, 1)
	assert.Equal(t, 3, projects.Page)
	assert.Equal(t, 25, projects.Limit)
	assert.Equal(t, 51, projects.Total)
	assert.Equal(t, 1, requests)
}

func TestListProjectsDefersPaginationValidationToAPI(t *testing.T) {
	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		writeAPIJSON(t, w, http.StatusBadRequest, map[string]string{"error": "invalid pagination"})
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", WithHTTPClient(server.Client()))
	require.NoError(t, err)

	_, err = client.ListProjects(context.Background(), 0, 100)
	require.ErrorContains(t, err, "HTTP 400: invalid pagination")
	_, err = client.ListProjects(context.Background(), 1, 0)
	require.ErrorContains(t, err, "HTTP 400: invalid pagination")
	_, err = client.ListProjects(context.Background(), 1, 101)
	require.ErrorContains(t, err, "HTTP 400: invalid pagination")
	assert.Equal(t, []string{"page=0&limit=100", "page=1&limit=0", "page=1&limit=101"}, queries)
}

func TestDatabaseMethodsUseGeneratedRoutes(t *testing.T) {
	projectIDText := "11111111-1111-4111-8111-111111111111"
	projectID := mustProjectID(t, projectIDText)
	var requests []string
	var createBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer token", r.Header.Get("Authorization"))
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+projectIDText+"/databases":
			assert.Equal(t, "page=2&limit=25", r.URL.RawQuery)
			writeAPIJSON(t, w, http.StatusOK, map[string]any{
				"data":     []any{databaseResponse("33333333-3333-4333-8333-333333333333", projectIDText, "app")},
				"has_more": false,
				"page":     2,
				"limit":    25,
				"total":    1,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+projectIDText+"/databases":
			require.NoError(t, json.NewDecoder(r.Body).Decode(&createBody))
			writeAPIJSON(t, w, http.StatusCreated, databaseResponse("33333333-3333-4333-8333-333333333333", projectIDText, "app"))
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+projectIDText+"/databases/app":
			writeAPIJSON(t, w, http.StatusOK, databaseResponse("33333333-3333-4333-8333-333333333333", projectIDText, "app"))
		case r.Method == http.MethodDelete && r.URL.Path == "/projects/"+projectIDText+"/databases/app":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "token", WithHTTPClient(server.Client()))
	require.NoError(t, err)

	databases, err := client.ListDatabases(context.Background(), projectID, 2, 25)
	require.NoError(t, err)
	require.Len(t, databases.Data, 1)
	assert.Equal(t, "app", databases.Data[0].Name)

	created, err := client.CreateDatabase(context.Background(), projectID, "app", "aws-us-east-2", "15", "volcano-db-s")
	require.NoError(t, err)
	assert.Equal(t, "app", created.Name)
	assert.Equal(t, map[string]any{
		"name":          "app",
		"region":        "aws-us-east-2",
		"pg_version":    "15",
		"database_type": "volcano-db-s",
	}, createBody)

	got, err := client.GetDatabase(context.Background(), projectID, "app")
	require.NoError(t, err)
	assert.Equal(t, "app", got.Name)

	require.NoError(t, client.DeleteDatabase(context.Background(), projectID, "app"))
	assert.Equal(t, []string{
		"GET /projects/" + projectIDText + "/databases?page=2&limit=25",
		"POST /projects/" + projectIDText + "/databases",
		"GET /projects/" + projectIDText + "/databases/app",
		"DELETE /projects/" + projectIDText + "/databases/app",
	}, requests)
}

func TestDatabaseErrorsNormalize(t *testing.T) {
	projectID := mustProjectID(t, "11111111-1111-4111-8111-111111111111")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeAPIJSON(t, w, http.StatusInternalServerError, map[string]string{"error": "boom"})
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", WithHTTPClient(server.Client()))
	require.NoError(t, err)

	_, err = client.ListDatabases(context.Background(), projectID, 1, 100)
	require.ErrorContains(t, err, "HTTP 500: boom")
}

func TestVariableMethodsUseGeneratedRoutes(t *testing.T) {
	projectIDText := "11111111-1111-4111-8111-111111111111"
	projectID := mustProjectID(t, projectIDText)
	var requests []string
	var createBody map[string]string
	var updateBody map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer token", r.Header.Get("Authorization"))
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+projectIDText+"/variables":
			assert.Equal(t, "page=2&limit=25", r.URL.RawQuery)
			writeAPIJSON(t, w, http.StatusOK, map[string]any{
				"data":     []any{variableResponse("33333333-3333-4333-8333-333333333333", projectIDText, "API_KEY", "old-value")},
				"has_more": false,
				"page":     2,
				"limit":    25,
				"total":    1,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+projectIDText+"/variables":
			require.NoError(t, json.NewDecoder(r.Body).Decode(&createBody))
			writeAPIJSON(t, w, http.StatusCreated, variableResponse("33333333-3333-4333-8333-333333333333", projectIDText, "API_KEY", "new-value"))
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+projectIDText+"/variables/API_KEY":
			writeAPIJSON(t, w, http.StatusOK, variableResponse("33333333-3333-4333-8333-333333333333", projectIDText, "API_KEY", "new-value"))
		case r.Method == http.MethodPut && r.URL.Path == "/projects/"+projectIDText+"/variables/API_KEY":
			require.NoError(t, json.NewDecoder(r.Body).Decode(&updateBody))
			writeAPIJSON(t, w, http.StatusOK, variableResponse("33333333-3333-4333-8333-333333333333", projectIDText, "API_KEY", "new-value"))
		case r.Method == http.MethodDelete && r.URL.Path == "/projects/"+projectIDText+"/variables/API_KEY":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "token", WithHTTPClient(server.Client()))
	require.NoError(t, err)

	variables, err := client.ListVariables(context.Background(), projectID, 2, 25)
	require.NoError(t, err)
	require.Len(t, variables.Data, 1)
	assert.Equal(t, "API_KEY", variables.Data[0].Name)

	created, err := client.CreateVariable(context.Background(), projectID, " API_KEY ", "new-value")
	require.NoError(t, err)
	assert.Equal(t, "API_KEY", created.Name)
	assert.Equal(t, map[string]string{"name": "API_KEY", "value": "new-value"}, createBody)

	got, err := client.GetVariable(context.Background(), projectID, "API_KEY")
	require.NoError(t, err)
	assert.Equal(t, "new-value", got.Value)

	updated, err := client.UpdateVariable(context.Background(), projectID, "API_KEY", "new-value")
	require.NoError(t, err)
	assert.Equal(t, "new-value", updated.Value)
	assert.Equal(t, map[string]string{"value": "new-value"}, updateBody)

	require.NoError(t, client.DeleteVariable(context.Background(), projectID, "API_KEY"))
	assert.Equal(t, []string{
		"GET /projects/" + projectIDText + "/variables?page=2&limit=25",
		"POST /projects/" + projectIDText + "/variables",
		"GET /projects/" + projectIDText + "/variables/API_KEY",
		"PUT /projects/" + projectIDText + "/variables/API_KEY",
		"DELETE /projects/" + projectIDText + "/variables/API_KEY",
	}, requests)
}

func TestFunctionMethodsUseGeneratedRoutes(t *testing.T) {
	projectIDText := "11111111-1111-4111-8111-111111111111"
	functionIDText := "22222222-2222-4222-8222-222222222222"
	deploymentIDText := "33333333-3333-4333-8333-333333333333"
	schedulerIDText := "44444444-4444-4444-8444-444444444444"
	projectID := mustProjectID(t, projectIDText)
	functionID := mustProjectID(t, functionIDText)
	deploymentID := mustProjectID(t, deploymentIDText)
	schedulerID := mustProjectID(t, schedulerIDText)
	var requests []string
	var deployFields map[string]string
	var deployFilename string
	var batchManifest []struct {
		Name      string `json:"name"`
		Runtime   string `json:"runtime"`
		Handler   string `json:"handler"`
		FileField string `json:"file_field"`
	}
	var batchFilenames []string
	var updateBody map[string]bool
	var invokeBody map[string]any
	var logSearchBodies []map[string]any
	var schedulerCreateBody map[string]any
	var schedulerUpdateBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer token", r.Header.Get("Authorization"))
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+projectIDText+"/functions":
			assert.Equal(t, "2", r.URL.Query().Get("page"))
			assert.Equal(t, "25", r.URL.Query().Get("limit"))
			writeAPIJSON(t, w, http.StatusOK, map[string]any{
				"data":     []any{functionResponse(functionIDText, projectIDText, "hello")},
				"has_more": false,
				"page":     2,
				"limit":    25,
				"total":    1,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+projectIDText+"/functions":
			require.NoError(t, r.ParseMultipartForm(1024*1024))
			deployFields = map[string]string{
				"name":    r.FormValue("name"),
				"runtime": r.FormValue("runtime"),
				"handler": r.FormValue("handler"),
			}
			files := r.MultipartForm.File["code"]
			require.Len(t, files, 1)
			deployFilename = files[0].Filename
			writeAPIJSON(t, w, http.StatusCreated, functionResponse(functionIDText, projectIDText, "hello"))
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+projectIDText+"/functions/batch":
			require.NoError(t, r.ParseMultipartForm(1024*1024))
			require.NoError(t, json.Unmarshal([]byte(r.FormValue("functions")), &batchManifest))
			for _, fieldName := range []string{"code_0", "code_1"} {
				files := r.MultipartForm.File[fieldName]
				require.Len(t, files, 1)
				batchFilenames = append(batchFilenames, files[0].Filename)
			}
			writeAPIJSON(t, w, http.StatusMultiStatus, map[string]any{
				"batch_id": "55555555-5555-4555-8555-555555555555",
				"data":     []any{functionResponse(functionIDText, projectIDText, "hello")},
				"failed": []any{
					map[string]any{"name": "two", "error": "failed to start function workflow"},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+projectIDText+"/functions/"+functionIDText:
			writeAPIJSON(t, w, http.StatusOK, functionResponse(functionIDText, projectIDText, "hello"))
		case r.Method == http.MethodDelete && r.URL.Path == "/projects/"+projectIDText+"/functions/"+functionIDText:
			w.WriteHeader(http.StatusAccepted)
		case r.Method == http.MethodPatch && r.URL.Path == "/projects/"+projectIDText+"/functions/"+functionIDText:
			require.NoError(t, json.NewDecoder(r.Body).Decode(&updateBody))
			writeAPIJSON(t, w, http.StatusOK, functionResponse(functionIDText, projectIDText, "hello"))
		case r.Method == http.MethodPost && r.URL.Path == "/functions/"+functionIDText+"/invoke":
			require.NoError(t, json.NewDecoder(r.Body).Decode(&invokeBody))
			writeAPIJSON(t, w, http.StatusOK, map[string]any{"ok": true})
		case r.Method == http.MethodGet && r.URL.Path == "/functions/runtimes":
			writeAPIJSON(t, w, http.StatusOK, map[string]any{
				"runtimes": []any{
					map[string]any{"name": "nodejs24.x", "language": "nodejs", "default": true},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+projectIDText+"/functions/"+functionIDText+"/deployments":
			assert.Equal(t, "3", r.URL.Query().Get("page"))
			assert.Equal(t, "10", r.URL.Query().Get("limit"))
			writeAPIJSON(t, w, http.StatusOK, map[string]any{
				"data":     []any{functionDeploymentResponse(deploymentIDText, projectIDText, functionIDText)},
				"has_more": false,
				"page":     3,
				"limit":    10,
				"total":    1,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+projectIDText+"/logs/search":
			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			logSearchBodies = append(logSearchBodies, body)
			if len(logSearchBodies) == 1 {
				writeAPIJSON(t, w, http.StatusOK, logsResponse("function runtime"))
				return
			}
			writeAPIJSON(t, w, http.StatusOK, logsResponse("deployment build"))
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+projectIDText+"/functions/"+functionIDText+"/schedulers":
			writeAPIJSON(t, w, http.StatusOK, map[string]any{
				"data":     []any{functionSchedulerResponse(schedulerIDText, functionIDText, projectIDText, "hello scheduler", true)},
				"has_more": false,
				"page":     1,
				"limit":    25,
				"total":    1,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+projectIDText+"/functions/"+functionIDText+"/schedulers":
			require.NoError(t, json.NewDecoder(r.Body).Decode(&schedulerCreateBody))
			writeAPIJSON(t, w, http.StatusCreated, functionSchedulerResponse(schedulerIDText, functionIDText, projectIDText, "hello scheduler", true))
		case r.Method == http.MethodPatch && r.URL.Path == "/projects/"+projectIDText+"/functions/"+functionIDText+"/schedulers/"+schedulerIDText:
			require.NoError(t, json.NewDecoder(r.Body).Decode(&schedulerUpdateBody))
			writeAPIJSON(t, w, http.StatusOK, functionSchedulerResponse(schedulerIDText, functionIDText, projectIDText, "hello scheduler", false))
		case r.Method == http.MethodDelete && r.URL.Path == "/projects/"+projectIDText+"/functions/"+functionIDText+"/schedulers/"+schedulerIDText:
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "token", WithHTTPClient(server.Client()))
	require.NoError(t, err)

	functions, err := client.ListFunctions(context.Background(), projectID, 2, 25)
	require.NoError(t, err)
	require.Len(t, functions.Data, 1)
	assert.Equal(t, "hello", functions.Data[0].Name)

	deployed, err := client.DeployFunction(context.Background(), projectID, FunctionDeployInput{
		Name:          "hello",
		Runtime:       "nodejs24.x",
		Handler:       "handler",
		SourceArchive: []byte("archive"),
	})
	require.NoError(t, err)
	assert.Equal(t, "hello", deployed.Name)
	assert.Equal(t, map[string]string{
		"name":    "hello",
		"runtime": "nodejs24.x",
		"handler": "handler",
	}, deployFields)
	assert.Equal(t, "hello.tar.gz", deployFilename)

	batch, err := client.DeployFunctionsBatch(context.Background(), projectID, []FunctionDeployInput{
		{Name: "one", Runtime: "nodejs24.x", Handler: "handler", SourceArchive: []byte("one")},
		{Name: "two", Runtime: "python3.13", Handler: "handler", SourceArchive: []byte("two")},
	})
	require.NoError(t, err)
	require.Len(t, batch.Data, 1)
	assert.Equal(t, "hello", batch.Data[0].Name)
	require.NotNil(t, batch.Failed)
	require.Len(t, *batch.Failed, 1)
	assert.Equal(t, "two", (*batch.Failed)[0].Name)
	assert.Equal(t, []string{"one.tar.gz", "two.tar.gz"}, batchFilenames)
	assert.Equal(t, []struct {
		Name      string `json:"name"`
		Runtime   string `json:"runtime"`
		Handler   string `json:"handler"`
		FileField string `json:"file_field"`
	}{
		{Name: "one", Runtime: "nodejs24.x", Handler: "handler", FileField: "code_0"},
		{Name: "two", Runtime: "python3.13", Handler: "handler", FileField: "code_1"},
	}, batchManifest)

	got, err := client.GetFunction(context.Background(), projectID, functionID)
	require.NoError(t, err)
	assert.Equal(t, "hello", got.Name)

	require.NoError(t, client.DeleteFunction(context.Background(), projectID, functionID))

	updated, err := client.UpdateFunctionVisibility(context.Background(), projectID, functionID, true)
	require.NoError(t, err)
	assert.Equal(t, "hello", updated.Name)
	assert.Equal(t, map[string]bool{"is_public": true}, updateBody)

	invoked, err := client.InvokeFunction(context.Background(), functionID, FunctionInvokeInput{
		Payload: map[string]any{"k": "v"},
	})
	require.NoError(t, err)
	require.NotNil(t, invoked)
	assert.Equal(t, true, (*invoked)["ok"])
	assert.Equal(t, map[string]any{"payload": map[string]any{"k": "v"}}, invokeBody)

	runtimes, err := client.ListFunctionRuntimes(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "nodejs24.x", runtimes[0].Name)

	deployments, err := client.ListFunctionDeployments(context.Background(), projectID, functionID, 3, 10)
	require.NoError(t, err)
	require.Len(t, deployments.Data, 1)
	assert.Equal(t, deploymentIDText, deployments.Data[0].Id.String())

	runtimeLogs, err := client.GetFunctionLogs(context.Background(), projectID, functionID, 50, "fn-next")
	require.NoError(t, err)
	assert.Equal(t, "function runtime", runtimeLogs.Data[0].Message)
	require.Len(t, logSearchBodies, 1)
	runtimeResource, ok := logSearchBodies[0]["resource"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "function", runtimeResource["type"])
	assert.Equal(t, []any{functionIDText}, runtimeResource["ids"])
	assert.InEpsilon(t, 50, logSearchBodies[0]["limit"], 0)
	assert.Equal(t, "fn-next", logSearchBodies[0]["cursor"])

	deploymentLogs, err := client.GetFunctionDeploymentLogs(context.Background(), projectID, functionID, deploymentID, 75, "dep-next")
	require.NoError(t, err)
	assert.Equal(t, "deployment build", deploymentLogs.Data[0].Message)
	require.Len(t, logSearchBodies, 2)
	buildResource, ok := logSearchBodies[1]["resource"].(map[string]any)
	require.True(t, ok)
	buildDeployments, ok := buildResource["deployments"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "function", buildResource["type"])
	assert.Equal(t, []any{functionIDText}, buildResource["ids"])
	assert.Equal(t, []any{deploymentIDText}, buildDeployments["ids"])
	assert.InEpsilon(t, 75, logSearchBodies[1]["limit"], 0)
	assert.Equal(t, "dep-next", logSearchBodies[1]["cursor"])

	schedulers, err := client.ListFunctionSchedulers(context.Background(), projectID, functionID)
	require.NoError(t, err)
	require.Len(t, schedulers.Data, 1)
	require.NotNil(t, schedulers.Data[0].Name)
	assert.Equal(t, "hello scheduler", *schedulers.Data[0].Name)

	enabled := true
	createdScheduler, err := client.CreateFunctionScheduler(context.Background(), projectID, functionID, FunctionSchedulerInput{
		Name:           "hello scheduler",
		CronExpression: "*/5 * * * *",
		Payload:        map[string]any{"k": "v"},
		Regions:        []string{"us-east-1"},
		Enabled:        &enabled,
	})
	require.NoError(t, err)
	require.NotNil(t, createdScheduler.Id)
	assert.Equal(t, schedulerIDText, createdScheduler.Id.String())
	assert.Equal(t, "hello scheduler", schedulerCreateBody["name"])
	scheduleField, ok := schedulerCreateBody["schedule"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "*/5 * * * *", scheduleField["cron_expression"])
	assert.Equal(t, true, schedulerCreateBody["enabled"])
	assert.Equal(t, []any{"us-east-1"}, schedulerCreateBody["regions"])

	disabled := false
	updatedScheduler, err := client.UpdateFunctionScheduler(context.Background(), projectID, functionID, schedulerID, FunctionSchedulerInput{
		Enabled: &disabled,
	})
	require.NoError(t, err)
	require.NotNil(t, updatedScheduler.Enabled)
	assert.False(t, *updatedScheduler.Enabled)
	assert.Equal(t, false, schedulerUpdateBody["enabled"])

	require.NoError(t, client.DeleteFunctionScheduler(context.Background(), projectID, functionID, schedulerID))

	assert.Equal(t, []string{
		"GET /projects/" + projectIDText + "/functions?page=2&limit=25",
		"POST /projects/" + projectIDText + "/functions",
		"POST /projects/" + projectIDText + "/functions/batch",
		"GET /projects/" + projectIDText + "/functions/" + functionIDText,
		"DELETE /projects/" + projectIDText + "/functions/" + functionIDText,
		"PATCH /projects/" + projectIDText + "/functions/" + functionIDText,
		"POST /functions/" + functionIDText + "/invoke",
		"GET /functions/runtimes",
		"GET /projects/" + projectIDText + "/functions/" + functionIDText + "/deployments?page=3&limit=10",
		"POST /projects/" + projectIDText + "/logs/search",
		"POST /projects/" + projectIDText + "/logs/search",
		"GET /projects/" + projectIDText + "/functions/" + functionIDText + "/schedulers",
		"POST /projects/" + projectIDText + "/functions/" + functionIDText + "/schedulers",
		"PATCH /projects/" + projectIDText + "/functions/" + functionIDText + "/schedulers/" + schedulerIDText,
		"DELETE /projects/" + projectIDText + "/functions/" + functionIDText + "/schedulers/" + schedulerIDText,
	}, requests)
}

func TestDeleteFunctionAcceptsAsyncAndNoContent(t *testing.T) {
	projectID := mustProjectID(t, "11111111-1111-4111-8111-111111111111")
	functionID := mustProjectID(t, "22222222-2222-4222-8222-222222222222")
	for _, status := range []int{http.StatusAccepted, http.StatusNoContent} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodDelete, r.Method)
				w.WriteHeader(status)
			}))
			defer server.Close()

			client, err := NewClient(server.URL, "", WithHTTPClient(server.Client()))
			require.NoError(t, err)
			require.NoError(t, client.DeleteFunction(context.Background(), projectID, functionID))
		})
	}
}

func TestInvokeFunctionErrorsNormalize(t *testing.T) {
	functionID := mustProjectID(t, "22222222-2222-4222-8222-222222222222")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeAPIJSON(t, w, http.StatusTooManyRequests, map[string]string{"error": "rate limited"})
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", WithHTTPClient(server.Client()))
	require.NoError(t, err)

	_, err = client.InvokeFunction(context.Background(), functionID, FunctionInvokeInput{})
	require.ErrorContains(t, err, "HTTP 429: rate limited")
}

func mustProjectID(t *testing.T, value string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(value)
	require.NoError(t, err)
	return id
}

func projectResponse(id, name string) map[string]any {
	return map[string]any{
		"all_regions":      false,
		"created_at":       "2026-05-20T00:00:00Z",
		"id":               id,
		"name":             name,
		"selected_regions": []string{},
		"status":           "active",
		"updated_at":       "2026-05-20T00:00:00Z",
	}
}

func databaseResponse(id, projectID, name string) map[string]any {
	return map[string]any{
		"connection_string": "postgres://example",
		"created_at":        "2026-05-20T00:00:00Z",
		"database_type":     "volcano-db-s",
		"id":                id,
		"name":              name,
		"pg_version":        "16",
		"project_id":        projectID,
		"region":            "aws-us-east-1",
		"status":            "active",
		"updated_at":        "2026-05-20T00:00:00Z",
	}
}

func variableResponse(id, projectID, name, value string) map[string]any {
	return map[string]any{
		"created_at": "2026-05-20T00:00:00Z",
		"id":         id,
		"name":       name,
		"project_id": projectID,
		"status":     "active",
		"updated_at": "2026-05-20T00:00:00Z",
		"value":      value,
	}
}

func functionResponse(id, projectID, name string) map[string]any {
	return map[string]any{
		"created_at":       "2026-05-20T00:00:00Z",
		"deployed_regions": []string{"aws-us-east-1"},
		"handler":          "handler",
		"id":               id,
		"invoke_url":       "https://" + id + ".functions.volcano.dev/",
		"is_public":        true,
		"name":             name,
		"project_id":       projectID,
		"runtime":          "nodejs24.x",
		"status":           "active",
		"updated_at":       "2026-05-20T00:00:00Z",
	}
}

func functionSchedulerResponse(id, functionID, projectID, name string, enabled bool) map[string]any {
	return map[string]any{
		"created_at":      "2026-05-20T00:00:00Z",
		"cron_expression": "*/5 * * * *",
		"enabled":         enabled,
		"function_id":     functionID,
		"id":              id,
		"name":            name,
		"project_id":      projectID,
		"regions":         []string{"aws-us-east-1"},
		"schedule_kind":   "cron",
		"updated_at":      "2026-05-20T00:00:00Z",
	}
}

func functionDeploymentResponse(id, projectID, functionID string) map[string]any {
	return map[string]any{
		"created_at":  "2026-05-20T00:00:00Z",
		"function_id": functionID,
		"id":          id,
		"operation":   "deploy",
		"project_id":  projectID,
		"status":      "active",
		"updated_at":  "2026-05-20T00:00:00Z",
	}
}

func logsResponse(message string) map[string]any {
	return map[string]any{
		"data": []any{
			map[string]any{
				"message":   message,
				"timestamp": int64(1760000000000),
			},
		},
		"has_more": false,
		"limit":    100,
		"page":     1,
		"total":    1,
	}
}

func writeAPIJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	require.NoError(t, json.NewEncoder(w).Encode(value))
}
