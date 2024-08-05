package runner

import (
	"os"
	"testing"

	"github.com/buildkite/test-splitter/internal/plan"
	"github.com/google/go-cmp/cmp"
)

func TestNewRspec_DefaultCommand(t *testing.T) {
	defaultCommand := "bundle exec rspec {{testExamples}}"
	rspec := NewRspec(Rspec{})

	if rspec.TestCommand != defaultCommand {
		t.Errorf("rspec.TestCommand = %q, want %q", rspec.TestCommand, defaultCommand)
	}
}

func TestNewRspec_CustomCommand(t *testing.T) {
	customCommand := "bin/rspec --options {{testExamples}} --format"
	rspec := NewRspec(Rspec{
		TestCommand: customCommand,
	})

	if rspec.TestCommand != customCommand {
		t.Errorf("rspec.TestCommand = %q, want %q", rspec.TestCommand, customCommand)
	}
}

func TestNewRspec_DefaultPattern(t *testing.T) {
	rspec := NewRspec(Rspec{})
	got := rspec.TestFilePattern

	want := "spec/**/*_spec.rb"

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Rspec.TestFilePattern diff (-got +want):\n%s", diff)
	}
}

func TestNewRspec_CustomPattern(t *testing.T) {
	os.Setenv("BUILDKITE_SPLITTER_TEST_FILE_PATTERN", "spec/models/**/*_spec.rb")
	defer os.Unsetenv("BUILDKITE_SPLITTER_TEST_FILE_PATTERN")

	rspec := NewRspec(Rspec{
		TestFilePattern: "spec/models/**/*_spec.rb",
	})
	got := rspec.TestFilePattern

	want := "spec/models/**/*_spec.rb"

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Rspec.TestFilePattern diff (-got +want):\n%s", diff)
	}
}

func TestNewRspec_ExcludePattern(t *testing.T) {
	os.Setenv("BUILDKITE_SPLITTER_TEST_FILE_EXCLUDE_PATTERN", "spec/features/**")
	defer os.Unsetenv("BUILDKITE_SPLITTER_TEST_FILE_EXCLUDE_PATTERN")

	rspec := NewRspec(Rspec{
		TestFileExcludePattern: "spec/features/**",
	})
	got := rspec.TestFileExcludePattern

	want := "spec/features/**"

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Rspec.TestFilePattern diff (-got +want):\n%s", diff)
	}
}

func TestRetryCommand_DefaultRetryCommand(t *testing.T) {
	testCommand := "bin/rspec --options {{testExamples}}"
	rspec := NewRspec(Rspec{
		TestCommand: testCommand,
	})

	got, err := rspec.RetryCommand()
	if err != nil {
		t.Errorf("Rspec.RetryCommand() error = %v", err)
	}

	want := "bin/rspec --options --only-failures"
	if diff := cmp.Diff(got.String(), want); diff != "" {
		t.Errorf("Rspec.RetryCommand() diff (-got +want):\n%s", diff)
	}
}

func TestRetryCommand_CustomRetryCommand(t *testing.T) {
	testCommand := "bin/rspec --options {{testExamples}}"
	retryCommand := "bin/rspec --only-failures --fast-fail"
	rspec := NewRspec(Rspec{
		TestCommand:      testCommand,
		RetryTestCommand: retryCommand,
	})

	got, err := rspec.RetryCommand()
	if err != nil {
		t.Errorf("Rspec.RetryCommand() error = %v", err)
	}

	want := "bin/rspec --only-failures --fast-fail"
	if diff := cmp.Diff(got.String(), want); diff != "" {
		t.Errorf("Rspec.RetryCommand() diff (-got +want):\n%s", diff)
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
