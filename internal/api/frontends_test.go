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

// TestDeployFrontendAcceptsEmptyCreatedBody ensures that if the server
// breaks the OpenAPI contract by returning 201 Created with no body, we treat
// the request as a successful deploy instead of surfacing "HTTP 201: Created"
// as an error.
func TestDeployFrontendAcceptsEmptyCreatedBody(t *testing.T) {
	projectID := uuid.MustParse("11111111-1111-4111-8111-111111111111")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/projects/"+projectID.String()+"/frontends" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "token", WithHTTPClient(server.Client()))
	require.NoError(t, err)

	deployed, err := client.DeployFrontend(context.Background(), projectID, FrontendDeployInput{
		Name:      "web",
		Framework: "nextjs",
		Archive:   []byte("archive"),
	})
	require.NoError(t, err)
	require.NotNil(t, deployed)
	assert.Equal(t, "web", deployed.Name)
}

// TestRedeployFrontendAcceptsEmptyBody ensures that if the server breaks
// the OpenAPI contract by returning 200 OK with no body on redeploy, we
// treat the request as a successful redeploy instead of surfacing
// "HTTP 200: OK" as an error.
func TestRedeployFrontendAcceptsEmptyBody(t *testing.T) {
	projectID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	frontendID := uuid.MustParse("22222222-2222-4222-8222-222222222222")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expectedPath := "/projects/" + projectID.String() + "/frontends/" + frontendID.String() + "/redeploy"
		if r.Method != http.MethodPost || r.URL.Path != expectedPath {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "token", WithHTTPClient(server.Client()))
	require.NoError(t, err)

	redeployed, err := client.RedeployFrontend(context.Background(), projectID, frontendID)
	require.NoError(t, err)
	require.NotNil(t, redeployed)
	assert.Equal(t, frontendID, redeployed.Id)
}

func TestFrontendDomainAndLogsMethodsUseGeneratedRoutes(t *testing.T) {
	projectIDText := "11111111-1111-4111-8111-111111111111"
	frontendIDText := "22222222-2222-4222-8222-222222222222"
	deploymentIDText := "33333333-3333-4333-8333-333333333333"
	projectID := uuid.MustParse(projectIDText)
	frontendID := uuid.MustParse(frontendIDText)
	deploymentID := uuid.MustParse(deploymentIDText)
	var requests []string
	var createBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer token", r.Header.Get("Authorization"))
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+projectIDText+"/frontends/"+frontendIDText+"/deployments":
			assert.Equal(t, "3", r.URL.Query().Get("page"))
			assert.Equal(t, "10", r.URL.Query().Get("limit"))
			writeAPIJSON(t, w, http.StatusOK, map[string]any{
				"data":     []any{frontendDeploymentResponse(deploymentIDText, projectIDText, frontendIDText)},
				"has_more": false,
				"page":     3,
				"limit":    10,
				"total":    1,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+projectIDText+"/frontends/"+frontendIDText+"/logs":
			assert.Equal(t, "50", r.URL.Query().Get("limit"))
			assert.Equal(t, "fe-next", r.URL.Query().Get("cursor"))
			writeAPIJSON(t, w, http.StatusOK, logsResponse("frontend runtime"))
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+projectIDText+"/frontends/"+frontendIDText+"/deployments/"+deploymentIDText+"/logs":
			assert.Equal(t, "75", r.URL.Query().Get("limit"))
			assert.Equal(t, "dep-next", r.URL.Query().Get("cursor"))
			writeAPIJSON(t, w, http.StatusOK, logsResponse("frontend build"))
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+projectIDText+"/frontends/"+frontendIDText+"/domain":
			require.NoError(t, json.NewDecoder(r.Body).Decode(&createBody))
			writeAPIJSON(t, w, http.StatusCreated, frontendCustomDomainResponse("app.example.com", "provisioning"))
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+projectIDText+"/frontends/"+frontendIDText+"/domain":
			writeAPIJSON(t, w, http.StatusOK, frontendCustomDomainResponse("app.example.com", "active"))
		case r.Method == http.MethodDelete && r.URL.Path == "/projects/"+projectIDText+"/frontends/"+frontendIDText+"/domain":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "token", WithHTTPClient(server.Client()))
	require.NoError(t, err)

	deployments, err := client.ListFrontendDeployments(context.Background(), projectID, frontendID, 3, 10)
	require.NoError(t, err)
	require.Len(t, deployments.Data, 1)
	assert.Equal(t, deploymentIDText, deployments.Data[0].Id.String())

	runtimeLogs, err := client.GetFrontendLogs(context.Background(), projectID, frontendID, 50, "fe-next")
	require.NoError(t, err)
	assert.Equal(t, "frontend runtime", runtimeLogs.Data[0].Message)

	deploymentLogs, err := client.GetFrontendDeploymentLogs(context.Background(), projectID, frontendID, deploymentID, 75, "dep-next")
	require.NoError(t, err)
	assert.Equal(t, "frontend build", deploymentLogs.Data[0].Message)

	createdDomain, err := client.CreateFrontendCustomDomain(context.Background(), projectID, frontendID, FrontendCustomDomainInput{
		Domain:              " app.example.com ",
		CertificatePEM:      " cert ",
		PrivateKeyPEM:       " key ",
		CertificateChainPEM: " chain ",
	})
	require.NoError(t, err)
	assert.Equal(t, "app.example.com", createdDomain.Domain)
	tlsConfig, ok := createBody["tls"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "app.example.com", createBody["domain"])
	assert.Equal(t, "byoc", tlsConfig["mode"])
	assert.Equal(t, "cert", tlsConfig["certificate_pem"])
	assert.Equal(t, "key", tlsConfig["private_key_pem"])
	assert.Equal(t, "chain", tlsConfig["certificate_chain_pem"])

	gotDomain, err := client.GetFrontendCustomDomain(context.Background(), projectID, frontendID)
	require.NoError(t, err)
	assert.Equal(t, "active", string(gotDomain.DomainStatus))

	require.NoError(t, client.DeleteFrontendCustomDomain(context.Background(), projectID, frontendID))
	assert.Equal(t, []string{
		"GET /projects/" + projectIDText + "/frontends/" + frontendIDText + "/deployments?page=3&limit=10",
		"GET /projects/" + projectIDText + "/frontends/" + frontendIDText + "/logs?limit=50&cursor=fe-next",
		"GET /projects/" + projectIDText + "/frontends/" + frontendIDText + "/deployments/" + deploymentIDText + "/logs?limit=75&cursor=dep-next",
		"POST /projects/" + projectIDText + "/frontends/" + frontendIDText + "/domain",
		"GET /projects/" + projectIDText + "/frontends/" + frontendIDText + "/domain",
		"DELETE /projects/" + projectIDText + "/frontends/" + frontendIDText + "/domain",
	}, requests)
}

func frontendDeploymentResponse(id, projectID, frontendID string) map[string]any {
	return map[string]any{
		"created_at":  "2026-05-20T00:00:00Z",
		"frontend_id": frontendID,
		"id":          id,
		"operation":   "deploy",
		"project_id":  projectID,
		"status":      "active",
		"updated_at":  "2026-05-20T00:00:00Z",
	}
}

func frontendCustomDomainResponse(domain, status string) map[string]any {
	return map[string]any{
		"created_at":           "2026-05-20T00:00:00Z",
		"domain":               domain,
		"domain_status":        status,
		"effective_urls":       []string{"https://" + domain + "/"},
		"tls_mode":             "byoc",
		"updated_at":           "2026-05-20T00:00:00Z",
		"verification_records": []any{map[string]any{"type": "TXT", "name": "_volcano." + domain, "value": "verify"}},
		"verification_status":  "verified",
		"required_routing_record": map[string]any{
			"name":        domain,
			"record_type": "CNAME",
			"value":       "d123.cloudfront.net",
		},
	}
}
