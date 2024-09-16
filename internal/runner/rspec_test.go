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
		TestCommand: "rspec",
	})
	files := []string{"./fixtures/rspec/spec/spells/expelliarmus_spec.rb"}
	got, err := rspec.Run(files, false)

	want := RunResult{
		Status: RunStatusPassed,
	}

	if err != nil {
		t.Errorf("Rspec.Run(%q) error = %v", files, err)
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Rspec.Run(%q) diff (-got +want):\n%s", files, diff)
	}
}

func TestRspecRun_RetryCommand(t *testing.T) {
	rspec := Rspec{
		RunnerConfig{
			TestCommand:      "rspec --invalid-option",
			RetryTestCommand: "rspec",
		},
	}
	files := []string{}
	got, err := rspec.Run(files, true)

	want := RunResult{
		Status: RunStatusPassed,
	}

	if err != nil {
		t.Errorf("Rspec.Run(%q) error = %v", files, err)
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Rspec.Run(%q) diff (-got +want):\n%s", files, diff)
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

	files := []string{"./fixtures/rspec/spec/failure_spec.rb"}
	got, err := rspec.Run(files, false)

	want := RunResult{
		Status:      RunStatusFailed,
		FailedTests: []string{"./fixtures/rspec/spec/failure_spec.rb[1:1]"},
	}

	if err != nil {
		t.Errorf("Rspec.Run(%q) error = %v", files, err)
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Rspec.Run(%q) diff (-got +want):\n%s", files, diff)
	}
}

func TestRspecRun_TestFailedWithoutResultFile(t *testing.T) {
	rspec := NewRspec(RunnerConfig{
		TestCommand: "rspec",
	})

	t.Cleanup(func() {
		os.Remove(rspec.ResultPath)
	})

	files := []string{"./fixtures/rspec/spec/failure_spec.rb"}
	got, err := rspec.Run(files, false)

	want := RunResult{
		Status: RunStatusError,
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Rspec.Run(%q) diff (-got +want):\n%s", files, diff)
	}

	exitError := new(exec.ExitError)
	if !errors.As(err, &exitError) {
		t.Errorf("Rspec.Run(%q) error type = %T (%v), want *exec.ExitError", files, err, err)
	}
}

func TestRspecRun_CommandFailed(t *testing.T) {
	rspec := Rspec{
		RunnerConfig{
			TestCommand: "rspec --invalid-option",
		},
	}
	files := []string{}
	got, err := rspec.Run(files, false)

	want := RunResult{
		Status: RunStatusError,
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Rspec.Run(%q) diff (-got +want):\n%s", files, diff)
	}

	exitError := new(exec.ExitError)
	if !errors.As(err, &exitError) {
		t.Errorf("Rspec.Run(%q) error type = %T (%v), want *exec.ExitError", files, err, err)
	}
}

func TestRspecRun_SignaledError(t *testing.T) {
	rspec := NewRspec(RunnerConfig{
		TestCommand: "../../test/support/segv.sh",
	})
	files := []string{"./fixtures/rspec/spec/failure_spec.rb"}

	got, err := rspec.Run(files, false)

	want := RunResult{
		Status: RunStatusError,
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Rspec.Run(%q) diff (-got +want):\n%s", files, diff)
	}

	signalError := new(ProcessSignaledError)
	if !errors.As(err, &signalError) {
		t.Errorf("Rspec.Run(%q) error type = %T (%v), want *ErrProcessSignaled", files, err, err)
	}
	if signalError.Signal != syscall.SIGSEGV {
		t.Errorf("Rspec.Run(%q) signal = %d, want %d", files, syscall.SIGSEGV, signalError.Signal)
	}
}

func TestRspecCommandNameAndArgs_WithPlaceholder(t *testing.T) {
	testCases := []string{"spec/models/user_spec.rb", "spec/models/billing_spec.rb"}
	testCommand := "bin/rspec --options {{testExamples}} --out {{resultPath}}"

	rspec := Rspec{
		RunnerConfig{
			TestCommand: testCommand,
			ResultPath:  "tmp/rspec.json",
		},
	}

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

	rspec := Rspec{
		RunnerConfig{
			TestCommand: testCommand,
		},
	}

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

	rspec := Rspec{
		RunnerConfig{
			TestCommand: testCommand,
		},
	}

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
	files := []string{"./fixtures/rspec/spec/spells/expelliarmus_spec.rb"}
	got, err := rspec.GetExamples(files)

	want := []plan.TestCase{
		{
			Identifier: "./fixtures/rspec/spec/spells/expelliarmus_spec.rb[1:1]",
			Name:       "disarms the opponent",
			Path:       "./fixtures/rspec/spec/spells/expelliarmus_spec.rb[1:1]",
			Scope:      "Expelliarmus disarms the opponent",
		},
		{
			Identifier: "./fixtures/rspec/spec/spells/expelliarmus_spec.rb[1:2]",
			Name:       "knocks the wand out of the opponents hand",
			Path:       "./fixtures/rspec/spec/spells/expelliarmus_spec.rb[1:2]",
			Scope:      "Expelliarmus knocks the wand out of the opponents hand",
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
	files := []string{"./fixtures/rspec/spec/spells/expelliarmus_spec.rb"}
	want := []plan.TestCase{
		{
			Identifier: "./fixtures/rspec/spec/spells/expelliarmus_spec.rb[1:1]",
			Name:       "disarms the opponent",
			Path:       "./fixtures/rspec/spec/spells/expelliarmus_spec.rb[1:1]",
			Scope:      "Expelliarmus disarms the opponent",
		},
		{
			Identifier: "./fixtures/rspec/spec/spells/expelliarmus_spec.rb[1:2]",
			Name:       "knocks the wand out of the opponents hand",
			Path:       "./fixtures/rspec/spec/spells/expelliarmus_spec.rb[1:2]",
			Scope:      "Expelliarmus knocks the wand out of the opponents hand",
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
	files := []string{"./fixtures/rspec/spec/specs_with_shared_examples_spec.rb"}
	got, err := rspec.GetExamples(files)

	want := []plan.TestCase{
		{
			Identifier: "./fixtures/rspec/spec/specs_with_shared_examples_spec.rb[1:1:1]",
			Name:       "behaves like a shared example",
			Path:       "./fixtures/rspec/spec/specs_with_shared_examples_spec.rb[1:1:1]",
			Scope:      "Specs with shared examples behaves like shared behaves like a shared example",
		},
	}

	if err != nil {
		t.Errorf("Rspec.GetExamples(%q) error = %v", files, err)
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Rspec.GetExamples(%q) diff (-got +want):\n%s", files, diff)
	}
}
