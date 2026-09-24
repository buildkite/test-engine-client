package runnerexec

import (
	"context"
	"errors"
	"os/exec"
)

// Run is not yet implemented on Windows. Modern Windows supports Unix-domain
// stream sockets, so future support could reuse the HTTP protocol unchanged.
// It would need Windows process-tree supervision and cancellation, socket access
// controls using Windows ACLs, and verification of the runner runtime's socket
// support. The Unix implementation's process groups and chmod are not portable.
func Run(context.Context, *exec.Cmd, Source, Options) error {
	return errors.New("persistent runners require Unix")
}
