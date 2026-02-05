package runner

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"

	"github.com/buildkite/test-engine-client/internal/plan"
	"github.com/google/go-cmp/cmp"
	"github.com/kballard/go-shellquote"
)

func TestNewRspec(t *testing.T) {
	cases := []struct {
		input RunnerConfig
		want  RunnerConfig
	}{
		//default
		{
			input: RunnerConfig{},
			want: RunnerConfig{
				TestCommand:            "bundle exec rspec --format progress --format json --out {{resultPath}} {{testExamples}}",
				TestFilePattern:        "spec/**/*_spec.rb",
				TestFileExcludePattern: "",
				RetryTestCommand:       "bundle exec rspec --format progress --format json --out {{resultPath}} {{testExamples}}",
			},
		},
		// custom
		{
			input: RunnerConfig{
				TestCommand:            "bin/rspec --format documentation {{testExamples}} --format json --out {{resultPath}}",
				TestFilePattern:        "spec/models/**/*_spec.rb",
				TestFileExcludePattern: "spec/features/**/*_spec.rb",
				RetryTestCommand:       "bin/rspec --fail-fast {{testExamples}}",
			},
			want: RunnerConfig{
				TestCommand:            "bin/rspec --format documentation {{testExamples}} --format json --out {{resultPath}}",
				TestFilePattern:        "spec/models/**/*_spec.rb",
				TestFileExcludePattern: "spec/features/**/*_spec.rb",
				RetryTestCommand:       "bin/rspec --fail-fast {{testExamples}}",
			},
		},
		// RetryTestCommand fallback to TestCommand
		{
			input: RunnerConfig{
				TestCommand: "bundle exec --format json --out out.json {{testExamples}}",
			},
			want: RunnerConfig{
				TestCommand:            "bundle exec --format json --out out.json {{testExamples}}",
				TestFilePattern:        "spec/**/*_spec.rb",
				TestFileExcludePattern: "",
				RetryTestCommand:       "bundle exec --format json --out out.json {{testExamples}}",
			},
		},
	}

	for _, c := range cases {
		got := NewRspec(c.input)
		if diff := cmp.Diff(got.RunnerConfig, c.want); diff != "" {
			t.Errorf("NewRspec(%v) diff (-got +want):\n%s", c.input, diff)
		}
	}
}

func TestRspecRun(t *testing.T) {
	rspec := NewRspec(RunnerConfig{
		TestCommand: "rspec --format json --out {{resultPath}}",
		ResultPath:  "tmp/rspec.json",
	})

	t.Cleanup(func() {
		os.Remove(rspec.ResultPath)
	})

	testCases := []plan.TestCase{
		{Path: "./testdata/rspec/spec/spells/expelliarmus_spec.rb"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := rspec.Run(result, testCases, false)

	if err != nil {
		t.Errorf("Rspec.Run(%q) error = %v", testCases, err)
	}

	if len(result.tests) != 2 {
		t.Errorf("Rspec.Run(%q) len(RunResult.tests) = %d, want 2", testCases, len(result.tests))
	}

	if result.Status() != RunStatusPassed {
		t.Errorf("Rspec.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusPassed)
	}
}

func TestRspecRun_RetryCommand(t *testing.T) {
	rspec := NewRspec(RunnerConfig{
		TestCommand:      "rspec --invalid-option",
		RetryTestCommand: "rspec --format json --out {{resultPath}}",
		ResultPath:       "tmp/rspec.json",
	})

	testCases := []plan.TestCase{
		{Path: "./testdata/rspec/spec/spells/expelliarmus_spec.rb"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := rspec.Run(result, testCases, true)

	if err != nil {
		t.Errorf("Rspec.Run(%q) error = %v", testCases, err)
	}

	if result.Status() != RunStatusPassed {
		t.Errorf("Rspec.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusPassed)
	}
}

func TestRspecRun_TestFailedWithResultFile(t *testing.T) {
	rspec := NewRspec(RunnerConfig{
		TestCommand: "rspec --format json --out {{resultPath}}",
		ResultPath:  "tmp/rspec.json",
	})

	t.Cleanup(func() {
		os.Remove(rspec.ResultPath)
	})

	testCases := []plan.TestCase{
		{Path: "./testdata/rspec/spec/failure_spec.rb"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := rspec.Run(result, testCases, false)

	wantFailedTests := []plan.TestCase{
		{
			Name:       "fails",
			Scope:      "Failure",
			Identifier: "./testdata/rspec/spec/failure_spec.rb[1:1]",
			Path:       "./testdata/rspec/spec/failure_spec.rb[1:1]",
		},
	}

	if err != nil {
		t.Errorf("Rspec.Run(%q) error = %v", testCases, err)
	}

	if result.Status() != RunStatusFailed {
		t.Errorf("Rspec.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusFailed)
	}

	if diff := cmp.Diff(result.FailedTests(), wantFailedTests); diff != "" {
		t.Errorf("Rspec.Run(%q) RunResult.FailedTests() diff (-got +want):\n%s", testCases, diff)
	}
}

func TestRspecRun_TestFailedWithoutResultFile(t *testing.T) {
	rspec := NewRspec(RunnerConfig{
		TestCommand: "rspec",
	})

	t.Cleanup(func() {
		os.Remove(rspec.ResultPath)
	})

	testCases := []plan.TestCase{
		{Path: "./testdata/rspec/spec/failure_spec.rb"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := rspec.Run(result, testCases, false)

	if result.Status() != RunStatusUnknown {
		t.Errorf("Rspec.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusUnknown)
	}

	exitError := new(exec.ExitError)
	if !errors.As(err, &exitError) {
		t.Errorf("Rspec.Run(%q) error type = %T (%v), want *exec.ExitError", testCases, err, err)
	}
}

func TestRspecRun_TestSkipped(t *testing.T) {
	rspec := NewRspec(RunnerConfig{
		TestCommand: "rspec --format json --out {{resultPath}} --format progress",
		ResultPath:  "tmp/rspec.json",
	})

	t.Cleanup(func() {
		os.Remove(rspec.ResultPath)
	})

	testCases := []plan.TestCase{
		{Path: "./testdata/rspec/spec/skipped_spec.rb"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := rspec.Run(result, testCases, false)

	if err != nil {
		t.Errorf("Rspec.Run(%q) error = %v", testCases, err)
	}

	if result.Status() != RunStatusPassed {
		t.Errorf("Rspec.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusPassed)
	}

	test := result.tests["skipped/is skipped/./testdata/rspec/spec/skipped_spec.rb[1:1]"]
	if test.Status != TestStatusSkipped {
		t.Errorf("Rspec.Run(%q) test.Status = %v, want %v", testCases, test.Status, TestStatusSkipped)
	}
}

func TestRspecRun_TestExit(t *testing.T) {
	rspec := NewRspec(RunnerConfig{
		TestCommand: "rspec --format json --out {{resultPath}} --format progress",
		ResultPath:  "tmp/rspec.json",
	})

	t.Cleanup(func() {
		os.Remove(rspec.ResultPath)
	})

	testCases := []plan.TestCase{
		{Path: "./testdata/rspec/spec/exit_spec.rb"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := rspec.Run(result, testCases, false)

	if err != nil {
		t.Errorf("Rspec.Run(%q) error = %v", testCases, err)
	}

	if result.Status() != RunStatusError {
		t.Errorf("Rspec.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusError)
	}

	wantError := "RSpec exited with code 7, but no failed tests were reported. This may be caused by an explicit call to `exit` in the code or specs"
	if diff := cmp.Diff(result.error.Error(), wantError); diff != "" {
		t.Errorf("Rspec.Run(%q) RunResult.error diff (-got +want):\n%s", testCases, diff)
	}
}

func TestRspecRun_ErrorOutsideOfExamples(t *testing.T) {
	rspec := NewRspec(RunnerConfig{
		TestCommand: "rspec --format json --out {{resultPath}} --format documentation",
		ResultPath:  "tmp/rspec.json",
	})

	t.Cleanup(func() {
		os.Remove(rspec.ResultPath)
	})

	testCases := []plan.TestCase{
		{Path: "./testdata/rspec/spec/bad_syntax_spec.rb"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := rspec.Run(result, testCases, false)

	if err != nil {
		t.Errorf("Rspec.Run(%q) error = %v", testCases, err)
	}

	if result.Status() != RunStatusError {
		t.Errorf("Rspec.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusError)
	}
}

func TestRspecRun_CommandFailed(t *testing.T) {
	rspec := NewRspec(RunnerConfig{
		TestCommand: "rspec --invalid-option",
	})
	testCases := []plan.TestCase{}
	result := NewRunResult([]plan.TestCase{})
	err := rspec.Run(result, testCases, false)

	if result.Status() != RunStatusUnknown {
		t.Errorf("Rspec.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusUnknown)
	}

	exitError := new(exec.ExitError)
	if !errors.As(err, &exitError) {
		t.Errorf("Rspec.Run(%q) error type = %T (%v), want *exec.ExitError", testCases, err, err)
	}
}

func TestRspecRun_SignaledError(t *testing.T) {
	rspec := NewRspec(RunnerConfig{
		TestCommand: "./testdata/segv.sh",
	})

	testCases := []plan.TestCase{
		{Path: "./testdata/rspec/spec/failure_spec.rb"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := rspec.Run(result, testCases, false)

	if result.Status() != RunStatusUnknown {
		t.Errorf("Rspec.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusUnknown)
	}

	signalError := new(ProcessSignaledError)
	if !errors.As(err, &signalError) {
		t.Errorf("Rspec.Run(%q) error type = %T (%v), want *ErrProcessSignaled", testCases, err, err)
	}
	if signalError.Signal != syscall.SIGSEGV {
		t.Errorf("Rspec.Run(%q) signal = %d, want %d", testCases, syscall.SIGSEGV, signalError.Signal)
	}
}

func TestRspecCommandNameAndArgs_WithPlaceholder(t *testing.T) {
	testCases := []string{"spec/models/user_spec.rb", "spec/models/billing_spec.rb"}
	testCommand := "bin/rspec --options {{testExamples}} --out {{resultPath}}"

	rspec := NewRspec(RunnerConfig{
		TestCommand: testCommand,
		ResultPath:  "tmp/rspec.json",
	})

	gotName, gotArgs, err := rspec.commandNameAndArgs(testCommand, testCases)
	if err != nil {
		t.Errorf("commandNameAndArgs(%q, %q) error = %v", testCases, testCommand, err)
	}

	wantName := "bin/rspec"
	wantArgs := []string{"--options", "spec/models/user_spec.rb", "spec/models/billing_spec.rb", "--out", rspec.ResultPath}

	if diff := cmp.Diff(gotName, wantName); diff != "" {
		t.Errorf("commandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("commandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
}

func TestRspecCommandNameAndArgs_WithoutTestExamplesPlaceholder(t *testing.T) {
	testCases := []string{"spec/models/user_spec.rb", "spec/models/billing_spec.rb"}
	testCommand := "bin/rspec --options --format"

	rspec := NewRspec(RunnerConfig{
		TestCommand: testCommand,
	})

	gotName, gotArgs, err := rspec.commandNameAndArgs(testCommand, testCases)
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

func TestRspecCommandNameAndArgs_InvalidTestCommand(t *testing.T) {
	testCases := []string{"spec/models/user_spec.rb", "spec/models/billing_spec.rb"}
	testCommand := "bin/rspec --options ' {{testExamples}}"

	rspec := NewRspec(RunnerConfig{
		TestCommand: testCommand,
	})

	gotName, gotArgs, err := rspec.commandNameAndArgs(testCommand, testCases)

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

func TestRspecGetExamples(t *testing.T) {
	rspec := NewRspec(RunnerConfig{
		TestCommand: "rspec",
	})
	files := []string{"./testdata/rspec/spec/spells/expelliarmus_spec.rb"}
	got, err := rspec.GetExamples(files)

	want := []plan.TestCase{
		{
			Identifier: "./testdata/rspec/spec/spells/expelliarmus_spec.rb[1:1]",
			Name:       "disarms the opponent",
			Path:       "./testdata/rspec/spec/spells/expelliarmus_spec.rb[1:1]",
			Scope:      "Expelliarmus",
		},
		{
			Identifier: "./testdata/rspec/spec/spells/expelliarmus_spec.rb[1:2]",
			Name:       "knocks the wand out of the opponents hand",
			Path:       "./testdata/rspec/spec/spells/expelliarmus_spec.rb[1:2]",
			Scope:      "Expelliarmus",
		},
	}

	if err != nil {
		t.Errorf("Rspec.GetExamples(%q) error = %v", files, err)
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Rspec.GetExamples(%q) diff (-got +want):\n%s", files, diff)
	}
}

func TestRspecGetExamples_WithOtherFormatters(t *testing.T) {
	files := []string{"./testdata/rspec/spec/spells/expelliarmus_spec.rb"}
	want := []plan.TestCase{
		{
			Identifier: "./testdata/rspec/spec/spells/expelliarmus_spec.rb[1:1]",
			Name:       "disarms the opponent",
			Path:       "./testdata/rspec/spec/spells/expelliarmus_spec.rb[1:1]",
			Scope:      "Expelliarmus",
		},
		{
			Identifier: "./testdata/rspec/spec/spells/expelliarmus_spec.rb[1:2]",
			Name:       "knocks the wand out of the opponents hand",
			Path:       "./testdata/rspec/spec/spells/expelliarmus_spec.rb[1:2]",
			Scope:      "Expelliarmus",
		},
	}

	// Create a temporary file to store the JSON output of the rspec dry run.
	// So we don't end up with a lot of files after running this test.
	// We'll clean up the file after the test.
	f, err := os.CreateTemp("", "rspec.json")
	if err != nil {
		t.Errorf("os.CreateTemp() error = %v", err)
	}
	defer f.Close()
	defer os.Remove(f.Name())
	withOtherJson := "rspec --format json --out " + f.Name()

	commands := []string{"rspec --format documentation", "rspec --format html", withOtherJson}
	for _, command := range commands {
		rspec := NewRspec(RunnerConfig{
			TestCommand: command,
		})
		got, err := rspec.GetExamples(files)

		t.Run(command, func(t *testing.T) {

			if err != nil {
				t.Errorf("Rspec.GetExamples(%q) error = %v", files, err)
			}

			if diff := cmp.Diff(got, want); diff != "" {
				t.Errorf("Rspec.GetExamples(%q) diff (-got +want):\n%s", files, diff)
			}
		})
	}
}

func TestRspecGetExamples_WithSharedExamples(t *testing.T) {
	rspec := NewRspec(RunnerConfig{
		TestCommand: "rspec",
	})
	files := []string{"./testdata/rspec/spec/specs_with_shared_examples_spec.rb"}
	got, err := rspec.GetExamples(files)

	want := []plan.TestCase{
		{
			Identifier: "./testdata/rspec/spec/specs_with_shared_examples_spec.rb[1:1:1]",
			Name:       "behaves like a shared example",
			Path:       "./testdata/rspec/spec/specs_with_shared_examples_spec.rb[1:1:1]",
			Scope:      "Specs with shared examples behaves like shared",
		},
	}

	if err != nil {
		t.Errorf("Rspec.GetExamples(%q) error = %v", files, err)
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Rspec.GetExamples(%q) diff (-got +want):\n%s", files, diff)
	}
}
