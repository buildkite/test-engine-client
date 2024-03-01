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
	t.Run("all configurations are present and valid", func(t *testing.T) {
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
	})

	t.Run("all configurations are missing", func(t *testing.T) {
		os.Clearenv()

		_, err := New()
		if err == nil {
			t.Errorf("config.New() expected error, got nil")
		}

		if !errors.As(err, new(InvalidConfigError)) {
			t.Errorf("config.Validate() expected InvalidConfigError, got %v", err)
		}
	})

	t.Run("some configurations are invalid", func(t *testing.T) {
		setEnv(t)
		os.Setenv("BUILDKITE_SPLITTER_MODE", "dynamic")
		os.Unsetenv("BUILDKITE_SUITE_TOKEN")
		defer os.Clearenv()

		_, err := New()
		if err == nil {
			t.Errorf("config.New() expected error, got nil")
		}

		if !errors.As(err, new(InvalidConfigError)) {
			t.Errorf("config.Validate() expected InvalidConfigError, got %v", err)
		}

		validationErrors := err.(InvalidConfigError)
		if len(validationErrors) != 2 {
			t.Errorf("config.readFromEnv() expected 1 error, got %v", len(validationErrors))
		}
	})

	t.Run("configurations with default values are missing", func(t *testing.T) {
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
	})
}
