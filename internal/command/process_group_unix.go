//go:build !windows

package command

import (
	"os/exec"
	"syscall"
)

func startProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.SysProcAttr.Pgid = 0
}

// stopProcessGroup interrupts the group like Ctrl+C in a terminal. Playwright
// only cleans up on SIGINT: on SIGTERM it exits without stopping its webServer,
// which runs in its own process group and would keep the port for the next
// command.
func stopProcessGroup(cmd *exec.Cmd) {
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGINT)
}

func killProcessGroup(cmd *exec.Cmd) {
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
