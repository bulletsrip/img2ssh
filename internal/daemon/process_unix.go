//go:build !windows

package daemon

import (
	"os"
	"syscall"
)

func ProcessAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// RestartRunningDaemon asks the service manager to restart the daemon after
// the process exits, matching the existing launchd/systemd behavior.
func RestartRunningDaemon(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Signal(syscall.SIGTERM)
}
