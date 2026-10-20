//go:build !windows

package telemetry

import (
	"os/exec"
	"syscall"
)

// detach starts cmd in its own session, so closing the terminal that ran the
// parent command does not kill an in-flight flush.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
