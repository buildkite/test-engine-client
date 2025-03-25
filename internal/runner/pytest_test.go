package runner

import (
	"errors"
	"os/exec"
	"testing"

	"github.com/buildkite/test-engine-client/internal/plan"
	"github.com/google/go-cmp/cmp"
	"github.com/kballard/go-shellquote"
)

func TestPytestRun(t *testing.T) {
	changeCwd(t, "./testdata/pytest")

	pytest := NewPytest(RunnerConfig{})
	testCases := []plan.TestCase{
		{Path: "test_sample.py"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := pytest.Run(result, testCases, false)

	if err != nil {
		t.Errorf("Pytest.Run(%q) error = %v", testCases, err)
	}

	if result.Status() != RunStatusUnknown {
		t.Errorf("Pytest.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusUnknown)
	}
}

func TestPytestRun_TestFailed(t *testing.T) {
	changeCwd(t, "./testdata/pytest")

	pytest := NewPytest(RunnerConfig{})
	testCases := []plan.TestCase{
		{Path: "failed_test.py"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := pytest.Run(result, testCases, false)

	if result.Status() != RunStatusUnknown {
		t.Errorf("Pytest.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusUnknown)
	}

	exitError := new(exec.ExitError)
	if !errors.As(err, &exitError) {
		t.Errorf("Pytest.Run(%q) error type = %T (%v), want *exec.ExitError", testCases, err, err)
	}
}

func TestPytestRun_CommandFailed(t *testing.T) {
	changeCwd(t, "./testdata/pytest")

	pytest := NewPytest(RunnerConfig{
		TestCommand: "pytest help",
	})

	testCases := []plan.TestCase{
		{Path: "test_sample.py"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := pytest.Run(result, testCases, false)

	if result.Status() != RunStatusUnknown {
		t.Errorf("Pytest.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusUnknown)
	}

	exitError := new(exec.ExitError)
	if !errors.As(err, &exitError) {
		t.Errorf("Pytest.Run(%q) error type = %T (%v), want *exec.ExitError", testCases, err, err)
	}
}

func TestPytestGetFiles(t *testing.T) {
	pytest := NewPytest(RunnerConfig{})

	got, err := pytest.GetFiles()
	if err != nil {
		t.Errorf("Pytest.GetFiles() error = %v", err)
	}

	want := []string{
		"testdata/pytest/failed_test.py",
		"testdata/pytest/test_sample.py",
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Pytest.GetFiles() diff (-got +want):\n%s", diff)
	}
}

func TestPytestCommandNameAndArgs_WithInterpolationPlaceholder(t *testing.T) {
	testCases := []string{"failed_test.py", "test_sample.py"}
	testCommand := "pytest {{testExamples}} --full-trace"

	pytest := NewPytest(RunnerConfig{
		TestCommand: testCommand,
	})

	gotName, gotArgs, err := pytest.commandNameAndArgs(testCommand, testCases)
	if err != nil {
		t.Errorf("commandNameAndArgs(%q, %q) error = %v", testCases, testCommand, err)
	}

	wantName := "pytest"
	wantArgs := []string{"failed_test.py", "test_sample.py", "--full-trace"}

	if diff := cmp.Diff(gotName, wantName); diff != "" {
		t.Errorf("commandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("commandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
}

func TestPytestCommandNameAndArgs_WithoutTestExamplesPlaceholder(t *testing.T) {
	testCases := []string{"failed_test.py", "test_sample.py"}
	testCommand := "pytest --full-trace"

	pytest := NewPytest(RunnerConfig{
		TestCommand: testCommand,
	})

	gotName, gotArgs, err := pytest.commandNameAndArgs(testCommand, testCases)
	if err != nil {
		t.Errorf("commandNameAndArgs(%q, %q) error = %v", testCases, testCommand, err)
	}

	wantName := "pytest"
	wantArgs := []string{"--full-trace", "failed_test.py", "test_sample.py"}

	if diff := cmp.Diff(gotName, wantName); diff != "" {
		t.Errorf("commandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("commandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
}

func TestPytestCommandNameAndArgs_InvalidTestCommand(t *testing.T) {
	testCases := []string{"failed_test.py", "test_sample.py"}
	testCommand := "pytest '{{testExamples}}"

	pytest := NewPytest(RunnerConfig{
		TestCommand: testCommand,
	})

	gotName, gotArgs, err := pytest.commandNameAndArgs(testCommand, testCases)

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
