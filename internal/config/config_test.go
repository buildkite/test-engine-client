package config

import (
	"errors"
	"testing"

	"github.com/buildkite/test-engine-client/internal/env"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func getExampleEnv() env.Env {
	return env.Map{
		"BUILDKITE_PARALLEL_JOB_COUNT":           "60",
		"BUILDKITE_PARALLEL_JOB":                 "7",
		"BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN": "my_token",
		"BUILDKITE_TEST_ENGINE_BASE_URL":         "https://build.kite",
		"BUILDKITE_TEST_ENGINE_TEST_CMD":         "bin/rspec {{testExamples}}",
		"BUILDKITE_ORGANIZATION_SLUG":            "my_org",
		"BUILDKITE_TEST_ENGINE_SUITE_SLUG":       "my_suite",
		"BUILDKITE_BUILD_ID":                     "123",
		"BUILDKITE_STEP_ID":                      "456",
		"BUILDKITE_TEST_ENGINE_TEST_RUNNER":      "rspec",
		"BUILDKITE_TEST_ENGINE_RESULT_PATH":      "tmp/rspec.json",
		"BUILDKITE_RETRY_COUNT":                  "0",
	}
}

func TestNewConfig(t *testing.T) {
	c, err := New(getExampleEnv())
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
	_, err := New(env.Map{})

	if !errors.As(err, new(InvalidConfigError)) {
		t.Errorf("config.Validate() error = %v, want InvalidConfigError", err)
	}
}

func TestNewConfig_MissingConfigWithDefault(t *testing.T) {
	env := getExampleEnv()
	env.Delete("BUILDKITE_TEST_ENGINE_MODE")
	env.Delete("BUILDKITE_TEST_ENGINE_BASE_URL")
	env.Delete("BUILDKITE_TEST_ENGINE_TEST_CMD")

	c, err := New(env)
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
	env := getExampleEnv()
	env.Set("BUILDKITE_TEST_ENGINE_MODE", "dynamic")
	env.Delete("BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN")

	_, err := New(env)

	var invConfigError InvalidConfigError
	if !errors.As(err, &invConfigError) {
		t.Errorf("config.Validate() error = %v, want InvalidConfigError", err)
	}

	if len(invConfigError) != 1 {
		t.Errorf("config.readFromEnv() error length = %d, want 2", len(invConfigError))
	}
}
