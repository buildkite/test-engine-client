package runner

import (
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestCommandNameAndArgs_WithCommandArgs(t *testing.T) {
	rspec := Rspec{}
	testCases := []string{"spec/models/user_spec.rb", "spec/models/billing_spec.rb"}
	testArgs := []string{"bin/rspec", "--options", "{{testExamples}}", "--format"}

	gotName, gotArgs := rspec.commandNameAndArgs(testCases, testArgs)

	wantName := "bin/rspec"
	wantArgs := []string{"--options", "spec/models/user_spec.rb", "spec/models/billing_spec.rb", "--format"}

	if diff := cmp.Diff(gotName, wantName); diff != "" {
		t.Errorf("Rspec.commandNameAndArgs() diff (-got +want):\n%s", diff)
	}
	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("Rspec.commandNameAndArgs() diff (-got +want):\n%s", diff)
	}
}

func TestCommandNameAndArgs_WithoutTestPlaceholder(t *testing.T) {
	rspec := Rspec{}
	testCases := []string{"spec/models/user_spec.rb", "spec/models/billing_spec.rb"}
	testArgs := []string{"bin/rspec", "--options", "--format"}

	gotName, gotArgs := rspec.commandNameAndArgs(testCases, testArgs)

	wantName := "bin/rspec"
	wantArgs := []string{"--options", "--format", "spec/models/user_spec.rb", "spec/models/billing_spec.rb"}

	if diff := cmp.Diff(gotName, wantName); diff != "" {
		t.Errorf("Rspec.commandNameAndArgs() diff (-got +want):\n%s", diff)
	}
	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("Rspec.commandNameAndArgs() diff (-got +want):\n%s", diff)
	}
}

func TestCommandNameAndArgs_DefaultCommand(t *testing.T) {
	rspec := Rspec{}
	testCases := []string{"spec/models/user_spec.rb", "spec/models/billing_spec.rb"}
	testArgs := []string{}

	gotName, gotArgs := rspec.commandNameAndArgs(testCases, testArgs)

	wantName := "bundle"
	wantArgs := []string{"exec", "rspec", "spec/models/user_spec.rb", "spec/models/billing_spec.rb"}

	if diff := cmp.Diff(gotName, wantName); diff != "" {
		t.Errorf("Rspec.commandNameAndArgs() diff (-got +want):\n%s", diff)
	}
	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("Rspec.commandNameAndArgs() diff (-got +want):\n%s", diff)
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
