package runnerexec

import (
	"context"
	"errors"
	"os/exec"
)

// Run currently requires Unix process groups and a Unix domain socket.
func Run(context.Context, *exec.Cmd, Source, Options) error {
	return errors.New("persistent runners require Unix")
}
