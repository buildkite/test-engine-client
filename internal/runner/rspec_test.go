package runner

import (
	"errors"
	"os"
	"testing"

	"github.com/buildkite/test-splitter/internal/plan"
	"github.com/google/go-cmp/cmp"
	"github.com/kballard/go-shellquote"
)

func TestNewRspec_DefaultCommand(t *testing.T) {
	defaultCommand := "bundle exec rspec {{testExamples}}"
	rspec := NewRspec("")

	if rspec.TestCommand != defaultCommand {
		t.Errorf("rspec.TestCommand = %q, want %q", rspec.TestCommand, defaultCommand)
	}
}

func TestNewRspec_CustomCommand(t *testing.T) {
	customCommand := "bin/rspec --options {{testExamples}} --format"
	rspec := NewRspec(customCommand)

	if rspec.TestCommand != customCommand {
		t.Errorf("rspec.TestCommand = %q, want %q", rspec.TestCommand, customCommand)
	}
}

func TestCommandNameAndArgs_WithInterpolationPlaceholder(t *testing.T) {
	testCases := []string{"spec/models/user_spec.rb", "spec/models/billing_spec.rb"}
	testCommand := "bin/rspec --options {{testExamples}} --format"
	rspec := NewRspec(testCommand)

	gotName, gotArgs, err := rspec.commandNameAndArgs(testCases)
	if err != nil {
		t.Errorf("Rspec.commandNameAndArgs(%q, %q) error = %v", testCases, testCommand, err)
	}

	wantName := "bin/rspec"
	wantArgs := []string{"--options", "spec/models/user_spec.rb", "spec/models/billing_spec.rb", "--format"}

	if diff := cmp.Diff(gotName, wantName); diff != "" {
		t.Errorf("Rspec.commandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("Rspec.commandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
}

func TestCommandNameAndArgs_WithoutInterpolationPlaceholder(t *testing.T) {
	testCases := []string{"spec/models/user_spec.rb", "spec/models/billing_spec.rb"}
	testCommand := "bin/rspec --options --format"
	rspec := NewRspec(testCommand)

	gotName, gotArgs, err := rspec.commandNameAndArgs(testCases)
	if err != nil {
		t.Errorf("Rspec.commandNameAndArgs(%q, %q) error = %v", testCases, testCommand, err)
	}

	wantName := "bin/rspec"
	wantArgs := []string{"--options", "--format", "spec/models/user_spec.rb", "spec/models/billing_spec.rb"}

	if diff := cmp.Diff(gotName, wantName); diff != "" {
		t.Errorf("Rspec.commandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("Rspec.commandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
}

func TestCommandNameAndArgs_InvalidTestCommand(t *testing.T) {
	testCases := []string{"spec/models/user_spec.rb", "spec/models/billing_spec.rb"}
	testCommand := "bin/rspec --options ' {{testExamples}}"
	rspec := NewRspec(testCommand)

	gotName, gotArgs, err := rspec.commandNameAndArgs(testCases)

	wantName := ""
	wantArgs := []string{}

	if diff := cmp.Diff(gotName, wantName); diff != "" {
		t.Errorf("Rspec.commandNameAndArgs() diff (-got +want):\n%s", diff)
	}
	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("Rspec.commandNameAndArgs() diff (-got +want):\n%s", diff)
	}
	if !errors.Is(err, shellquote.UnterminatedSingleQuoteError) {
		t.Errorf("Rspec.commandNameAndArgs() error = %v, want %v", err, shellquote.UnterminatedSingleQuoteError)
	}
}

func TestRetryCommand_DefaultRetryCommand(t *testing.T) {
	testCommand := "bin/rspec --options {{testExamples}}"
	rspec := NewRspec(testCommand)

	got, err := rspec.RetryCommand()
	if err != nil {
		t.Errorf("Rspec.RetryCommand() error = %v", err)
	}

	want := "bin/rspec --options --only-failures"
	if diff := cmp.Diff(got.String(), want); diff != "" {
		t.Errorf("Rspec.RetryCommand() diff (-got +want):\n%s", diff)
	}
}

func TestRspecDiscoveryPattern_Default(t *testing.T) {
	rspec := Rspec{}
	got := rspec.discoveryPattern()

	want := DiscoveryPattern{
		IncludePattern: "spec/**/*_spec.rb",
		ExcludePattern: "",
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Rspec.discoveryPattern() diff (-got +want):\n%s", diff)
	}
}

func TestRspecDiscoveryPattern_IncludePattern(t *testing.T) {
	os.Setenv("BUILDKITE_SPLITTER_TEST_FILE_PATTERN", "spec/models/**/*_spec.rb")
	defer os.Unsetenv("BUILDKITE_SPLITTER_TEST_FILE_PATTERN")

	rspec := Rspec{}
	got := rspec.discoveryPattern()

	want := DiscoveryPattern{
		IncludePattern: "spec/models/**/*_spec.rb",
		ExcludePattern: "",
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Rspec.discoveryPattern() diff (-got +want):\n%s", diff)
	}
}

func TestRspecDiscoveryPattern_ExcludePattern(t *testing.T) {
	os.Setenv("BUILDKITE_SPLITTER_TEST_FILE_EXCLUDE_PATTERN", "spec/features/**")
	defer os.Unsetenv("BUILDKITE_SPLITTER_TEST_FILE_EXCLUDE_PATTERN")

	rspec := Rspec{}
	got := rspec.discoveryPattern()

	want := DiscoveryPattern{
		IncludePattern: "spec/**/*_spec.rb",
		ExcludePattern: "spec/features/**",
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Rspec.discoveryPattern() diff (-got +want):\n%s", diff)
	}
}

func TestRspecGetExamples(t *testing.T) {
	rspec := NewRspec("rspec")
	files := []string{"fixtures/spec/spells/expelliarmus_spec.rb"}
	got, err := rspec.GetExamples(files)

	want := []plan.TestCase{
		{
			Format:     plan.TestCaseFormatExample,
			Identifier: "./fixtures/spec/spells/expelliarmus_spec.rb[1:1]",
			Name:       "disarms the opponent",
			Path:       "./fixtures/spec/spells/expelliarmus_spec.rb:2",
			Scope:      "Expelliarmus disarms the opponent",
		},
		{
			Format:     plan.TestCaseFormatExample,
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
	files := []string{"fixtures/spec/spells/expelliarmus_spec.rb"}
	want := []plan.TestCase{
		{
			Format:     plan.TestCaseFormatExample,
			Identifier: "./fixtures/spec/spells/expelliarmus_spec.rb[1:1]",
			Name:       "disarms the opponent",
			Path:       "./fixtures/spec/spells/expelliarmus_spec.rb:2",
			Scope:      "Expelliarmus disarms the opponent",
		},
		{
			Format:     plan.TestCaseFormatExample,
			Identifier: "./fixtures/spec/spells/expelliarmus_spec.rb[1:2]",
			Name:       "knocks the wand out of the opponents hand",
			Path:       "./fixtures/spec/spells/expelliarmus_spec.rb:6",
			Scope:      "Expelliarmus knocks the wand out of the opponents hand",
		},
	}

	commands := []string{"rspec --format documentation", "rspec --format json --out rspec.json", "rspec --format html"}
	for _, command := range commands {
		rspec := NewRspec(command)
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
