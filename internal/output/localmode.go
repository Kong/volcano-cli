package output

import (
	"fmt"
	"io"
)

// LocalModeServiceStatus is one local-mode service status row.
type LocalModeServiceStatus struct {
	Name    string
	Running bool
}

// LocalModeStatusDetails contains the data rendered by `volcano status`.
type LocalModeStatusDetails struct {
	Services    []LocalModeServiceStatus
	ProjectID   string
	UserID      string
	APIURL      string
	AnonKey     string
	ServiceKey  string
	DatabaseURL string
	PSQLCommand string
}

// LocalModeNotRunning renders the local-mode status when the stack is stopped.
func LocalModeNotRunning(w io.Writer) {
	fmt.Fprintln(w, "Volcano Local Development Status")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Status: Not running")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Run 'volcano start' to initialize the local development environment")
}

// LocalModeDockerUnavailable renders Docker setup guidance for local mode.
func LocalModeDockerUnavailable(w io.Writer) {
	fmt.Fprintln(w, "Docker is not available")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Volcano needs a Docker-compatible engine to run the local development environment")
	fmt.Fprintln(w, "(Docker Desktop, OrbStack, Colima, Docker Engine, or Podman).")
	fmt.Fprintln(w, "Run 'volcano doctor' to diagnose, or see https://docs.docker.com/engine/install/")
}

// LocalModeStatus renders local-mode service status and metadata.
func LocalModeStatus(w io.Writer, details LocalModeStatusDetails) {
	fmt.Fprintln(w, "Volcano Local Development Status")
	fmt.Fprintln(w)

	fmt.Fprintln(w, "Services:")
	for _, service := range details.Services {
		fmt.Fprintf(w, "  %-12s %s\n", service.Name, localModeStatusText(service.Running))
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Project:")
	fmt.Fprintf(w, "  Project ID: %s\n", details.ProjectID)
	fmt.Fprintf(w, "  User ID:    %s\n", details.UserID)

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Credentials:")
	fmt.Fprintf(w, "  API URL:     %s\n", details.APIURL)
	fmt.Fprintf(w, "  Anon Key:    %s\n", details.AnonKey)
	fmt.Fprintf(w, "  Service Key: %s\n", details.ServiceKey)

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Database Connection:")
	fmt.Fprintf(w, "  PostgreSQL URI: %s\n", details.DatabaseURL)
	fmt.Fprintf(w, "  psql command:   %s\n", details.PSQLCommand)
}

func localModeStatusText(running bool) string {
	if running {
		return "running"
	}
	return "not running"
}
