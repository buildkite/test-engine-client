package config

import (
	"strconv"
)

func (c Config) DumpEnv() map[string]string {
	envs := make(map[string]string)
	envs["BUILDKITE_BUILD_ID"] = c.BuildId
	envs["BUILDKITE_JOB_ID"] = c.JobId
	envs["BUILDKITE_STEP_ID"] = c.StepId
	envs["BUILDKITE_ORGANIZATION_SLUG"] = c.OrganizationSlug
	envs["BUILDKITE_PARALLEL_JOB_COUNT"] = strconv.Itoa(c.Parallelism)
	envs["BUILDKITE_PARALLEL_JOB"] = strconv.Itoa(c.NodeIndex)
	envs["BUILDKITE_TEST_ENGINE_DEBUG_ENABLED"] = strconv.FormatBool(c.DebugEnabled)
	envs["BUILDKITE_TEST_ENGINE_RETRY_COUNT"] = strconv.Itoa(c.MaxRetries)
	envs["BUILDKITE_TEST_ENGINE_RETRY_CMD"] = c.RetryCommand
	envs["BUILDKITE_TEST_ENGINE_SPLIT_BY_EXAMPLE"] = strconv.FormatBool(c.SplitByExample)
	envs["BUILDKITE_TEST_ENGINE_SUITE_SLUG"] = c.SuiteSlug
	envs["BUILDKITE_TEST_ENGINE_TEST_CMD"] = c.TestCommand
	envs["BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN"] = c.TestFileExcludePattern
	envs["BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN"] = c.TestFilePattern
	envs["BUILDKITE_TEST_ENGINE_TEST_RUNNER"] = c.TestRunner
	envs["BUILDKITE_BRANCH"] = c.Branch
	envs["BUILDKITE_RETRY_COUNT"] = strconv.Itoa(c.JobRetryCount)
	envs["BUILDKITE_TEST_ENGINE_IDENTIFIER"] = c.Identifier
	envs["BUILDKITE_TEST_ENGINE_DEBUG_ENABLED"] = strconv.FormatBool(c.DebugEnabled)

	return envs
}
