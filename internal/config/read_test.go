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
	os.Setenv("BUILDKITE_SPLITTER_TEST_CMD", "bin/rspec {{testExamples}}")
	os.Setenv("BUILDKITE_SPLITTER_API_ACCESS_TOKEN", "my_token")
	os.Setenv("BUILDKITE_ORGANIZATION_SLUG", "my_org")
	os.Setenv("BUILDKITE_SPLITTER_SUITE_SLUG", "my_suite")
	os.Setenv("BUILDKITE_SPLITTER_RETRY_COUNT", "3")
	os.Setenv("BUILDKITE_SPLITTER_SPLIT_BY_EXAMPLE", "TRUE")
	os.Setenv("BUILDKITE_BUILD_ID", "123")
	os.Setenv("BUILDKITE_STEP_ID", "456")
	os.Setenv("BUILDKITE_SPLITTER_TEST_FILE_PATTERN", "spec/unit/**/*_spec.rb")
	os.Setenv("BUILDKITE_SPLITTER_TEST_FILE_EXCLUDE_PATTERN", "spec/feature/**/*_spec.rb")
	os.Setenv("BUILDKITE_SPLITTER_RESULT_PATH", "result.json")
	defer os.Clearenv()

	c := Config{}
	err := c.readFromEnv()

	want := Config{
		Parallelism:            10,
		NodeIndex:              0,
		ServerBaseUrl:          "https://buildkite.localhost",
		Identifier:             "123/456",
		TestCommand:            "bin/rspec {{testExamples}}",
		AccessToken:            "my_token",
		OrganizationSlug:       "my_org",
		SuiteSlug:              "my_suite",
		MaxRetries:             3,
		SplitByExample:         true,
		TestFilePattern:        "spec/unit/**/*_spec.rb",
		TestFileExcludePattern: "spec/feature/**/*_spec.rb",
		TestRunner:             "rspec",
		ResultPath:             "result.json",
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
	os.Setenv("BUILDKITE_SPLITTER_TEST_CMD", "")
	os.Setenv("BUILDKITE_SPLITTER_RETRY_COUNT", "")
	os.Setenv("BUILDKITE_BUILD_ID", "123")
	os.Setenv("BUILDKITE_STEP_ID", "456")
	defer os.Clearenv()

	c := Config{}
	c.readFromEnv()
	if c.ServerBaseUrl != "https://api.buildkite.com" {
		t.Errorf("ServerBaseUrl = %v, want %v", c.ServerBaseUrl, "https://api.buildkite.com")
	}

	if c.Identifier != "123/456" {
		t.Errorf("Identifier = %v, want %v", c.Identifier, "123/456")
	}

	if c.MaxRetries != 0 {
		t.Errorf("MaxRetries = %v, want %v", c.MaxRetries, 0)
	}
}

func TestConfigReadFromEnv_NotInteger(t *testing.T) {
	os.Setenv("BUILDKITE_BUILD_ID", "abc")
	os.Setenv("BUILDKITE_STEP_ID", "123")
	os.Setenv("BUILDKITE_PARALLEL_JOB_COUNT", "foo")
	os.Setenv("BUILDKITE_PARALLEL_JOB", "bar")
	defer os.Unsetenv("BUILDKITE_BUILD_ID")
	defer os.Unsetenv("BUILDKITE_STEP_ID")
	defer os.Unsetenv("BUILDKITE_PARALLEL_JOB_COUNT")
	defer os.Unsetenv("BUILDKITE_PARALLEL_JOB")

	c := Config{}
	err := c.readFromEnv()

	var invConfigError InvalidConfigError
	if !errors.As(err, &invConfigError) {
		t.Errorf("config.readFromEnv() error = %v, want InvalidConfigError", err)
	}

	if len(invConfigError) != 2 {
		t.Errorf("%v", invConfigError)
		t.Errorf("config.readFromEnv() error length = %d, want 2", len(invConfigError))
	}
}

func TestConfigReadFromEnv_MissingBuildId(t *testing.T) {
	os.Setenv("BUILDKITE_SPLITTER_BASE_URL", "")
	os.Setenv("BUILDKITE_SPLITTER_MODE", "")
	os.Setenv("BUILDKITE_SPLITTER_TEST_CMD", "")
	os.Setenv("BUILDKITE_SPLITTER_RETRY_COUNT", "")
	os.Setenv("BUILDKITE_STEP_ID", "123")
	defer os.Clearenv()

	c := Config{}
	err := c.readFromEnv()

	var invConfigError InvalidConfigError
	if !errors.As(err, &invConfigError) {
		t.Errorf("config.readFromEnv() error = %v, want InvalidConfigError", err)
	}

	want := "BUILDKITE_BUILD_ID must not be blank"

	if got := invConfigError[0].Error(); got != want {
		t.Errorf("config.readFromEnv() got = %v, want = %v", got, want)
	}
}

func TestConfigReadFromEnv_MissingStepId(t *testing.T) {
	os.Setenv("BUILDKITE_SPLITTER_BASE_URL", "")
	os.Setenv("BUILDKITE_SPLITTER_MODE", "")
	os.Setenv("BUILDKITE_SPLITTER_TEST_CMD", "")
	os.Setenv("BUILDKITE_SPLITTER_RETRY_COUNT", "")
	os.Setenv("BUILDKITE_BUILD_ID", "123")
	defer os.Clearenv()

	c := Config{}
	err := c.readFromEnv()

	var invConfigError InvalidConfigError
	if !errors.As(err, &invConfigError) {
		t.Errorf("config.readFromEnv() error = %v, want InvalidConfigError", err)
	}

	want := "BUILDKITE_STEP_ID must not be blank"

	if errors.As(err, &invConfigError) {
		if got := invConfigError[0].Error(); got != want {
			t.Errorf("config.readFromEnv() got = %v, want = %v", got, want)
		}
	}
}
