package config

import (
	"errors"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func setEnv(t *testing.T) {
	t.Helper()
	os.Setenv("BUILDKITE_PARALLEL_JOB_COUNT", "60")
	os.Setenv("BUILDKITE_PARALLEL_JOB", "7")
	os.Setenv("BUILDKITE_SPLITTER_BASE_URL", "https://build.kite")
	os.Setenv("BUILDKITE_SPLITTER_MODE", "static")
	os.Setenv("BUILDKITE_SPLITTER_IDENTIFIER", "xyz")
	os.Setenv("BUILDKITE_SPLITTER_SUITE_TOKEN", "my_token")
	os.Setenv("BUILDKITE_SPLITTER_TEST_CMD", "bin/rspec {{testExamples}}")
}

func TestNewConfig(t *testing.T) {
	setEnv(t)
	defer os.Clearenv()

	c, err := New()
	if err != nil {
		t.Errorf("config.New() error = %v", err)
	}

	want := Config{
		Parallelism:   60,
		NodeIndex:     7,
		ServerBaseUrl: "https://build.kite",
		Mode:          "static",
		Identifier:    "xyz",
		SuiteToken:    "my_token",
		TestCommand:   "bin/rspec {{testExamples}}",
	}

	if diff := cmp.Diff(c, want); diff != "" {
		t.Errorf("config.New() diff (-got +want):\n%s", diff)
	}
}

func TestNewConfig_EmptyConfig(t *testing.T) {
	os.Clearenv()

	_, err := New()

	if !errors.As(err, new(InvalidConfigError)) {
		t.Errorf("config.Validate() error = %v, want InvalidConfigError", err)
	}
}

func TestNewConfig_MissingConfigWithDefault(t *testing.T) {
	setEnv(t)
	os.Unsetenv("BUILDKITE_SPLITTER_MODE")
	os.Unsetenv("BUILDKITE_SPLITTER_BASE_URL")
	os.Unsetenv("BUILDKITE_SPLITTER_TEST_CMD")
	defer os.Clearenv()

	c, err := New()
	if err != nil {
		t.Errorf("config.New() error = %v", err)
	}

	want := Config{
		Parallelism:   60,
		NodeIndex:     7,
		ServerBaseUrl: "https://buildkite.com",
		Mode:          "static",
		Identifier:    "xyz",
		SuiteToken:    "my_token",
	}

	if diff := cmp.Diff(c, want); diff != "" {
		t.Errorf("config.New() diff (-got +want):\n%s", diff)
	}
}

func TestNewConfig_InvalidConfig(t *testing.T) {
	setEnv(t)
	os.Setenv("BUILDKITE_SPLITTER_MODE", "dynamic")
	os.Unsetenv("BUILDKITE_SPLITTER_SUITE_TOKEN")
	defer os.Clearenv()

	_, err := New()

	var invConfigError InvalidConfigError
	if !errors.As(err, &invConfigError) {
		t.Errorf("config.Validate() error = %v, want InvalidConfigError", err)
	}

	if len(invConfigError) != 2 {
		t.Errorf("config.readFromEnv() error length = %d, want 2", len(invConfigError))
	}
}
