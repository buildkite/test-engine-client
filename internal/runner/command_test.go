package runner

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
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
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT} {
		t.Run(sig.String(), func(t *testing.T) {
			if signal.Ignored(sig) {
				t.Skipf("the test process was started with %v ignored, so it isn't forwarded", sig)
			}

			// The command sends the signal to the main process as soon as it
			// starts. If the main process weren't listening yet, the signal
			// would stop it instead of being forwarded.
			cmd := exec.Command("sh", "-c", fmt.Sprintf("kill -%d $PPID; exec sleep 10", sig))

			err := runAndForwardSignal(cmd)

			signalError := new(ProcessSignaledError)
			if !errors.As(err, &signalError) {
				t.Errorf("runAndForwardSignal(cmd) error type = %T (%v), want *ErrProcessSignaled", err, err)
			}
			if signalError.Signal != sig {
				t.Errorf("runAndForwardSignal(cmd) signal = %d, want  %d", signalError.Signal, sig)
			}
		})
	}
}

func TestRunAndForwardSignal_KeepsIgnoredSignalsIgnored(t *testing.T) {
	if os.Getenv("BKTEC_TEST_IGNORED_SIGHUP") == "1" {
		// This process was started with SIGHUP ignored. The command sends
		// itself SIGHUP and must inherit the ignore to survive it.
		err := runAndForwardSignal(exec.Command("sh", "-c", "kill -HUP $$"))
		if err != nil {
			t.Fatalf("runAndForwardSignal(cmd) error = %v, want nil", err)
		}
		return
	}
	cmd := exec.Command("nohup", os.Args[0], "-test.run=^TestRunAndForwardSignal_KeepsIgnoredSignalsIgnored$")
	cmd.Env = append(os.Environ(), "BKTEC_TEST_IGNORED_SIGHUP=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("test process started with nohup: %v\n%s", err, output)
	}
}

func TestRunAndForwardSignal_BrokenPipeInMainProcess(t *testing.T) {
	// Writing to a closed connection, such as when bktec closes an API
	// connection that was reset, raises SIGPIPE in bktec. The command must
	// keep running: like most commands, sleep is killed by SIGPIPE.
	cmd := exec.Command("sleep", "1")

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	reader.Close()
	defer writer.Close()

	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				// Each write fails with EPIPE and raises SIGPIPE in this process.
				writer.Write([]byte("x"))
			}
		}
	}()

	err = runAndForwardSignal(cmd)
	if err != nil {
		t.Errorf("runAndForwardSignal(cmd) error = %v, want nil", err)
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
	cmd, err := BuildCommand(testRunner, nil, false)
	if err != nil {
		t.Fatalf("BuildCommand() error = %v", err)
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
