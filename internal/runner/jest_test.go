package runner

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"

	"github.com/buildkite/test-engine-client/internal/plan"
	"github.com/google/go-cmp/cmp"
	"github.com/kballard/go-shellquote"
)

func TestNewJest(t *testing.T) {
	cases := []struct {
		input RunnerConfig
		want  RunnerConfig
	}{
		//default
		{
			input: RunnerConfig{},
			want: RunnerConfig{
				TestCommand:            "npx jest {{testExamples}} --json --testLocationInResults --outputFile {{resultPath}}",
				TestFilePattern:        "**/{__tests__/**/*,*.spec,*.test}.{ts,js,tsx,jsx}",
				TestFileExcludePattern: "",
				RetryTestCommand:       "npx jest --testNamePattern '{{testNamePattern}}' --json --testLocationInResults --outputFile {{resultPath}}",
			},
		},
		// custom
		{
			input: RunnerConfig{
				TestCommand:            "yarn test --json --outputFile {{resultPath}}",
				TestFilePattern:        "spec/models/**/*.spec.js",
				TestFileExcludePattern: "spec/features/**/*.spec.js",
				RetryTestCommand:       "yarn test --testNamePattern '{{testNamePattern}}' --json --testLocationInResults --outputFile {{resultPath}}",
			},
			want: RunnerConfig{
				TestCommand:            "yarn test --json --outputFile {{resultPath}}",
				TestFilePattern:        "spec/models/**/*.spec.js",
				TestFileExcludePattern: "spec/features/**/*.spec.js",
				RetryTestCommand:       "yarn test --testNamePattern '{{testNamePattern}}' --json --testLocationInResults --outputFile {{resultPath}}",
			},
		},
	}

	for _, c := range cases {
		got := NewJest(c.input)
		if diff := cmp.Diff(got.RunnerConfig, c.want); diff != "" {
			t.Errorf("NewJest(%v) diff (-got +want):\n%s", c.input, diff)
		}
	}
}

func TestJestRun(t *testing.T) {
	changeCwd(t, "./testdata/jest")

	jest := NewJest(RunnerConfig{
		TestCommand: "npx jest --json --outputFile {{resultPath}}",
		ResultPath:  "jest.json",
	})

	t.Cleanup(func() {
		os.Remove(jest.ResultPath)
	})

	testCases := []plan.TestCase{
		{Path: "./testdata/jest/spells/expelliarmus.spec.js"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := jest.Run(result, testCases, false)

	if err != nil {
		t.Errorf("Jest.Run(%q) error = %v", testCases, err)
	}

	if len(result.tests) != 1 {
		t.Errorf("Rspec.Run(%q) len(RunResult.tests) = %d, want 1", testCases, len(result.tests))
	}

	if result.Status() != RunStatusPassed {
		t.Errorf("Jest.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusPassed)
	}
}

func TestJestRun_Retry(t *testing.T) {
	changeCwd(t, "./testdata/jest")

	jest := NewJest(RunnerConfig{
		TestCommand:      "jest --invalid-option --json --outputFile {{resultPath}}",
		RetryTestCommand: "npx jest --testNamePattern '{{testNamePattern}}' --json --outputFile {{resultPath}} ./testdata/jest/spells/expelliarmus.spec.js ./testdata/jest/failure.spec.js",
		ResultPath:       "jest.json",
	})

	t.Cleanup(func() {
		os.Remove(jest.ResultPath)
	})

	testCases := []plan.TestCase{
		{Scope: "expelliarmus", Name: "disarms the opponent"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := jest.Run(result, testCases, true)

	if err != nil {
		t.Errorf("Jest.Run(%q) error = %v", testCases, err)
	}

	if result.Status() != RunStatusPassed {
		t.Errorf("Jest.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusPassed)
	}
}

func TestJestRun_TestFailed(t *testing.T) {
	changeCwd(t, "./testdata/jest")

	jest := NewJest(RunnerConfig{
		TestCommand: "npx jest --json --outputFile {{resultPath}}",
		ResultPath:  "jest.json",
	})

	t.Cleanup(func() {
		os.Remove(jest.ResultPath)
	})

	testCases := []plan.TestCase{
		{Path: "./testdata/jest/failure.spec.js"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := jest.Run(result, testCases, false)

	wantFailedTests := []plan.TestCase{
		{
			Scope: "this will fail",
			Name:  "for sure",
			Path:  "failure.spec.js",
		},
	}

	if err != nil {
		t.Errorf("Jest.Run(%q) error = %v", testCases, err)
	}

	if result.Status() != RunStatusFailed {
		t.Errorf("Jest.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusFailed)
	}

	if diff := cmp.Diff(result.FailedTests(), wantFailedTests); diff != "" {
		t.Errorf("Jest.Run(%q) RunResult.FailedTests() diff (-got +want):\n%s", testCases, diff)
	}
}

func TestJestRun_TestSkipped(t *testing.T) {
	changeCwd(t, "./testdata/jest")

	jest := NewJest(RunnerConfig{
		TestCommand: "npx jest --json --outputFile {{resultPath}}",
		ResultPath:  "jest.json",
	})

	t.Cleanup(func() {
		os.Remove(jest.ResultPath)
	})

	testCases := []plan.TestCase{
		{Path: "./testdata/jest/skipped.spec.js"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := jest.Run(result, testCases, false)

	if err != nil {
		t.Errorf("Jest.Run(%q) error = %v", testCases, err)
	}

	if result.Status() != RunStatusPassed {
		t.Errorf("Jest.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusPassed)
	}

	test := result.tests["this will be skipped/for sure/skipped.spec.js"]
	if test.Status != TestStatusSkipped {
		t.Errorf("Jest.Run(%q) test.Status = %v, want %v", testCases, test.Status, TestStatusSkipped)
	}

	todoTest := result.tests["this will be skipped/todo yeah/skipped.spec.js"]
	if todoTest.Status != TestStatusSkipped {
		t.Errorf("Jest.Run(%q) todoTest.Status = %v, want %v", testCases, todoTest.Status, TestStatusSkipped)
	}
}

func TestJestRun_RuntimeError(t *testing.T) {
	changeCwd(t, "./testdata/jest")

	jest := NewJest(RunnerConfig{
		TestCommand: "npx jest --json --outputFile {{resultPath}}",
		ResultPath:  "jest.json",
	})

	t.Cleanup(func() {
		os.Remove(jest.ResultPath)
	})

	testCases := []plan.TestCase{
		{Path: "./testdata/jest/spells/expelliarmus.spec.js"},
		{Path: "./testdata/jest/runtimeError.spec.js"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := jest.Run(result, testCases, false)

	if err != nil {
		t.Errorf("Jest.Run(%q) error = %v", testCases, err)
	}

	if result.Status() != RunStatusError {
		t.Errorf("Jest.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusError)
	}
}

// This test ensures that when a test fails due to timeout and passes after retry, the run result should be passed.
// It covers the scenario where the test location is missing when it timed out, but set properly when passed, resulting in the test
// being treated as different tests.
func TestJestRun_TestTimeoutWithRetry(t *testing.T) {
	changeCwd(t, "./testdata/jest")

	jest := NewJest(RunnerConfig{
		TestCommand:      "npx jest --json --outputFile {{resultPath}} --testTimeout=1",
		ResultPath:       "jest.json",
		RetryTestCommand: "npx jest --testNamePattern '{{testNamePattern}}' --json --outputFile {{resultPath}} ./testdata/jest/slow.spec.js",
	})

	t.Cleanup(func() {
		os.Remove(jest.ResultPath)
	})

	testCases := []plan.TestCase{
		{Path: "./testdata/jest/slow.spec.js"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := jest.Run(result, testCases, false)

	if err != nil {
		t.Errorf("Jest.Run() error = %v", err)
	}

	if result.Status() != RunStatusFailed {
		t.Errorf("Jest.Run() RunResult.Status = %v, want %v", result.Status(), RunStatusFailed)
	}

	// after the test failed due to timeout, retry the test that will eventually passed
	failedTests := result.FailedTests()
	err = jest.Run(result, failedTests, true)

	if err != nil {
		t.Errorf("Jest.Run() error = %v", err)
	}

	if result.Status() != RunStatusPassed {
		t.Errorf("Jest.Run() RunResult.Status = %v, want %v", result.Status(), RunStatusPassed)
	}
}

func TestJestRun_CommandFailed(t *testing.T) {
	jest := NewJest(RunnerConfig{
		TestCommand: "npx jest --invalid-option --outputFile {{resultPath}}",
	})

	t.Cleanup(func() {
		os.Remove(jest.ResultPath)
	})

	testCases := []plan.TestCase{}
	result := NewRunResult([]plan.TestCase{})
	err := jest.Run(result, testCases, false)

	if result.Status() != RunStatusUnknown {
		t.Errorf("Jest.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusUnknown)
	}

	exitError := new(exec.ExitError)
	if !errors.As(err, &exitError) {
		t.Errorf("Jest.Run(%q) error type = %T (%v), want *exec.ExitError", testCases, err, err)
	}
}

func TestJestRun_SignaledError(t *testing.T) {
	jest := NewJest(RunnerConfig{
		TestCommand: "./testdata/segv.sh --outputFile {{resultPath}}",
	})

	testCases := []plan.TestCase{
		{Path: "./doesnt-matter.spec.js"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := jest.Run(result, testCases, false)

	if result.Status() != RunStatusUnknown {
		t.Errorf("Jest.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusUnknown)
	}

	signalError := new(ProcessSignaledError)
	if !errors.As(err, &signalError) {
		t.Errorf("Jest.Run(%q) error type = %T (%v), want *ErrProcessSignaled", testCases, err, err)
	}
	if signalError.Signal != syscall.SIGSEGV {
		t.Errorf("Jest.Run(%q) signal = %d, want %d", testCases, syscall.SIGSEGV, signalError.Signal)
	}
}

func TestJestCommandNameAndArgs_WithInterpolationPlaceholder(t *testing.T) {
	testCases := []string{"spec/user.spec.js", "spec/billing.spec.js"}
	testCommand := "jest {{testExamples}} --outputFile {{resultPath}}"

	jest := NewJest(RunnerConfig{
		TestCommand: testCommand,
		ResultPath:  "jest.json",
	})

	gotName, gotArgs, err := jest.commandNameAndArgs(testCommand, testCases)
	if err != nil {
		t.Errorf("commandNameAndArgs(%q, %q) error = %v", testCases, testCommand, err)
	}

	wantName := "jest"
	wantArgs := []string{"spec/user.spec.js", "spec/billing.spec.js", "--outputFile", "jest.json"}

	if diff := cmp.Diff(gotName, wantName); diff != "" {
		t.Errorf("commandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("commandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
}

func TestJestCommandNameAndArgs_WithoutInterpolationPlaceholder(t *testing.T) {
	testCases := []string{"spec/user.spec.js", "spec/billing.spec.js"}
	testCommand := "jest --json --outputFile {{resultPath}}"

	jest := NewJest(RunnerConfig{
		TestCommand: testCommand,
		ResultPath:  "jest.json",
	})

	gotName, gotArgs, err := jest.commandNameAndArgs(testCommand, testCases)
	if err != nil {
		t.Errorf("commandNameAndArgs(%q, %q) error = %v", testCases, testCommand, err)
	}

	wantName := "jest"
	wantArgs := []string{"--json", "--outputFile", "jest.json", "spec/user.spec.js", "spec/billing.spec.js"}

	if diff := cmp.Diff(gotName, wantName); diff != "" {
		t.Errorf("commandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("commandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
}

func TestJestCommandNameAndArgs_InvalidTestCommand(t *testing.T) {
	testCases := []string{"spec/user.spec.js", "spec/billing.spec.js"}
	testCommand := "jest --options '{{testExamples}}"

	jest := NewJest(RunnerConfig{
		TestCommand: testCommand,
	})

	gotName, gotArgs, err := jest.commandNameAndArgs(testCommand, testCases)

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

func TestJestRetryCommandNameAndArgs_HappyPath(t *testing.T) {
	testCases := []string{"this will fail", "this other one will fail"}
	retryTestCommand := "jest --testNamePattern '{{testNamePattern}}' --json --testLocationInResults --outputFile {{resultPath}}"

	jest := NewJest(RunnerConfig{
		RetryTestCommand: retryTestCommand,
		ResultPath:       "jest.json",
	})

	gotName, gotArgs, err := jest.retryCommandNameAndArgs(retryTestCommand, testCases)
	if err != nil {
		t.Errorf("retryCommandNameAndArgs(%q, %q) error = %v", testCases, retryTestCommand, err)
	}

	wantName := "jest"
	wantArgs := []string{"--testNamePattern", "(this will fail|this other one will fail)", "--json", "--testLocationInResults", "--outputFile", "jest.json"}

	if diff := cmp.Diff(gotName, wantName); diff != "" {
		t.Errorf("retryCommandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, retryTestCommand, diff)
	}
	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("retryCommandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, retryTestCommand, diff)
	}
}

func TestJestRetryCommandNameAndArgs_WithSpecialCharacters(t *testing.T) {
	testCases := []string{"test with special characters .+*?()|[]{}^$", "another test"}
	retryTestCommand := "jest --testNamePattern '{{testNamePattern}}' --json --testLocationInResults --outputFile {{resultPath}}"

	jest := NewJest(RunnerConfig{
		RetryTestCommand: retryTestCommand,
		ResultPath:       "jest.json",
	})

	gotName, gotArgs, err := jest.retryCommandNameAndArgs(retryTestCommand, testCases)
	if err != nil {
		t.Errorf("retryCommandNameAndArgs(%q, %q) error = %v", testCases, retryTestCommand, err)
	}

	wantName := "jest"
	wantArgs := []string{"--testNamePattern", `(test with special characters \.\+\*\?\(\)\|\[\]\{\}\^\$|another test)`, "--json", "--testLocationInResults", "--outputFile", "jest.json"}

	if diff := cmp.Diff(gotName, wantName); diff != "" {
		t.Errorf("retryCommandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, retryTestCommand, diff)
	}
	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("retryCommandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, retryTestCommand, diff)
	}
}

func TestJestRetryCommandNameAndArgs_WithoutInterpolationPlaceholder(t *testing.T) {
	testCases := []string{"this will fail", "this other one will fail"}
	retryTestCommand := "jest --json --outputFile {{resultPath}}"

	jest := NewJest(RunnerConfig{
		RetryTestCommand: retryTestCommand,
		ResultPath:       "jest.json",
	})

	gotName, gotArgs, err := jest.retryCommandNameAndArgs(retryTestCommand, testCases)
	fmt.Println(err)

	wantName := ""
	wantArgs := []string{}

	if diff := cmp.Diff(gotName, wantName); diff != "" {
		t.Errorf("retryCommandNameAndArgs() diff (-got +want):\n%s", diff)
	}
	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("retryCommandNameAndArgs() diff (-got +want):\n%s", diff)
	}

	desiredString := "couldn't find '{{testNamePattern}}' sentinel in retry command"
	if err.Error() != desiredString {
		t.Errorf("retryCommandNameAndArgs() error = %v, want %v", err, desiredString)
	}
}

func TestJestGetFiles(t *testing.T) {
	changeCwd(t, "./testdata/jest")
	jest := NewJest(RunnerConfig{})

	got, err := jest.GetFiles()
	if err != nil {
		t.Errorf("Jest.GetFiles() error = %v", err)
	}

	want := []string{
		"failure.spec.js",
		"runtimeError.spec.js",
		"skipped.spec.js",
		"slow.spec.js",
		"spells/expelliarmus.spec.js",
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Jest.GetFiles() diff (-got +want):\n%s", diff)
	}
}
