package runner

import (
	"errors"
	"os"
	"testing"

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
