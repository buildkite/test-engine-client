package runner

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/kballard/go-shellquote"
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

func TestCommandNameAndArgs_WithInterpolationPlaceholder(t *testing.T) {
	testCases := []string{"spec/models/user_spec.rb", "spec/models/billing_spec.rb"}
	testCommand := "bin/rspec --options {{testExamples}} --format"

	gotName, gotArgs, err := commandNameAndArgs(testCommand, testCases)
	if err != nil {
		t.Errorf("commandNameAndArgs(%q, %q) error = %v", testCases, testCommand, err)
	}

	wantName := "bin/rspec"
	wantArgs := []string{"--options", "spec/models/user_spec.rb", "spec/models/billing_spec.rb", "--format"}

	if diff := cmp.Diff(gotName, wantName); diff != "" {
		t.Errorf("commandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("commandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
}

func TestCommandNameAndArgs_WithoutInterpolationPlaceholder(t *testing.T) {
	testCases := []string{"spec/models/user_spec.rb", "spec/models/billing_spec.rb"}
	testCommand := "bin/rspec --options --format"

	gotName, gotArgs, err := commandNameAndArgs(testCommand, testCases)
	if err != nil {
		t.Errorf("commandNameAndArgs(%q, %q) error = %v", testCases, testCommand, err)
	}

	wantName := "bin/rspec"
	wantArgs := []string{"--options", "--format", "spec/models/user_spec.rb", "spec/models/billing_spec.rb"}

	if diff := cmp.Diff(gotName, wantName); diff != "" {
		t.Errorf("commandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("commandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
}

func TestCommandNameAndArgs_InvalidTestCommand(t *testing.T) {
	testCases := []string{"spec/models/user_spec.rb", "spec/models/billing_spec.rb"}
	testCommand := "bin/rspec --options ' {{testExamples}}"

	gotName, gotArgs, err := commandNameAndArgs(testCommand, testCases)

	wantName := ""
	wantArgs := []string{}

	if diff := cmp.Diff(gotName, wantName); diff != "" {
		t.Errorf("commandNameAndArgs() diff (-got +want):\n%s", diff)
	}
	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("commandNameAndArgs() diff (-got +want):\n%s", diff)
	}
	if !errors.Is(err, shellquote.UnterminatedSingleQuoteError) {
		t.Errorf("commandNameAndArgs() error = %v, want %v", err, shellquote.UnterminatedSingleQuoteError)
	}
}
