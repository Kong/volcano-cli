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

func TestServiceKeyRequests(t *testing.T) {
	projectID := uuid.MustParse("eac37d5a-5f6f-42d8-acf6-0f2ae9c7a550")
	keyID := uuid.MustParse("33333333-3333-4333-8333-333333333333")
	var methods []string
	var paths []string
	var queries []string
	var createBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		paths = append(paths, r.URL.Path)
		queries = append(queries, r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.RawQuery != "":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[{"id":"33333333-3333-4333-8333-333333333333","name":"admin","key_prefix":"sk-prefix","key_value":"sk-value","permissions":["*"]}],"has_more":false,"page":2,"limit":25,"total":1}`))
		case r.Method == http.MethodPost:
			require.NoError(t, json.NewDecoder(r.Body).Decode(&createBody))
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"33333333-3333-4333-8333-333333333333","name":"admin","key_prefix":"sk-prefix","key_value":"sk-value","permissions":["functions.invoke"]}`))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"33333333-3333-4333-8333-333333333333","name":"admin","key_prefix":"sk-prefix","key_value":"sk-value","permissions":["*"]}`))
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "pk-account", WithHTTPClient(server.Client()))
	require.NoError(t, err)
	page, err := client.ListServiceKeys(context.Background(), projectID, 2, 25)
	require.NoError(t, err)
	assert.Equal(t, 1, len(page.Data))
	assert.Equal(t, "sk-value", *page.Data[0].KeyValue)
	created, err := client.CreateServiceKey(context.Background(), projectID, "admin", []string{"functions.invoke"})
	require.NoError(t, err)
	assert.Equal(t, "sk-value", *created.KeyValue)
	loaded, err := client.GetServiceKey(context.Background(), projectID, keyID)
	require.NoError(t, err)
	assert.Equal(t, "sk-value", *loaded.KeyValue)

	assert.Equal(t, []string{http.MethodGet, http.MethodPost, http.MethodGet}, methods)
	assert.Equal(t, []string{
		"/projects/" + projectID.String() + "/service-keys",
		"/projects/" + projectID.String() + "/service-keys",
		"/projects/" + projectID.String() + "/service-keys/" + keyID.String(),
	}, paths)
	assert.Equal(t, "page=2&limit=25", queries[0])
	assert.Equal(t, map[string]any{"name": "admin", "permissions": []any{"functions.invoke"}}, createBody)
}

func TestServiceKeyReadErrorsRedactResponseBody(t *testing.T) {
	projectID := uuid.MustParse("eac37d5a-5f6f-42d8-acf6-0f2ae9c7a550")
	keyID := uuid.MustParse("33333333-3333-4333-8333-333333333333")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"33333333-3333-4333-8333-333333333333","key_value":"sk-do-not-leak"}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "pk-account", WithHTTPClient(server.Client()))
	require.NoError(t, err)

	_, err = client.ListServiceKeys(context.Background(), projectID, 1, 100)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "sk-do-not-leak")
	_, err = client.GetServiceKey(context.Background(), projectID, keyID)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "sk-do-not-leak")
}
