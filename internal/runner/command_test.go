package runner

import (
	"errors"
	"os"
	"os/exec"
	"strings"
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

func TestEnvironmentWithOverridesReplacesInheritedValues(t *testing.T) {
	got := environmentWithOverrides(
		[]string{"KEEP=original", "TOKEN=old", "TOKEN=duplicate"},
		map[string]string{"TOKEN": "local", "ADDED": "value"},
	)

	values := make(map[string]string)
	for _, entry := range got {
		key, value, _ := strings.Cut(entry, "=")
		if _, duplicate := values[key]; duplicate {
			t.Errorf("environment contains duplicate key %q: %v", key, got)
		}
		values[key] = value
	}
	want := map[string]string{"KEEP": "original", "TOKEN": "local", "ADDED": "value"}
	if len(values) != len(want) {
		t.Fatalf("environmentWithOverrides() = %v, want %v", values, want)
	}
	for key, value := range want {
		if values[key] != value {
			t.Errorf("environmentWithOverrides()[%q] = %q, want %q", key, values[key], value)
		}
	}
}

func TestBuildCommandUsesTestProcessEnvironment(t *testing.T) {
	testRunner := NewRspec(RunnerConfig{
		TestCommand:    "echo {{testExamples}}",
		uploadToken:    "upstream-token",
		testProcessEnv: map[string]string{"BUILDKITE_TESTS_OTLP_TOKEN": "local-token", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": "http://127.0.0.1:1234/v1/traces"},
	})
	cmd, err := buildCommand(testRunner, nil, false)
	if err != nil {
		t.Fatalf("buildCommand() error = %v", err)
	}

	values := make(map[string]string)
	for _, entry := range cmd.Env {
		key, value, _ := strings.Cut(entry, "=")
		values[key] = value
	}
	if got := values["BUILDKITE_ANALYTICS_TOKEN"]; got != "upstream-token" {
		t.Errorf("BUILDKITE_ANALYTICS_TOKEN = %q, want upstream-token", got)
	}
	if got := values["BUILDKITE_TESTS_OTLP_TOKEN"]; got != "local-token" {
		t.Errorf("BUILDKITE_TESTS_OTLP_TOKEN = %q, want local-token", got)
	}
	if got := values["OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"]; got != "http://127.0.0.1:1234/v1/traces" {
		t.Errorf("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT = %q", got)
	}
}
