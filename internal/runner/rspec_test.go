package runner

import (
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestRspecCommand_Default(t *testing.T) {
	rspec := Rspec{}
	testCases := []string{"spec/models/user_spec.rb", "spec/models/billing_spec.rb"}
	testArgs := []string{}

	got := rspec.Command(testCases, testArgs)

	want := "bundle exec rspec spec/models/user_spec.rb spec/models/billing_spec.rb"
	if diff := cmp.Diff(got.String(), want); diff != "" {
		t.Errorf("Rspec.Command() diff (-got +want):\n%s", diff)
	}
}

func TestRspecCommand_WithTestCommandArgs(t *testing.T) {
	rspec := Rspec{}
	testCases := []string{"spec/models/user_spec.rb", "spec/models/billing_spec.rb"}
	testArgs := []string{"bin/rspec", "--options", "{{testExample}}", "--format"}

	got := rspec.Command(testCases, testArgs)

	want := "bin/rspec --options spec/models/user_spec.rb spec/models/billing_spec.rb --format"
	if diff := cmp.Diff(got.String(), want); diff != "" {
		t.Errorf("Rspec.Command() diff (-got +want):\n%s", diff)
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
