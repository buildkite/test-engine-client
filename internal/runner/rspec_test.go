package runner

import (
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
)

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
	os.Setenv("BUILDKITE_SPLITTER_PATTERN", "spec/models/**/*_spec.rb")
	defer os.Unsetenv("BUILDKITE_SPLITTER_PATTERN")

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
	os.Setenv("BUILDKITE_SPLITTER_EXCLUDE_PATTERN", "spec/features/**")
	defer os.Unsetenv("BUILDKITE_SPLITTER_EXCLUDE_PATTERN")

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
