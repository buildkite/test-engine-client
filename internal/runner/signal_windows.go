//go:build windows

package runner

import "os"

// isIgnoredSignal checks if the signal should be ignored.
// On Windows, there isn't a direct equivalent to SIGCHLD that needs ignoring in this context.
var isIgnoredSignal = func(sig os.Signal) bool {
	return false
}
