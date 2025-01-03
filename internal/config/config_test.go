package config

import (
	"errors"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func setEnv(t *testing.T) {
	t.Helper()
	os.Setenv("BUILDKITE_PARALLEL_JOB_COUNT", "60")
	os.Setenv("BUILDKITE_PARALLEL_JOB", "7")
	os.Setenv("BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN", "my_token")
	os.Setenv("BUILDKITE_TEST_ENGINE_BASE_URL", "https://build.kite")
	os.Setenv("BUILDKITE_TEST_ENGINE_TEST_CMD", "bin/rspec {{testExamples}}")
	os.Setenv("BUILDKITE_ORGANIZATION_SLUG", "my_org")
	os.Setenv("BUILDKITE_TEST_ENGINE_SUITE_SLUG", "my_suite")
	os.Setenv("BUILDKITE_BUILD_ID", "123")
	os.Setenv("BUILDKITE_STEP_ID", "456")
	os.Setenv("BUILDKITE_TEST_ENGINE_TEST_RUNNER", "rspec")
	os.Setenv("BUILDKITE_TEST_ENGINE_RESULT_PATH", "tmp/rspec.json")
	os.Setenv("BUILDKITE_RETRY_COUNT", "0")
}

func TestNewConfig(t *testing.T) {
	setEnv(t)
	defer os.Clearenv()

	c, err := New()
	if err != nil {
		t.Errorf("config.New() error = %v", err)
	}

	want := Config{
		Parallelism:      60,
		NodeIndex:        7,
		ServerBaseUrl:    "https://build.kite",
		Identifier:       "123/456",
		TestCommand:      "bin/rspec {{testExamples}}",
		AccessToken:      "my_token",
		OrganizationSlug: "my_org",
		ResultPath:       "tmp/rspec.json",
		SuiteSlug:        "my_suite",
		TestRunner:       "rspec",
		JobRetryCount:    0,
		errs:             InvalidConfigError{},
	}

	if diff := cmp.Diff(c, want, cmpopts.IgnoreUnexported(Config{})); diff != "" {
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
	os.Unsetenv("BUILDKITE_TEST_ENGINE_MODE")
	os.Unsetenv("BUILDKITE_TEST_ENGINE_BASE_URL")
	os.Unsetenv("BUILDKITE_TEST_ENGINE_TEST_CMD")
	defer os.Clearenv()

	c, err := New()
	if err != nil {
		t.Errorf("config.New() error = %v", err)
	}

	want := Config{
		Parallelism:      60,
		NodeIndex:        7,
		ServerBaseUrl:    "https://api.buildkite.com",
		Identifier:       "123/456",
		AccessToken:      "my_token",
		OrganizationSlug: "my_org",
		SuiteSlug:        "my_suite",
		TestRunner:       "rspec",
		ResultPath:       "tmp/rspec.json",
		JobRetryCount:    0,
	}

	if diff := cmp.Diff(c, want, cmpopts.IgnoreUnexported(Config{})); diff != "" {
		t.Errorf("config.New() diff (-got +want):\n%s", diff)
	}
}

func TestNewConfig_InvalidConfig(t *testing.T) {
	setEnv(t)
	os.Setenv("BUILDKITE_TEST_ENGINE_MODE", "dynamic")
	os.Unsetenv("BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN")
	defer os.Clearenv()

	_, err := New()

	var invConfigError InvalidConfigError
	if !errors.As(err, &invConfigError) {
		t.Errorf("config.Validate() error = %v, want InvalidConfigError", err)
	}

	if len(invConfigError) != 1 {
		t.Errorf("config.readFromEnv() error length = %d, want 2", len(invConfigError))
	}
}
