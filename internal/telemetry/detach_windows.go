//go:build windows

package telemetry

import (
	"os/exec"
	"syscall"
)

// detach starts cmd without a console and outside the parent's process group,
// so it outlives the command that spawned it.
func detach(cmd *exec.Cmd) {
	const detachedProcess = 0x00000008
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | detachedProcess,
	}
}
