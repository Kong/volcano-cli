package localmode

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchInfoRunsDockerExecLocalInfo(t *testing.T) {
	var gotName string
	var gotArgs []string

	info, err := FetchInfo(context.Background(), CommandRunnerFunc(func(_ context.Context, name string, args ...string) ([]byte, error) {
		gotName = name
		gotArgs = append([]string(nil), args...)
		return []byte(`{
			"api_url":"http://localhost:8000",
			"project_id":"00000000-0000-0000-0000-000000000000",
			"project_name":"local-dev",
			"user_id":"local-dev-user",
			"auth_user_id":"11111111-1111-1111-1111-111111111111",
			"auth_user_email":"dev@localhost",
			"user_token":"pk-local-dev-token-00000000000000000000",
			"anon_key":"ak-0000000000000000000000000000000000000000",
			"service_key":"sk-1111111111111111111111111111111111111111",
			"default_database_name":"app",
			"default_database_region":"local",
			"default_database_postgres_version":"16",
			"database_url":"postgres://volcano:volcano@localhost:8002/app?sslmode=disable&application_name=volcano_full_access:app",
			"redis_url":"redis://localhost:6379",
			"jwt_secret":"server-owned-jwt-secret",
			"encryption_key":"server-owned-encryption-key",
			"anon_key_secret":"server-owned-anon-secret",
			"service_key_secret":"server-owned-service-secret"
		}`), nil
	}))

	require.NoError(t, err)
	assert.Equal(t, "docker", gotName)
	assert.Equal(t, []string{"exec", "volcano-server", "/app/volcano-hosting", "local", "info", "--format", "json"}, gotArgs)
	assert.Equal(t, "http://localhost:8000", info.APIURL)
	assert.Equal(t, "00000000-0000-0000-0000-000000000000", info.ProjectID)
	assert.Equal(t, "local-dev", info.ProjectName)
	assert.Equal(t, "local-dev-user", info.UserID)
	assert.Equal(t, "11111111-1111-1111-1111-111111111111", info.AuthUserID)
	assert.Equal(t, "dev@localhost", info.AuthUserEmail)
	assert.Equal(t, "pk-local-dev-token-00000000000000000000", info.UserToken)
	assert.Equal(t, "ak-0000000000000000000000000000000000000000", info.AnonKey)
	assert.Equal(t, "sk-1111111111111111111111111111111111111111", info.ServiceKey)
	assert.Equal(t, "app", info.DefaultDatabaseName)
	assert.Equal(t, "local", info.DefaultDatabaseRegion)
	assert.Equal(t, "16", info.DefaultDatabasePostgresVersion)
	assert.Equal(t, "postgres://volcano:volcano@localhost:8002/app?sslmode=disable&application_name=volcano_full_access:app", info.DatabaseURL)
	assert.Equal(t, "redis://localhost:6379", info.RedisURL)
	assert.Equal(t, "server-owned-jwt-secret", info.JWTSecret)
	assert.Equal(t, "server-owned-encryption-key", info.EncryptionKey)
	assert.Equal(t, "server-owned-anon-secret", info.AnonKeySecret)
	assert.Equal(t, "server-owned-service-secret", info.ServiceKeySecret)
}

func TestFetchInfoSurfacesCommandError(t *testing.T) {
	_, err := FetchInfo(context.Background(), CommandRunnerFunc(func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("docker failed")
	}))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to run local info command")
	assert.Contains(t, err.Error(), "is the volcano-server container running?")
	assert.Contains(t, err.Error(), "docker failed")
}

func TestFetchInfoRejectsEmptyOutput(t *testing.T) {
	_, err := FetchInfo(context.Background(), CommandRunnerFunc(func(context.Context, string, ...string) ([]byte, error) {
		return []byte(" \n\t"), nil
	}))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty output")
}

func TestFetchInfoRejectsInvalidJSON(t *testing.T) {
	_, err := FetchInfo(context.Background(), CommandRunnerFunc(func(context.Context, string, ...string) ([]byte, error) {
		return []byte("not json"), nil
	}))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse local info output")
}

func TestInfoCommandArgsReturnsCopy(t *testing.T) {
	args := InfoCommandArgs()
	args[0] = "changed"

	assert.Equal(t, "exec", InfoCommandArgs()[0])
}

func TestInfoStringRedactsSecrets(t *testing.T) {
	info := Info{
		APIURL:           "http://localhost:8000",
		ProjectID:        "00000000-0000-0000-0000-000000000000",
		ProjectName:      "local-dev",
		UserToken:        "pk-local-dev-token-00000000000000000000",
		AnonKey:          "ak-0000000000000000000000000000000000000000",
		ServiceKey:       "sk-1111111111111111111111111111111111111111",
		DatabaseURL:      "postgres://volcano:volcano@localhost:8002/app",
		RedisURL:         "redis://localhost:6379",
		JWTSecret:        "server-owned-jwt-secret",
		EncryptionKey:    "server-owned-encryption-key",
		AnonKeySecret:    "server-owned-anon-secret",
		ServiceKeySecret: "server-owned-service-secret",
	}

	for _, rendered := range []string{
		fmt.Sprintf("%+v", info),
		fmt.Sprintf("%#v", info),
	} {
		assert.Contains(t, rendered, "local-dev")
		assert.Contains(t, rendered, redactedValue)
		for _, secret := range []string{
			"pk-local-dev-token-00000000000000000000",
			"ak-0000000000000000000000000000000000000000",
			"sk-1111111111111111111111111111111111111111",
			"postgres://volcano:volcano@localhost:8002/app",
			"redis://localhost:6379",
			"server-owned-jwt-secret",
			"server-owned-encryption-key",
			"server-owned-anon-secret",
			"server-owned-service-secret",
		} {
			assert.NotContains(t, rendered, secret)
		}
	}
}

func TestInfoPSQLCommandHint(t *testing.T) {
	info := Info{
		DatabaseURL: "postgres://user:pa'$(touch /tmp/pwned)`ss@localhost/app?sslmode=disable&application_name=volcano_full_access:app",
	}

	assert.Equal(t, "psql 'postgres://user:pa'\\''$(touch /tmp/pwned)`ss@localhost/app?sslmode=disable&application_name=volcano_full_access:app'", info.PSQLCommandHint())
}
