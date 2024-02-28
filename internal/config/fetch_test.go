package config

import (
	"os"
	"testing"
)

func TestConfigFetchFromEnv(t *testing.T) {
	t.Run("ServerBaseUrl default value", func(t *testing.T) {
		c := Config{}
		c.fetchFromEnv()
		if c.ServerBaseUrl != "https://buildkite.com" {
			t.Errorf("ServerBaseUrl = %v, want %v", c.ServerBaseUrl, "https://buildkite.com")
		}
	})

	t.Run("Mode default value", func(t *testing.T) {
		c := Config{}
		c.fetchFromEnv()
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
		err := c.fetchFromEnv()
		if err == nil {
			t.Errorf("config.fetchFromEnv expected error, got nil")
		}
	})

	t.Run("Node index is not an integer", func(t *testing.T) {
		os.Setenv("BUILDKITE_PARALLEL_JOB_COUNT", "10")
		defer os.Unsetenv("BUILDKITE_PARALLEL_JOB_COUNT")

		os.Setenv("BUILDKITE_PARALLEL_JOB", "bar")
		defer os.Unsetenv("BUILDKITE_PARALLEL_JOB")

		c := Config{}
		err := c.fetchFromEnv()
		if err == nil {
			t.Errorf("config.fetchFromEnv expected error, got nil")
		}
	})
}
