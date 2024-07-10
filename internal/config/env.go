package config

import (
	"os"
	"strconv"
)

// getEnvWithDefault retrieves the value of the environment variable named by the key.
// If the variable is present and not empty, the value is returned.
// Otherwise the returned value will be the default value.
func getEnvWithDefault(key string, defaultValue string) string {
	value, ok := os.LookupEnv(key)
	if !ok {
		return defaultValue
	}
	if value == "" {
		return defaultValue
	}
	return value
}

func getIntEnvWithDefault(key string, defaultValue int) (int, error) {
	value := os.Getenv(key)
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
	keys := []string{
		"BUILDKITE_BUILD_ID",
		"BUILDKITE_JOB_ID",
		"BUILDKITE_ORGANIZATION_SLUG",
		"BUILDKITE_PARALLEL_JOB_COUNT",
		"BUILDKITE_PARALLEL_JOB",
		"BUILDKITE_SPLITTER_DEBUG_ENABLED",
		"BUILDKITE_SPLITTER_RETRY_COUNT",
		"BUILDKITE_SPLITTER_RETRY_CMD",
		"BUILDKITE_SPLITTER_SLOW_FILE_THRESHOLD",
		"BUILDKITE_SPLITTER_SPLIT_BY_EXAMPLE",
		"BUILDKITE_SPLITTER_SUITE_SLUG",
		"BUILDKITE_SPLITTER_TEST_CMD",
		"BUILDKITE_SPLITTER_TEST_FILE_EXCLUDE_PATTERN",
		"BUILDKITE_SPLITTER_TEST_FILE_PATTERN",
		"BUILDKITE_STEP_ID",
	}

	envs := make(map[string]string)
	for _, key := range keys {
		envs[key] = os.Getenv(key)
	}

	envs["BUILDKITE_SPLITTER_IDENTIFIER"] = c.Identifier

	return envs
}
