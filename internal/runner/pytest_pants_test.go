package runner

import (
	"errors"
	"os/exec"
	"testing"

	"github.com/buildkite/test-engine-client/internal/plan"
	"github.com/google/go-cmp/cmp"
	"github.com/kballard/go-shellquote"
)

// test cases are not supported in pytest pants at this time. It's required to
// have all tests cases passed to the test command.
// TODO: add support for test cases in pytest pants. This is a temporary
// workaround to allow bktec to run pytest pants.

func TestPytestPantsRun(t *testing.T) {
	changeCwd(t, "./testdata/pytest_pants")

	pytest := NewPytestPants(RunnerConfig{
		TestCommand: "pants test //passing_test.py -- --json={{resultPath}} --merge-json",
	})

	testCases := []plan.TestCase{
		{Path: "passing_test.py"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := pytest.Run(result, testCases, false)

	if err != nil {
		t.Errorf("PytestPants.Run(%q) error = %v", testCases, err)
	}

	if result.Status() != RunStatusPassed {
		t.Errorf("PytestPants.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusPassed)
	}
}

func TestPytestPantsRun_RetryCommand(t *testing.T) {
	changeCwd(t, "./testdata/pytest_pants")

	pytest := NewPytestPants(RunnerConfig{
		TestCommand:      "pants test //failing_test.py -- --json={{resultPath}} --merge-json",
		RetryTestCommand: "pants test //passing_test.py -- --json={{resultPath}} --merge-json",
	})

	testCases := []plan.TestCase{
		{Path: "passing_test.py"},
	}

	result := NewRunResult([]plan.TestCase{})
	err := pytest.Run(result, testCases, true)

	if err != nil {
		t.Errorf("PytestPants.Run(%q) error = %v", testCases, err)
	}
}

func TestPytestPantsRun_TestFailed(t *testing.T) {
	changeCwd(t, "./testdata/pytest_pants")

	pytest := NewPytestPants(RunnerConfig{
		TestCommand: "pants test //:: -- --json={{resultPath}} --merge-json",
		ResultPath:  "result-failed.json",
	})
	testCases := []plan.TestCase{
		{Path: "failing_test.py"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := pytest.Run(result, testCases, false)

	if err != nil {
		t.Errorf("PytestPants.Run(%q) error = %v", testCases, err)
	}

	if result.Status() != RunStatusFailed {
		t.Errorf("PytestPants.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusFailed)
	}

	failedTest := result.FailedTests()

	if len(failedTest) != 1 {
		t.Errorf("len(result.FailedTests()) = %d, want 1", len(failedTest))
	}

	wantFailedTests := []plan.TestCase{
		{
			Format:     "example",
			Identifier: "a1be7e52-0dba-4018-83ce-a1598ca68807",
			Name:       "test_failed",
			Path:       "tests/failing_test.py::test_failed",
			Scope:      "tests/failing_test.py",
		},
	}

	if diff := cmp.Diff(failedTest, wantFailedTests); diff != "" {
		t.Errorf("PytestPants.Run(%q) RunResult.FailedTests() diff (-got +want):\n%s", testCases, diff)
	}
}

func TestPytestPantsRun_CommandFailed(t *testing.T) {
	changeCwd(t, "./testdata/pytest_pants")

	pytest := NewPytestPants(RunnerConfig{
		TestCommand: "pants test //:: -- --non-existent-pytest-option --json={{resultPath}} --merge-json",
	})

	testCases := []plan.TestCase{
		{Path: "passing_test.py"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := pytest.Run(result, testCases, false)

	if result.Status() != RunStatusUnknown {
		t.Errorf("PytestPants.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusUnknown)
	}

	exitError := new(exec.ExitError)
	if !errors.As(err, &exitError) {
		t.Errorf("PytestPants.Run(%q) error type = %T (%v), want *exec.ExitError", testCases, err, err)
	}
}

func TestPytestPantsGetFiles(t *testing.T) {
	pytest := NewPytestPants(RunnerConfig{
		TestCommand: "pants test //:: -- --json={{resultPath}} --merge-json",
	})

	got, err := pytest.GetFiles()
	if err != nil {
		t.Errorf("PytestPants.GetFiles() error = %v", err)
	}

	// PytestPants doesn't support file discovery
	want := []string{}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("PytestPants.GetFiles() diff (-got +want):\n%s", diff)
	}
}

func TestPytestPantsGetExamples(t *testing.T) {
	pytest := NewPytestPants(RunnerConfig{
		TestCommand: "pants test //:: -- --json={{resultPath}} --merge-json",
	})

	got, err := pytest.GetExamples([]string{})
	if err == nil {
		t.Error("PytestPants.GetExamples() error = nil, want error")
	}

	if got != nil {
		t.Errorf("PytestPants.GetExamples() = %v, want nil", got)
	}
}

func TestPytestPantsCommandNameAndArgs_WithoutMergeJson(t *testing.T) {
	testCases := []string{"failing_test.py", "passing_test.py"}
	testCommand := "pants test //:: -- --json={{resultPath}}"

	pytest := NewPytestPants(RunnerConfig{
		TestCommand: testCommand,
		ResultPath:  "result.json",
	})

	_, _, err := pytest.commandNameAndArgs(testCommand, testCases)
	if err == nil {
		t.Error("commandNameAndArgs() error = nil, want error")
	}
}

func TestPytestPantsCommandNameAndArgs_WithoutResultPath(t *testing.T) {
	testCases := []string{"failing_test.py", "passing_test.py"}
	testCommand := "pants test //:: -- --merge-json"

	pytest := NewPytestPants(RunnerConfig{
		TestCommand: testCommand,
	})

	_, _, err := pytest.commandNameAndArgs(testCommand, testCases)
	if err == nil {
		t.Error("commandNameAndArgs() error = nil, want error")
	}
}

func TestPytestPantsCommandNameAndArgs_PytestArgsBeforeDash(t *testing.T) {
	testCases := []string{"failing_test.py", "passing_test.py"}
	testCommand := "pants test --json={{resultPath}} --merge-json //::"

	pytest := NewPytestPants(RunnerConfig{
		TestCommand: testCommand,
	})

	_, _, err := pytest.commandNameAndArgs(testCommand, testCases)
	if err == nil {
		t.Error("commandNameAndArgs() error = nil, want error")
	}
}

func TestPytestPantsCommandNameAndArgs_NoDashSeparator(t *testing.T) {
	testCases := []string{"failing_test.py", "passing_test.py"}
	testCommand := "pants test //:: --json={{resultPath}} --merge-json"

	pytest := NewPytestPants(RunnerConfig{
		TestCommand: testCommand,
	})

	gotName, gotArgs, err := pytest.commandNameAndArgs(testCommand, testCases)
	if err == nil {
		t.Error("commandNameAndArgs() error = nil, want error")
	}

	wantName := ""
	wantArgs := []string{}

	if diff := cmp.Diff(gotName, wantName); diff != "" {
		t.Errorf("commandNameAndArgs() diff (-got +want):\n%s", diff)
	}
	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("commandNameAndArgs() diff (-got +want):\n%s", diff)
	}
}

func TestPytestPantsCommandNameAndArgs_ValidCommand(t *testing.T) {
	testCases := []string{"failing_test.py", "passing_test.py"}
	testCommand := "pants test //:: -- --json={{resultPath}} --merge-json"

	pytest := NewPytestPants(RunnerConfig{
		TestCommand: testCommand,
		ResultPath:  "result.json",
	})

	gotName, gotArgs, err := pytest.commandNameAndArgs(testCommand, testCases)
	if err != nil {
		t.Errorf("commandNameAndArgs(%q, %q) error = %v", testCases, testCommand, err)
	}

	wantName := "pants"
	wantArgs := []string{"test", "//::", "--", "--json=result.json", "--merge-json"}

	if diff := cmp.Diff(gotName, wantName); diff != "" {
		t.Errorf("commandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("commandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
}

func TestPytestPantsCommandNameAndArgs_InvalidTestCommand(t *testing.T) {
	testCases := []string{"failing_test.py", "passing_test.py"}
	testCommand := "pants test //:: -- --json={{resultPath}}' --merge-json"

	pytest := NewPytestPants(RunnerConfig{
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
