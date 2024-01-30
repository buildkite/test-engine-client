package main

import (
	"fmt"
	"os"
	"strconv"
)

func FetchEnv(key string, defaultValue string) string {
	value, ok := os.LookupEnv(key)
	if !ok {
		return defaultValue
	}
	return value
}

func FetchIntEnv(key string, defaultValue int) (int, error) {
	value, ok := os.LookupEnv(key)
	if !ok {
		return defaultValue, nil
	}
	intValue, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue, fmt.Errorf("parsing env var %s value %q: %w", key, value, err)
	}
	return intValue, nil
}
