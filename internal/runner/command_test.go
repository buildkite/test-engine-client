package runner

import (
	"errors"
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
	if !errors.As(err, &exitError) {
		t.Fatalf("runAndForwardSignal(cmd) error type = %T (%v), want  *exec.ExitError", err, err)
	}
	if exitError.ExitCode() != 1 {
		t.Errorf("exitError.ExitCode() = %d, want 1", exitError.ExitCode())
	}
}

func TestRunAndForwardSignal_SignalReceivedInMainProcess(t *testing.T) {
	cmd := exec.Command("sleep", "10")

	// Send a SIGTERM signal to the main process.
	go func() {
		pid := os.Getpid()
		process, err := os.FindProcess(pid)
		if err != nil {
			t.Errorf("os.FindProcess(%d) error = %v", pid, err)
		}
		time.Sleep(300 * time.Millisecond)
		process.Signal(syscall.SIGTERM)
	}()

	err := runAndForwardSignal(cmd)

	signalError := new(ProcessSignaledError)
	if !errors.As(err, &signalError) {
		t.Errorf("runAndForwardSignal(cmd) error type = %T (%v), want *ErrProcessSignaled", err, err)
	}
	if signalError.Signal != syscall.SIGTERM {
		t.Errorf("runAndForwardSignal(cmd) signal = %d, want  %d", syscall.SIGTERM, signalError.Signal)
	}
}

func TestRunAndForwardSignal_SignalReceivedInSubProcess(t *testing.T) {
	cmd := exec.Command("./testdata/segv.sh")

	err := runAndForwardSignal(cmd)

	signalError := new(ProcessSignaledError)
	if !errors.As(err, &signalError) {
		t.Errorf("runAndForwardSignal(cmd) error type = %T (%v), want *ErrProcessSignaled", err, err)
	}
	if signalError.Signal != syscall.SIGSEGV {
		t.Errorf("runAndForwardSignal(cmd) signal = %d, want  %d", syscall.SIGSEGV, signalError.Signal)
	}
}
