package config

import (
	"errors"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestConfigReadFromEnv(t *testing.T) {
	os.Setenv("BUILDKITE_PARALLEL_JOB_COUNT", "10")
	os.Setenv("BUILDKITE_PARALLEL_JOB", "0")
	os.Setenv("BUILDKITE_SPLITTER_BASE_URL", "https://buildkite.localhost")
	os.Setenv("BUILDKITE_SPLITTER_MODE", "static")
	os.Setenv("BUILDKITE_BUILD_ID", "123")
	os.Setenv("BUILDKITE_SPLITTER_SUITE_TOKEN", "my_token")
	os.Setenv("BUILDKITE_TEST_SPLITTER_CMD", "bin/rspec {{testExamples}}")
	defer os.Clearenv()

	c := Config{}
	err := c.readFromEnv()

	want := Config{
		Parallelism:   10,
		NodeIndex:     0,
		ServerBaseUrl: "https://buildkite.localhost",
		Mode:          "static",
		Identifier:    "123",
		SuiteToken:    "my_token",
		TestCommand:   "bin/rspec {{testExamples}}",
	}

	if err != nil {
		t.Errorf("config.readFromEnv() error = %v", err)
	}

	if diff := cmp.Diff(c, want); diff != "" {
		t.Errorf("config.readFromEnv() diff (-got +want):\n%s", diff)
	}
}

func TestConfigReadFromEnv_MissingConfigWithDefault(t *testing.T) {
	os.Setenv("BUILDKITE_SPLITTER_BASE_URL", "")
	os.Setenv("BUILDKITE_SPLITTER_MODE", "")
	os.Setenv("BUILDKITE_TEST_SPLITTER_CMD", "")
	defer os.Unsetenv("BUILDKITE_SPLITTER_BASE_URL")
	defer os.Unsetenv("BUILDKITE_SPLITTER_MODE")
	defer os.Unsetenv("BUILDKITE_TEST_SPLITTER_CMD")

	c := Config{}
	c.readFromEnv()
	if c.ServerBaseUrl != "https://buildkite.com" {
		t.Errorf("ServerBaseUrl = %v, want %v", c.ServerBaseUrl, "https://buildkite.com")
	}

	if c.Mode != "static" {
		t.Errorf("Mode = %v, want %v", c.Mode, "static")
	}

	if c.TestCommand != "bundle exec rspec {{testExamples}}" {
		t.Errorf("TestCommand = %v, want %v", c.TestCommand, "bundle exec rspec {{testExamples}}")
	}
}

func TestConfigReadFromEnv_NotInteger(t *testing.T) {
	os.Setenv("BUILDKITE_PARALLEL_JOB_COUNT", "foo")
	os.Setenv("BUILDKITE_PARALLEL_JOB", "bar")
	defer os.Unsetenv("BUILDKITE_PARALLEL_JOB_COUNT")
	defer os.Unsetenv("BUILDKITE_PARALLEL_JOB")

	c := Config{}
	err := c.readFromEnv()

	var invConfigError InvalidConfigError
	if !errors.As(err, &invConfigError) {
		t.Errorf("config.readFromEnv() error = %v, want InvalidConfigError", err)
	}

	if len(invConfigError) != 2 {
		t.Errorf("config.readFromEnv() error length = %d, want 2", len(invConfigError))
	}
}
