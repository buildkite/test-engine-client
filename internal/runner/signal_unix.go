//go:build !windows

package runner

import (
	"os"
	"syscall"
)

// isIgnoredSignal checks if the signal should be ignored.
// On Unix-like systems, we ignore SIGCHLD, which is sent when a child process terminates.
var isIgnoredSignal = func(sig os.Signal) bool {
	return sig == syscall.SIGCHLD
}
