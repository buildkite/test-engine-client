package config

import (
	"os"
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
