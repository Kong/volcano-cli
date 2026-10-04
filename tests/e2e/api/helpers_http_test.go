package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

var errAPIE2ECleanup = errors.New("API E2E cleanup failed")

func createAPIE2EUser(t *testing.T, mgmtURL, userID string) {
	t.Helper()
	body := map[string]string{"id": userID, "name": "CLI E2E User"}
	apiE2EJSONRequest(t, http.MethodPost, mgmtURL+"/users", "", body, http.StatusCreated)
}

func createAPIE2EUserToken(t *testing.T, mgmtURL, userID string) string {
	t.Helper()
	tokenResp := apiE2EJSONRequest(t, http.MethodPost, mgmtURL+"/users/"+userID+"/tokens", "", map[string]string{"name": "cli-e2e-token"}, http.StatusCreated)
	token, ok := tokenResp["token"].(string)
	if !ok || strings.TrimSpace(token) == "" {
		t.Fatalf("management token response did not include token: %#v", tokenResp)
	}
	return token
}

func createAPIE2EProject(t *testing.T, apiURL, token, name string) string {
	t.Helper()
	project := apiE2EJSONRequest(t, http.MethodPost, apiURL+"/projects", token, map[string]string{"name": name}, http.StatusCreated)
	id, ok := project["id"].(string)
	if !ok || strings.TrimSpace(id) == "" {
		t.Fatalf("project response did not include id: %#v", project)
	}
	return id
}

func createAPIE2EServiceKey(t *testing.T, apiURL, token, projectID, name string) string {
	t.Helper()
	resp := apiE2EJSONRequest(t, http.MethodPost, apiURL+"/projects/"+projectID+"/service-keys", token, map[string]string{"name": name}, http.StatusCreated)
	key, ok := resp["key_value"].(string)
	if !ok || strings.TrimSpace(key) == "" {
		t.Fatalf("service key response did not include key_value: %#v", resp)
	}
	return key
}

func createAPIE2EAnonKey(t *testing.T, apiURL, token, projectID, name string, permissions ...string) string {
	t.Helper()
	body := map[string]any{"name": name, "permissions": permissions}
	resp := apiE2EJSONRequest(t, http.MethodPost, apiURL+"/projects/"+projectID+"/anon-keys", token, body, http.StatusCreated)
	key, ok := resp["key_value"].(string)
	if !ok || strings.TrimSpace(key) == "" {
		t.Fatalf("anon key response did not include key_value: %#v", resp)
	}
	return key
}

// signUpAPIE2EEndUser creates one of the project's own users and returns its
// access token. Signup answers without a session, so the token comes from
// signing in afterwards. The anon key needs auth.signup and auth.signin.
func signUpAPIE2EEndUser(t *testing.T, apiURL, anonKey string) string {
	t.Helper()
	credentials := map[string]string{
		"email":    "cli-e2e-" + apiE2ESuffix(t) + "@cloud-e2e.invalid",
		"password": "V0lcano!" + apiE2ESuffix(t) + apiE2ESuffix(t),
	}
	apiE2EJSONRequest(t, http.MethodPost, apiURL+"/auth/signup", anonKey, credentials, http.StatusCreated)
	session := apiE2EJSONRequest(t, http.MethodPost, apiURL+"/auth/signin", anonKey, credentials, http.StatusOK)
	token, ok := session["access_token"].(string)
	if !ok || strings.TrimSpace(token) == "" {
		t.Fatal("signin response did not include access_token")
	}
	return token
}

// apiE2EInvocation is the API's answer to one invoke.
type apiE2EInvocation struct {
	status int
	body   string
	// ran reports the headers the API sets only when the function executed.
	ran bool
}

// apiE2EInvoke invokes a function by ID with a caller's credential.
func apiE2EInvoke(apiURL, token, functionID string) (apiE2EInvocation, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL+"/functions/"+functionID+"/invoke",
		strings.NewReader(`{"payload":{}}`))
	if err != nil {
		return apiE2EInvocation{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return apiE2EInvocation{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return apiE2EInvocation{}, err
	}
	return apiE2EInvocation{
		status: resp.StatusCode,
		body:   strings.TrimSpace(string(body)),
		ran:    resp.Header.Get("X-Volcano-Function-Invoked") != "" || resp.Header.Get("X-Volcano-Compute-Ms") != "",
	}, nil
}

// sameAPIE2EJSON compares two response bodies as JSON, so key order and
// spacing do not count. Bodies that are not JSON compare as text.
func sameAPIE2EJSON(a, b string) bool {
	var decodedA, decodedB any
	if json.Unmarshal([]byte(a), &decodedA) != nil || json.Unmarshal([]byte(b), &decodedB) != nil {
		return a == b
	}
	return reflect.DeepEqual(decodedA, decodedB)
}

func deleteAPIE2EProject(apiURL, token, projectID string) error {
	return deleteAPIE2EResource(apiURL+"/projects/"+projectID, token)
}

func deleteAPIE2EUser(mgmtURL, userID string) error {
	return deleteAPIE2EResource(mgmtURL+"/users/"+userID, "")
}

func deleteAPIE2EResource(url, token string) error {
	deadline := time.Now().Add(apiE2EResourceDeleteTimeout)
	for {
		status, body, err := deleteAPIE2EResourceOnce(url, token)
		if err != nil {
			return err
		}
		if status >= http.StatusOK && status < http.StatusMultipleChoices || status == http.StatusNotFound {
			return nil
		}
		if status != http.StatusConflict {
			return fmt.Errorf("%w: DELETE %s returned %d: %s", errAPIE2ECleanup, url, status, body)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%w: DELETE %s remained blocked by 409", errAPIE2ECleanup, url)
		}
		time.Sleep(apiE2EPollInterval)
	}
}

func deleteAPIE2EResourceOnce(url, token string) (int, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, http.NoBody)
	if err != nil {
		return 0, "", fmt.Errorf("%w: build DELETE %s: %w", errAPIE2ECleanup, url, err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("%w: DELETE %s: %w", errAPIE2ECleanup, url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, strings.TrimSpace(string(body)), nil
}

func apiE2EJSONRequest(t *testing.T, method, url, token string, body any, expectedStatuses ...int) map[string]any {
	t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("failed to encode request body: %v", err)
		}
		reader = bytes.NewReader(data)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s failed: %v", method, url, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if !slices.Contains(expectedStatuses, resp.StatusCode) {
		t.Fatalf("%s %s returned status %d, want %v: %s", method, url, resp.StatusCode, expectedStatuses, strings.TrimSpace(string(data)))
	}
	if strings.TrimSpace(string(data)) == "" {
		return nil
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to decode JSON response from %s %s: %v\n%s", method, url, err, string(data))
	}
	return decoded
}
