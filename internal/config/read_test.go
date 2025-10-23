package config

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestConfigReadFromEnv(t *testing.T) {
	c := Config{errs: InvalidConfigError{}}

	c.readFromEnv(map[string]string{
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
		BuildId:                "123",
		StepId:                 "456",
		Parallelism:            10,
		NodeIndex:              0,
		ServerBaseUrl:          "https://buildkite.localhost",
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

	if diff := cmp.Diff(c, want, cmpopts.IgnoreUnexported(Config{})); diff != "" {
		t.Errorf("config.readFromEnv() diff (-got +want):\n%s", diff)
	}
}

func TestConfigReadFromEnv_MissingConfigWithDefault(t *testing.T) {
	c := Config{errs: InvalidConfigError{}}
	c.readFromEnv(map[string]string{
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

	if c.MaxRetries != 0 {
		t.Errorf("MaxRetries = %v, want %v", c.MaxRetries, 0)
	}
}

func TestConfigReadFromEnv_NotInteger(t *testing.T) {
	c := Config{errs: InvalidConfigError{}}
	c.readFromEnv(map[string]string{
		"BUILDKITE_BUILD_ID":           "abc",
		"BUILDKITE_STEP_ID":            "123",
		"BUILDKITE_PARALLEL_JOB_COUNT": "foo",
		"BUILDKITE_PARALLEL_JOB":       "bar",
	})

	if len(c.errs) != 2 {
		t.Errorf("%v", c.errs)
		t.Errorf("config.readFromEnv() error length = %d, want 2", len(c.errs))
	}
}

func TestConfigReadFromEnv_InvalidParallelJob(t *testing.T) {
	c := Config{errs: InvalidConfigError{}}
	c.readFromEnv(map[string]string{
		"BUILDKITE_BUILD_ID":           "123",
		"BUILDKITE_STEP_ID":            "456",
		"BUILDKITE_PARALLEL_JOB":       "",
		"BUILDKITE_PARALLEL_JOB_COUNT": "10",
	})

	var invConfigError InvalidConfigError
	if !errors.As(c.errs, &invConfigError) {
		t.Errorf("config.readFromEnv() error = %v, want InvalidConfigError", c.errs)
	}

	want := `BUILDKITE_PARALLEL_JOB was "", must be a number`

	if got := invConfigError.Error(); got != want {
		t.Errorf("config.readFromEnv() got = %v, want = %v", got, want)
	}
}

func TestConfigReadFromEnv_InvalidParallelJobCount(t *testing.T) {
	c := Config{errs: InvalidConfigError{}}
	c.readFromEnv(map[string]string{
		"BUILDKITE_BUILD_ID":           "123",
		"BUILDKITE_STEP_ID":            "456",
		"BUILDKITE_PARALLEL_JOB":       "10",
		"BUILDKITE_PARALLEL_JOB_COUNT": "",
	})

	var invConfigError InvalidConfigError
	if !errors.As(c.errs, &invConfigError) {
		t.Errorf("config.readFromEnv() error = %v, want InvalidConfigError", c.errs)
	}

	want := `BUILDKITE_PARALLEL_JOB_COUNT was "", must be a number`

	if got := invConfigError.Error(); got != want {
		t.Errorf("config.readFromEnv() got = %v, want = %v", got, want)
	}
}
