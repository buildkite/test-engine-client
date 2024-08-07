package runner

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"

	"github.com/buildkite/test-splitter/internal/plan"
	"github.com/google/go-cmp/cmp"
)

func TestNewRspec(t *testing.T) {
	cases := []struct {
		input Rspec
		want  Rspec
	}{
		//default
		{
			input: Rspec{},
			want: Rspec{
				TestCommand:            "bundle exec rspec --format progress {{testExamples}}",
				TestFilePattern:        "spec/**/*_spec.rb",
				TestFileExcludePattern: "",
				RetryTestCommand:       "bundle exec rspec --format progress {{testExamples}}",
			},
		},
		// custom
		{
			input: Rspec{
				TestCommand:            "bin/rspec --format documentation {{testExamples}}",
				TestFilePattern:        "spec/models/**/*_spec.rb",
				TestFileExcludePattern: "spec/features/**/*_spec.rb",
				RetryTestCommand:       "bin/rspec --fail-fast {{testExamples}}",
			},
			want: Rspec{
				TestCommand:            "bin/rspec --format documentation {{testExamples}}",
				TestFilePattern:        "spec/models/**/*_spec.rb",
				TestFileExcludePattern: "spec/features/**/*_spec.rb",
				RetryTestCommand:       "bin/rspec --fail-fast {{testExamples}}",
			},
		},
		// RetryTestCommand fallback to TestCommand
		{
			input: Rspec{
				TestCommand: "bundle exec --format json --out out.json {{testExamples}}",
			},
			want: Rspec{
				TestCommand:            "bundle exec --format json --out out.json {{testExamples}}",
				TestFilePattern:        "spec/**/*_spec.rb",
				TestFileExcludePattern: "",
				RetryTestCommand:       "bundle exec --format json --out out.json {{testExamples}}",
			},
		},
	}

	for _, c := range cases {
		got := NewRspec(c.input)
		if diff := cmp.Diff(got, &c.want); diff != "" {
			t.Errorf("NewRspec(%v) diff (-got +want):\n%s", c.input, diff)
		}
	}
}

func TestRspecRun(t *testing.T) {
	rspec := NewRspec(Rspec{
		TestCommand: "rspec",
	})
	files := []string{"./fixtures/spec/spells/expelliarmus_spec.rb"}
	got, err := rspec.Run(files, false)

	want := TestResult{
		Status: TestStatusPassed,
	}

	if err != nil {
		t.Errorf("Rspec.Run(%q) error = %v", files, err)
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Rspec.Run(%q) diff (-got +want):\n%s", files, diff)
	}
}

func TestRspecRun_Retry(t *testing.T) {
	rspec := Rspec{
		TestCommand:      "rspec --invalid-option",
		RetryTestCommand: "rspec",
	}
	files := []string{}
	got, err := rspec.Run(files, true)

	want := TestResult{
		Status: TestStatusPassed,
	}

	if err != nil {
		t.Errorf("Rspec.Run(%q) error = %v", files, err)
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Rspec.Run(%q) diff (-got +want):\n%s", files, diff)
	}
}

func TestRspecRun_TestFailed(t *testing.T) {
	rspec := NewRspec(Rspec{
		TestCommand: "rspec",
	})
	files := []string{"./fixtures/spec/failure_spec.rb"}
	got, err := rspec.Run(files, false)

	want := TestResult{
		Status:      TestStatusFailed,
		FailedTests: []string{"./fixtures/spec/failure_spec.rb[1:1]"},
	}

	if err != nil {
		t.Errorf("Rspec.Run(%q) error = %v", files, err)
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Rspec.Run(%q) diff (-got +want):\n%s", files, diff)
	}
}

func TestRspecRun_CommandFailed(t *testing.T) {
	rspec := Rspec{
		TestCommand: "rspec --invalid-option",
	}
	files := []string{}
	got, err := rspec.Run(files, false)

	want := TestResult{
		Status: TestStatusError,
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Rspec.Run(%q) diff (-got +want):\n%s", files, diff)
	}

	exitError := new(exec.ExitError)
	if !errors.As(err, &exitError) {
		t.Errorf("Expected exec.ExitError, but got %v", err)
	}
}

func TestRspecRun_SignaledError(t *testing.T) {
	rspec := NewRspec(Rspec{
		TestCommand: "../../test/support/segv.sh",
	})
	files := []string{"./fixtures/spec/failure_spec.rb"}

	got, err := rspec.Run(files, false)

	want := TestResult{
		Status: TestStatusError,
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Rspec.Run(%q) diff (-got +want):\n%s", files, diff)
	}

	signalError := new(ProcessSignaledError)
	if !errors.As(err, &signalError) {
		t.Errorf("Expected ErrProcessSignaled, but got %v", err)
	}
	if signalError.Signal != syscall.SIGSEGV {
		t.Errorf("Expected signal %d, but got %d", syscall.SIGSEGV, signalError.Signal)
	}
}

func TestRspecGetExamples(t *testing.T) {
	rspec := NewRspec(Rspec{
		TestCommand: "rspec",
	})
	files := []string{"./fixtures/spec/spells/expelliarmus_spec.rb"}
	got, err := rspec.GetExamples(files)

	want := []plan.TestCase{
		{
			Identifier: "./fixtures/spec/spells/expelliarmus_spec.rb[1:1]",
			Name:       "disarms the opponent",
			Path:       "./fixtures/spec/spells/expelliarmus_spec.rb:2",
			Scope:      "Expelliarmus disarms the opponent",
		},
		{
			Identifier: "./fixtures/spec/spells/expelliarmus_spec.rb[1:2]",
			Name:       "knocks the wand out of the opponents hand",
			Path:       "./fixtures/spec/spells/expelliarmus_spec.rb:6",
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
	files := []string{"./fixtures/spec/spells/expelliarmus_spec.rb"}
	want := []plan.TestCase{
		{
			Identifier: "./fixtures/spec/spells/expelliarmus_spec.rb[1:1]",
			Name:       "disarms the opponent",
			Path:       "./fixtures/spec/spells/expelliarmus_spec.rb:2",
			Scope:      "Expelliarmus disarms the opponent",
		},
		{
			Identifier: "./fixtures/spec/spells/expelliarmus_spec.rb[1:2]",
			Name:       "knocks the wand out of the opponents hand",
			Path:       "./fixtures/spec/spells/expelliarmus_spec.rb:6",
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
		rspec := NewRspec(Rspec{
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
