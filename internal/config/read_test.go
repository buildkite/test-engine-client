package config

import (
	"errors"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestConfigreadFromEnv(t *testing.T) {
	t.Run("all environment variables are present", func(t *testing.T) {
		os.Setenv("BUILDKITE_PARALLEL_JOB_COUNT", "10")
		os.Setenv("BUILDKITE_PARALLEL_JOB", "5")
		os.Setenv("BUILDKITE_SPLITTER_BASE_URL", "https://buildkite.localhost")
		os.Setenv("BUILDKITE_SPLITTER_MODE", "static")
		os.Setenv("BUILDKITE_BUILD_ID", "123")
		os.Setenv("BUILDKITE_SUITE_TOKEN", "my_token")
		defer os.Clearenv()

		c := Config{}
		err := c.readFromEnv()

		want := Config{
			Parallelism:   10,
			NodeIndex:     5,
			ServerBaseUrl: "https://buildkite.localhost",
			Mode:          "static",
			Identifier:    "123",
			SuiteToken:    "my_token",
		}

		if err != nil {
			t.Errorf("config.readFromEnv() expected no error, got error %v", err)
		}

		if diff := cmp.Diff(c, want); diff != "" {
			t.Errorf("config.readFromEnv() diff (-got +want):\n%s", diff)
		}
	})

	t.Run("ServerBaseUrl is missing", func(t *testing.T) {
		os.Setenv("BUILDKITE_SPLITTER_BASE_URL", "")
		defer os.Unsetenv("BUILDKITE_SPLITTER_BASE_URL")

		c := Config{}
		c.readFromEnv()
		if c.ServerBaseUrl != "https://buildkite.com" {
			t.Errorf("ServerBaseUrl = %v, want %v", c.ServerBaseUrl, "https://buildkite.com")
		}
	})

	t.Run("Mode is missing", func(t *testing.T) {
		os.Setenv("BUILDKITE_SPLITTER_MODE", "")
		defer os.Unsetenv("BUILDKITE_SPLITTER_MODE")

		c := Config{}
		c.readFromEnv()
		if c.Mode != "static" {
			t.Errorf("Mode = %v, want %v", c.Mode, "static")
		}
	})

	t.Run("Parallelism is not an integer", func(t *testing.T) {
		os.Setenv("BUILDKITE_PARALLEL_JOB_COUNT", "foo")
		defer os.Unsetenv("BUILDKITE_PARALLEL_JOB_COUNT")

		os.Setenv("BUILDKITE_PARALLEL_JOB", "5")
		defer os.Unsetenv("BUILDKITE_PARALLEL_JOB")

		c := Config{}
		err := c.readFromEnv()
		if err == nil {
			t.Errorf("config.readFromEnv() expected error, got nil")
		}

		if !errors.As(err, new(InvalidConfigError)) {
			t.Errorf("config.readFromEnv() expected ValidationError, got %v", err)
		}

		validationErrors := err.(InvalidConfigError)
		if len(validationErrors) != 1 {
			t.Errorf("config.readFromEnv() expected 1 error, got %v", len(validationErrors))
		}

		if validationErrors[0].name != "Parallelism" {
			t.Errorf("config.readFromEnv() expected error name %v, got %v", "Parallelism", validationErrors[0].name)
		}
	})

	t.Run("Node index is not an integer", func(t *testing.T) {
		os.Setenv("BUILDKITE_PARALLEL_JOB_COUNT", "10")
		defer os.Unsetenv("BUILDKITE_PARALLEL_JOB_COUNT")

		os.Setenv("BUILDKITE_PARALLEL_JOB", "bar")
		defer os.Unsetenv("BUILDKITE_PARALLEL_JOB")

		c := Config{}
		err := c.readFromEnv()
		if err == nil {
			t.Errorf("config.readFromEnv() expected error, got nil")
		}

		if !errors.As(err, new(InvalidConfigError)) {
			t.Errorf("config.readFromEnv() expected ValidationError, got %v", err)
		}

		validationErrors := err.(InvalidConfigError)
		if len(validationErrors) != 1 {
			t.Errorf("config.readFromEnv() expected 1 error, got %v", len(validationErrors))
		}

		if validationErrors[0].name != "NodeIndex" {
			t.Errorf("config.readFromEnv() expected error name %v, got %v", "NodeIndex", validationErrors[0].name)
		}
	})
}
