package output

import (
	"fmt"
	"io"

	"github.com/Kong/volcano-cli/internal/theme"
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
	on := theme.On(w)
	fmt.Fprintln(w, theme.Title("Volcano Local Development Status", on))
	fmt.Fprintln(w)
	fmt.Fprintf(w, "%s %s\n", theme.Dim("Status:", on), theme.Fail("Not running", on))
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Run %s to initialize the local development environment\n", theme.Command("'volcano start'", on))
}

// LocalModeDockerUnavailable renders Docker setup guidance for local mode.
func LocalModeDockerUnavailable(w io.Writer) {
	on := theme.On(w)
	fmt.Fprintln(w, theme.Error("Docker is not available", on))
	fmt.Fprintln(w)
	fmt.Fprintln(w, theme.Dim("Volcano needs a Docker-compatible engine to run the local development environment", on))
	fmt.Fprintln(w, theme.Dim("(Docker Desktop, OrbStack, Colima, Docker Engine, or Podman).", on))
	fmt.Fprintf(w, "Run %s to diagnose, or see %s\n", theme.Command("'volcano doctor'", on), theme.Dim("https://docs.docker.com/engine/install/", on))
}

// LocalModeStatus renders local-mode service status and metadata.
func LocalModeStatus(w io.Writer, details LocalModeStatusDetails) {
	on := theme.On(w)
	fmt.Fprintln(w, theme.Title("Volcano Local Development Status", on))
	fmt.Fprintln(w)

	fmt.Fprintln(w, theme.Title("Services:", on))
	for _, service := range details.Services {
		fmt.Fprintf(w, "  %-12s %s\n", service.Name, localModeStatusText(service.Running, on))
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, theme.Title("Project:", on))
	fmt.Fprintf(w, "  %s %s\n", theme.Dim("Project ID:", on), details.ProjectID)
	fmt.Fprintf(w, "  %s    %s\n", theme.Dim("User ID:", on), details.UserID)

	fmt.Fprintln(w)
	fmt.Fprintln(w, theme.Title("Credentials:", on))
	fmt.Fprintf(w, "  %s     %s\n", theme.Dim("API URL:", on), details.APIURL)
	fmt.Fprintf(w, "  %s    %s\n", theme.Dim("Anon Key:", on), details.AnonKey)
	fmt.Fprintf(w, "  %s %s\n", theme.Dim("Service Key:", on), details.ServiceKey)

	fmt.Fprintln(w)
	fmt.Fprintln(w, theme.Title("Database Connection:", on))
	fmt.Fprintf(w, "  %s %s\n", theme.Dim("PostgreSQL URI:", on), details.DatabaseURL)
	fmt.Fprintf(w, "  %s   %s\n", theme.Dim("psql command:", on), details.PSQLCommand)
}

func localModeStatusText(running, on bool) string {
	if running {
		return theme.Status("running", on)
	}
	return theme.Fail("not running", on)
}
