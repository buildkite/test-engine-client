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
