package runner

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"

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
		TestCommand: "jest --json --outputFile {{resultPath}}",
		ResultPath:  "jest.json",
	})

	t.Cleanup(func() {
		os.Remove(jest.ResultPath)
	})

	files := []string{"./testdata/jest/spells/expelliarmus.spec.js"}
	got, err := jest.Run(files, false)

	want := RunResult{
		Status: RunStatusPassed,
	}

	if err != nil {
		t.Errorf("Jest.Run(%q) error = %v", files, err)
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Jest.Run(%q) diff (-got +want):\n%s", files, diff)
	}
}

func TestJestRun_Retry(t *testing.T) {
	changeCwd(t, "./testdata/jest")

	jest := Jest{
		RunnerConfig{
			TestCommand:      "jest --invalid-option --json --outputFile {{resultPath}}",
			RetryTestCommand: "jest --testNamePattern '{{testNamePattern}}' --json --outputFile {{resultPath}}",
		},
	}

	t.Cleanup(func() {
		os.Remove(jest.ResultPath)
	})

	files := []string{"testdata/jest/spells/expelliarmus.spec.js"}
	got, err := jest.Run(files, true)

	want := RunResult{
		Status: RunStatusPassed,
	}

	if err != nil {
		t.Errorf("Jest.Run(%q) error = %v", files, err)
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Jest.Run(%q) diff (-got +want):\n%s", files, diff)
	}
}

func TestJestRun_TestFailed(t *testing.T) {
	changeCwd(t, "./testdata/jest")

	jest := NewJest(RunnerConfig{
		TestCommand: "jest --json --outputFile {{resultPath}}",
		ResultPath:  "jest.json",
	})

	t.Cleanup(func() {
		os.Remove(jest.ResultPath)
	})

	files := []string{"./testdata/jest/failure.spec.js"}
	got, err := jest.Run(files, false)

	want := RunResult{
		Status:      RunStatusFailed,
		FailedTests: []string{"this will fail for sure"},
	}

	if err != nil {
		t.Errorf("Jest.Run(%q) error = %v", files, err)
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Jest.Run(%q) diff (-got +want):\n%s", files, diff)
	}
}

func TestJestRun_CommandFailed(t *testing.T) {
	jest := Jest{
		RunnerConfig{
			TestCommand: "jest --invalid-option --outputFile {{resultPath}}",
		},
	}

	t.Cleanup(func() {
		os.Remove(jest.ResultPath)
	})

	files := []string{}
	got, err := jest.Run(files, false)

	want := RunResult{
		Status: RunStatusError,
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Jest.Run(%q) diff (-got +want):\n%s", files, diff)
	}

	exitError := new(exec.ExitError)
	if !errors.As(err, &exitError) {
		t.Errorf("Jest.Run(%q) error type = %T (%v), want *exec.ExitError", files, err, err)
	}
}

func TestJestRun_SignaledError(t *testing.T) {
	jest := NewJest(RunnerConfig{
		TestCommand: "./testdata/segv.sh --outputFile {{resultPath}}",
	})
	files := []string{"./doesnt-matter.spec.js"}

	got, err := jest.Run(files, false)

	want := RunResult{
		Status: RunStatusError,
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Jest.Run(%q) diff (-got +want):\n%s", files, diff)
	}

	signalError := new(ProcessSignaledError)
	if !errors.As(err, &signalError) {
		t.Errorf("Jest.Run(%q) error type = %T (%v), want *ErrProcessSignaled", files, err, err)
	}
	if signalError.Signal != syscall.SIGSEGV {
		t.Errorf("Jest.Run(%q) signal = %d, want %d", files, syscall.SIGSEGV, signalError.Signal)
	}
}

func TestJestCommandNameAndArgs_WithInterpolationPlaceholder(t *testing.T) {
	testCases := []string{"spec/user.spec.js", "spec/billing.spec.js"}
	testCommand := "jest {{testExamples}} --outputFile {{resultPath}}"

	jest := Jest{
		RunnerConfig{
			TestCommand: testCommand,
			ResultPath:  "jest.json",
		},
	}

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

	jest := Jest{
		RunnerConfig{
			TestCommand: testCommand,
			ResultPath:  "jest.json",
		},
	}

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

	jest := Jest{
		RunnerConfig{
			TestCommand: testCommand,
		},
	}

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

	jest := Jest{
		RunnerConfig{
			RetryTestCommand: retryTestCommand,
			ResultPath:       "jest.json",
		},
	}

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
	testCases := []string{"this will fail", "test with special characters .+*?()|[]{}^$"}
	retryTestCommand := "jest --testNamePattern '{{testNamePattern}}' --json --testLocationInResults --outputFile {{resultPath}}"

	jest := Jest{
		RunnerConfig{
			RetryTestCommand: retryTestCommand,
			ResultPath:       "jest.json",
		},
	}

	gotName, gotArgs, err := jest.retryCommandNameAndArgs(retryTestCommand, testCases)
	if err != nil {
		t.Errorf("retryCommandNameAndArgs(%q, %q) error = %v", testCases, retryTestCommand, err)
	}

	wantName := "jest"
	wantArgs := []string{"--testNamePattern", `(this will fail|test with special characters \.\+\*\?\(\)\|\[\]\{\}\^\$)`, "--json", "--testLocationInResults", "--outputFile", "jest.json"}

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

	jest := Jest{
		RunnerConfig{
			RetryTestCommand: retryTestCommand,
			ResultPath:       "jest.json",
		},
	}

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
		"spells/expelliarmus.spec.js",
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Jest.GetFiles() diff (-got +want):\n%s", diff)
	}
}
