package config

import (
	"errors"
	"testing"

	"github.com/buildkite/test-engine-client/internal/env"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestConfigReadFromEnv(t *testing.T) {
	c := Config{}
	err := c.readFromEnv(env.Map{
		"BUILDKITE_PARALLEL_JOB_COUNT":                       "10",
		"BUILDKITE_PARALLEL_JOB":                             "0",
		"BUILDKITE_TEST_ENGINE_BASE_URL":                     "https://buildkite.localhost",
		"BUILDKITE_TEST_ENGINE_TEST_CMD":                     "bin/rspec {{testExamples}}",
		"BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN":             "my_token",
		"BUILDKITE_ORGANIZATION_SLUG":                        "my_org",
		"BUILDKITE_TEST_ENGINE_SUITE_SLUG":                   "my_suite",
		"BUILDKITE_TEST_ENGINE_RETRY_COUNT":                  "3",
		"BUILDKITE_TEST_ENGINE_SPLIT_BY_EXAMPLE":             "TRUE",
		"BUILDKITE_BUILD_ID":                                 "123",
		"BUILDKITE_STEP_ID":                                  "456",
		"BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN":            "spec/unit/**/*_spec.rb",
		"BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN":    "spec/feature/**/*_spec.rb",
		"BUILDKITE_TEST_ENGINE_DISABLE_RETRY_FOR_MUTED_TEST": "TRUE",
		"BUILDKITE_TEST_ENGINE_RESULT_PATH":                  "result.json",
		"BUILDKITE_TEST_ENGINE_TEST_RUNNER":                  "rspec",
	})

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
		RetryForMutedTest:      false,
		JobRetryCount:          0,
	}

	if err != nil {
		t.Errorf("config.readFromEnv() error = %v", err)
	}

	if diff := cmp.Diff(c, want, cmpopts.IgnoreUnexported(Config{})); diff != "" {
		t.Errorf("config.readFromEnv() diff (-got +want):\n%s", diff)
	}
}

func TestConfigReadFromEnv_MissingConfigWithDefault(t *testing.T) {
	c := Config{errs: InvalidConfigError{}}
	c.readFromEnv(env.Map{
		"BUILDKITE_TEST_ENGINE_BASE_URL":    "",
		"BUILDKITE_TEST_ENGINE_MODE":        "",
		"BUILDKITE_TEST_ENGINE_TEST_CMD":    "",
		"BUILDKITE_TEST_ENGINE_RETRY_COUNT": "",
		"BUILDKITE_BUILD_ID":                "123",
		"BUILDKITE_STEP_ID":                 "456",
	})

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
	c := Config{errs: InvalidConfigError{}}
	err := c.readFromEnv(env.Map{
		"BUILDKITE_BUILD_ID":           "abc",
		"BUILDKITE_STEP_ID":            "123",
		"BUILDKITE_PARALLEL_JOB_COUNT": "foo",
		"BUILDKITE_PARALLEL_JOB":       "bar",
	})

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
	c := Config{errs: InvalidConfigError{}}
	err := c.readFromEnv(env.Map{
		"BUILDKITE_TEST_ENGINE_BASE_URL":    "",
		"BUILDKITE_TEST_ENGINE_MODE":        "",
		"BUILDKITE_TEST_ENGINE_TEST_CMD":    "",
		"BUILDKITE_TEST_ENGINE_RETRY_COUNT": "",
		"BUILDKITE_STEP_ID":                 "123",
		"BUILDKITE_PARALLEL_JOB":            "1",
		"BUILDKITE_PARALLEL_JOB_COUNT":      "10",
	})

	var invConfigError InvalidConfigError
	if !errors.As(err, &invConfigError) {
		t.Errorf("config.readFromEnv() error = %v, want InvalidConfigError", err)
	}

	want := "BUILDKITE_BUILD_ID must not be blank"

	if got := invConfigError.Error(); got != want {
		t.Errorf("config.readFromEnv() got = %v, want = %v", got, want)
	}
}

func TestConfigReadFromEnv_MissingStepId(t *testing.T) {
	c := Config{errs: InvalidConfigError{}}
	err := c.readFromEnv(env.Map{
		"BUILDKITE_TEST_ENGINE_BASE_URL":    "",
		"BUILDKITE_TEST_ENGINE_MODE":        "",
		"BUILDKITE_TEST_ENGINE_TEST_CMD":    "",
		"BUILDKITE_TEST_ENGINE_RETRY_COUNT": "",
		"BUILDKITE_BUILD_ID":                "123",
		"BUILDKITE_PARALLEL_JOB":            "1",
		"BUILDKITE_PARALLEL_JOB_COUNT":      "10",
	})

	var invConfigError InvalidConfigError
	if !errors.As(err, &invConfigError) {
		t.Errorf("config.readFromEnv() error = %v, want InvalidConfigError", err)
	}

	want := "BUILDKITE_STEP_ID must not be blank"

	if errors.As(err, &invConfigError) {
		if got := invConfigError.Error(); got != want {
			t.Errorf("config.readFromEnv() got = %v, want = %v", got, want)
		}
	}
}

func TestConfigReadFromEnv_InvalidParallelJob(t *testing.T) {
	c := Config{errs: InvalidConfigError{}}
	err := c.readFromEnv(env.Map{
		"BUILDKITE_BUILD_ID":           "123",
		"BUILDKITE_STEP_ID":            "456",
		"BUILDKITE_PARALLEL_JOB":       "",
		"BUILDKITE_PARALLEL_JOB_COUNT": "10",
	})

	var invConfigError InvalidConfigError
	if !errors.As(err, &invConfigError) {
		t.Errorf("config.readFromEnv() error = %v, want InvalidConfigError", err)
	}

	want := `BUILDKITE_PARALLEL_JOB was "", must be a number`

	if got := invConfigError.Error(); got != want {
		t.Errorf("config.readFromEnv() got = %v, want = %v", got, want)
	}
}

func TestConfigReadFromEnv_InvalidParallelJobCount(t *testing.T) {
	c := Config{errs: InvalidConfigError{}}
	err := c.readFromEnv(env.Map{
		"BUILDKITE_BUILD_ID":           "123",
		"BUILDKITE_STEP_ID":            "456",
		"BUILDKITE_PARALLEL_JOB":       "10",
		"BUILDKITE_PARALLEL_JOB_COUNT": "",
	})

	var invConfigError InvalidConfigError
	if !errors.As(err, &invConfigError) {
		t.Errorf("config.readFromEnv() error = %v, want InvalidConfigError", err)
	}

	want := `BUILDKITE_PARALLEL_JOB_COUNT was "", must be a number`

	if got := invConfigError.Error(); got != want {
		t.Errorf("config.readFromEnv() got = %v, want = %v", got, want)
	}
}
