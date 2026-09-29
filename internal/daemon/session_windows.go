//go:build windows

package daemon

// Windows can establish the configured SSH connection on demand. Requiring an
// already-open ssh.exe session prevents unattended clipboard syncing, so the
// daemon always permits the sync loop to use its configured key and host.
func hasActiveSSHSession() bool {
	return true
}
