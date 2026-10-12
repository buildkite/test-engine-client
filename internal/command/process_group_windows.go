package command

import "os/exec"

// Windows has no process groups to signal, and can't send SIGTERM, so it
// kills the command straight away. Its descendants are not stopped.
func startProcessGroup(*exec.Cmd) {}

func stopProcessGroup(cmd *exec.Cmd) {
	_ = cmd.Process.Kill()
}

func killProcessGroup(cmd *exec.Cmd) {
	_ = cmd.Process.Kill()
}
