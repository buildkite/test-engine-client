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
	os.Setenv("BUILDKITE_BUILD_ID", "xyz")
	os.Setenv("BUILDKITE_SUITE_TOKEN", "my_token")
}

func TestNewConfig(t *testing.T) {
	setEnv(t)
	defer os.Clearenv()

	c, err := New()
	if err != nil {
		t.Errorf("config.New() expected no error, got error %v", err)
	}

	want := Config{
		Parallelism:   60,
		NodeIndex:     7,
		ServerBaseUrl: "https://build.kite",
		Mode:          "static",
		Identifier:    "xyz",
		SuiteToken:    "my_token",
	}

	if diff := cmp.Diff(c, want); diff != "" {
		t.Errorf("config.New() diff (-got +want):\n%s", diff)
	}
}

func TestNewConfig_EmptyConfig(t *testing.T) {
	os.Clearenv()

	_, err := New()

	if !errors.As(err, new(InvalidConfigError)) {
		t.Errorf("config.Validate() expected InvalidConfigError, got %v", err)
	}
}

func TestNewConfig_MissingConfigWithDefault(t *testing.T) {
	setEnv(t)
	os.Unsetenv("BUILDKITE_SPLITTER_MODE")
	os.Unsetenv("BUILDKITE_SPLITTER_BASE_URL")
	defer os.Clearenv()

	c, err := New()
	if err != nil {
		t.Errorf("config.New() expected no error, got error %v", err)
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
	os.Unsetenv("BUILDKITE_SUITE_TOKEN")
	defer os.Clearenv()

	_, err := New()

	if !errors.As(err, new(InvalidConfigError)) {
		t.Errorf("config.Validate() expected InvalidConfigError, got %v", err)
		return
	}

	validationErrors := err.(InvalidConfigError)
	if len(validationErrors) != 2 {
		t.Errorf("config.readFromEnv() expected 2 error, got %v", len(validationErrors))
	}
}
