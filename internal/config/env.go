package config

import (
	"strconv"

	"github.com/buildkite/test-engine-client/internal/env"
)

// getEnvWithDefault retrieves the value of the environment variable named by the key.
// If the variable is present and not empty, the value is returned.
// Otherwise the returned value will be the default value.
func getEnvWithDefault(env env.Env, key string, defaultValue string) string {
	value, ok := env.Lookup(key)
	if !ok {
		return defaultValue
	}
	if value == "" {
		return defaultValue
	}
	return value
}

func getIntEnvWithDefault(env env.Env, key string, defaultValue int) (int, error) {
	value := env.Get(key)
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

func (c Config) DumpEnv(env env.Env) map[string]string {
	keys := []string{
		"BUILDKITE_BUILD_ID",
		"BUILDKITE_JOB_ID",
		"BUILDKITE_ORGANIZATION_SLUG",
		"BUILDKITE_PARALLEL_JOB_COUNT",
		"BUILDKITE_PARALLEL_JOB",
		"BUILDKITE_TEST_ENGINE_DEBUG_ENABLED",
		"BUILDKITE_TEST_ENGINE_RETRY_COUNT",
		"BUILDKITE_TEST_ENGINE_RETRY_CMD",
		"BUILDKITE_TEST_ENGINE_SPLIT_BY_EXAMPLE",
		"BUILDKITE_TEST_ENGINE_SUITE_SLUG",
		"BUILDKITE_TEST_ENGINE_TEST_CMD",
		"BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN",
		"BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN",
		"BUILDKITE_TEST_ENGINE_TEST_RUNNER",
		"BUILDKITE_STEP_ID",
		"BUILDKITE_BRANCH",
		"BUILDKITE_RETRY_COUNT",
	}

	envs := make(map[string]string)
	for _, key := range keys {
		envs[key] = env.Get(key)
	}

	envs["BUILDKITE_TEST_ENGINE_IDENTIFIER"] = c.Identifier

	return envs
}
