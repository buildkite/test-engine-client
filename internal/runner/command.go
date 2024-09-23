package runner

import (
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

// runAndForwardSignal runs the command and forwards any signals received to the command.
func runAndForwardSignal(cmd *exec.Cmd) error {
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout

	// Create a channel that will be closed when the command finishes.
	finishCh := make(chan struct{})
	defer close(finishCh)

	if err := cmd.Start(); err != nil {
		return err
	}

	// Start a goroutine that waits for a signal or the command to finish.
	go func() {
		// Create another channel to receive the signals.
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh)

		// Wait for a signal to be received or the command to finish.
		// Because a message can come through both channels asynchronously,
		// we use for loop to listen to both channels and select the one that has a message.
		// Without for loop, only one case would be selected and the other would be ignored.
		// If the signal is received first, the finishCh will never get processed and the goroutine will run forever.
		for {
			select {
			case sig := <-sigCh:
				// When the subprocess exits, it sends SIGCHLD to the parent process.
				// We ignore this signal because we don't want to forward it back to the subprocess.
				if sig == syscall.SIGCHLD {
					continue
				}
				// Ignore the error when sending the signal to the command.
				_ = cmd.Process.Signal(sig)
			case <-finishCh:
				// When the the command finishes, we stop listening for signals and return.
				signal.Stop(sigCh)
				return
			}
		}
	}()

	// Wait for the command to finish.
	err := cmd.Wait()

	if err != nil {
		// If the command was signaled, return a ProcessProcessSignaledError.
		if exitError, ok := err.(*exec.ExitError); ok {
			if status, ok := exitError.Sys().(syscall.WaitStatus); ok && status.Signaled() {
				return &ProcessSignaledError{Signal: status.Signal()}
			}
		}
		return err
	}

	return nil
}
