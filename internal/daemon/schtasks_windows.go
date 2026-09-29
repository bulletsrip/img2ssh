//go:build windows

package daemon

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const windowsTaskName = "img2ssh"

var (
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	procOpenProcess        = kernel32.NewProc("OpenProcess")
	procGetExitCodeProcess = kernel32.NewProc("GetExitCodeProcess")
	procCloseHandle        = kernel32.NewProc("CloseHandle")
)

const (
	processQueryLimitedInformation = 0x1000
	stillActive                    = 259
)

// InstallDaemon registers a per-user interactive logon task. It must run in
// the logged-in user's desktop session because Windows isolates clipboard
// access from background services running in session 0.
func InstallDaemon(binaryPath string) error {
	// Launch through hidden PowerShell so Task Scheduler never creates a
	// visible console window for the daemon process.
	psPath := strings.ReplaceAll(binaryPath, "'", "''")
	command := fmt.Sprintf(`powershell.exe -NoProfile -NonInteractive -WindowStyle Hidden -Command "Start-Process -FilePath '%s' -ArgumentList 'daemon' -WindowStyle Hidden"`, psPath)
	args := []string{"/Create", "/TN", windowsTaskName, "/TR", command, "/SC", "ONLOGON", "/IT", "/F"}
	if out, err := exec.Command("schtasks", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("schtasks create: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return StartDaemon()
}

func StartDaemon() error {
	if out, err := exec.Command("schtasks", "/Run", "/TN", windowsTaskName).CombinedOutput(); err != nil {
		return fmt.Errorf("schtasks run: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func UninstallDaemon() error {
	_ = exec.Command("schtasks", "/End", "/TN", windowsTaskName).Run()
	if out, err := exec.Command("schtasks", "/Delete", "/TN", windowsTaskName, "/F").CombinedOutput(); err != nil {
		return fmt.Errorf("schtasks delete: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func DaemonInstalled() bool {
	return exec.Command("schtasks", "/Query", "/TN", windowsTaskName).Run() == nil
}

func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, _, _ := procOpenProcess.Call(processQueryLimitedInformation, 0, uintptr(pid))
	if handle == 0 {
		return false
	}
	defer procCloseHandle.Call(handle)
	var code uint32
	result, _, _ := procGetExitCodeProcess.Call(handle, uintptr(unsafe.Pointer(&code)))
	return result != 0 && code == stillActive
}

// RestartRunningDaemon ends the scheduled task's process and starts a new
// interactive instance. taskkill is needed because Windows does not provide a
// catchable SIGTERM equivalent for a scheduled console process.
func RestartRunningDaemon(pid int) error {
	if ProcessAlive(pid) {
		out, err := exec.Command("taskkill", "/PID", fmt.Sprintf("%d", pid), "/T", "/F").CombinedOutput()
		if err != nil && ProcessAlive(pid) {
			return fmt.Errorf("taskkill daemon: %s: %w", strings.TrimSpace(string(out)), err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for ProcessAlive(pid) && time.Now().Before(deadline) {
			time.Sleep(100 * time.Millisecond)
		}
		if ProcessAlive(pid) {
			return fmt.Errorf("daemon process %d did not stop", pid)
		}
	}
	return StartDaemon()
}

func quoteWindowsArg(arg string) string {
	var out strings.Builder
	out.WriteByte('"')
	backslashes := 0
	for _, r := range arg {
		if r == '\\' {
			backslashes++
			continue
		}
		if r == '"' {
			out.WriteString(strings.Repeat(`\`, backslashes*2+1))
			out.WriteRune(r)
			backslashes = 0
			continue
		}
		if backslashes > 0 {
			out.WriteString(strings.Repeat(`\`, backslashes))
			backslashes = 0
		}
		out.WriteRune(r)
	}
	if backslashes > 0 {
		out.WriteString(strings.Repeat(`\`, backslashes*2))
	}
	out.WriteByte('"')
	return out.String()
}
