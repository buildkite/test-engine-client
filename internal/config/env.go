package config

import (
	"strconv"
)

// getEnvWithDefault retrieves the value of the environment variable named by the key.
// If the variable is present and not empty, the value is returned.
// Otherwise the returned value will be the default value.
func getEnvWithDefault(env map[string]string, key string, defaultValue string) string {
	value, ok := env[key]
	if !ok {
		return defaultValue
	}
	if value == "" {
		return defaultValue
	}
	return value
}

func getIntEnvWithDefault(env map[string]string, key string, defaultValue int) (int, error) {
	value := env[key]
	// If the environment variable is not set, return the default value.
	if value == "" {
		return defaultValue, nil
	}
	// Convert the value to int, and return error if it fails.
	valueInt, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue, err
	}
	// Return the value if it's successfully converted to int.
	return valueInt, nil
}

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
