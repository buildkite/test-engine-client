package runner

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestRunAndForwardSignal(t *testing.T) {
	cmd := exec.Command("echo", "hello world")

	err := runAndForwardSignal(cmd)
	if err != nil {
		t.Fatal(err)
	}
}

func TestRunAndForwardSignal_CommandExitsWithNonZero(t *testing.T) {
	cmd := exec.Command("false")

	err := runAndForwardSignal(cmd)
	exitError := new(exec.ExitError)
	if errors.As(err, &exitError) && exitError.ExitCode() != 1 {
		t.Errorf("Expected exit code 1, but got %d", exitError.ExitCode())
	}
}

func TestRunAndForwardSignal_SignalReceivedInMainProcess(t *testing.T) {
	cmd := exec.Command("sleep", "10")

	// Send a SIGTERM signal to the main process.
	go func() {
		pid := os.Getpid()
		process, err := os.FindProcess(pid)
		if err != nil {
			fmt.Println(err)
		}
		time.Sleep(300 * time.Millisecond)
		process.Signal(syscall.SIGTERM)
	}()

	err := runAndForwardSignal(cmd)

	signalError := new(ProcessSignaledError)
	if !errors.As(err, &signalError) {
		t.Errorf("Expected ErrProcessSignaled, but got %v", err)
	}
	if signalError.Signal != syscall.SIGTERM {
		t.Errorf("Expected signal %d, but got %d", syscall.SIGTERM, signalError.Signal)
	}
}

func TestRunAndForwardSignal_SignalReceivedInSubProcess(t *testing.T) {
	cmd := exec.Command("../../test/support/segv.sh")

	err := runAndForwardSignal(cmd)

	signalError := new(ProcessSignaledError)
	if !errors.As(err, &signalError) {
		t.Errorf("Expected ErrProcessSignaled, but got %v", err)
	}
	if signalError.Signal != syscall.SIGSEGV {
		t.Errorf("Expected signal %d, but got %d", syscall.SIGSEGV, signalError.Signal)
	}
}
