package runner

import (
	"fmt"
	"syscall"
)

type ProcessSignaledError struct {
	Signal syscall.Signal
}

func (e *ProcessSignaledError) Error() string {
	return fmt.Sprintf("process was signaled with signal %d", e.Signal)
}
