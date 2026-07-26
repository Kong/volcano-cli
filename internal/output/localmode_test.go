package output

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLocalModeNotRunning(t *testing.T) {
	var out bytes.Buffer

	LocalModeNotRunning(&out)

	assert.Equal(t, "Volcano Local Development Status\n\nStatus: Not running\n\nRun 'volcano start' to initialize the local development environment\n", out.String())
}

func TestLocalModeDockerUnavailable(t *testing.T) {
	var out bytes.Buffer

	LocalModeDockerUnavailable(&out)

	assert.Contains(t, out.String(), "Docker is not available")
	assert.Contains(t, out.String(), "Docker-compatible engine")
	assert.Contains(t, out.String(), "volcano doctor")
	assert.Contains(t, out.String(), "https://docs.docker.com/engine/install/")
}

func TestLocalModeStatus(t *testing.T) {
	var out bytes.Buffer

	LocalModeStatus(&out, LocalModeStatusDetails{
		Services: []LocalModeServiceStatus{
			{Name: "PostgreSQL", Running: true},
			{Name: "Redis", Running: false},
		},
		ProjectID:   "project-id",
		UserID:      "user-id",
		APIURL:      "http://localhost:8000",
		AnonKey:     "anon-key",
		ServiceKey:  "service-key",
		DatabaseURL: "postgres://example",
		PSQLCommand: "psql postgres://example",
	})

	for _, want := range []string{
		"Volcano Local Development Status",
		"PostgreSQL   running",
		"Redis        not running",
		"Project ID: project-id",
		"User ID:    user-id",
		"API URL:     http://localhost:8000",
		"Anon Key:    anon-key",
		"Service Key: service-key",
		"PostgreSQL URI: postgres://example",
		"psql command:   psql postgres://example",
	} {
		assert.Contains(t, out.String(), want)
	}
}
