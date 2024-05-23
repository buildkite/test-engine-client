package config

import (
	"errors"
	"os"
	"strconv"
	"testing"
)

func TestGetIntEnvWithDefault(t *testing.T) {
	os.Setenv("MY_KEY", "10")
	defer os.Unsetenv("MY_KEY")

	os.Setenv("EMPTY_KEY", "")
	defer os.Unsetenv("EMPTY_KEY")

	os.Setenv("INVALID_KEY", "invalid_value")
	defer os.Unsetenv("INVALID_KEY")

	tests := []struct {
		key          string
		defaultValue int
		want         int
		err          error
	}{
		{
			key:          "MY_KEY",
			defaultValue: 20,
			want:         10,
			err:          nil,
		},
		{
			key:          "NON_EXISTENT_KEY",
			defaultValue: 30,
			want:         30,
			err:          nil,
		},
		{
			key:          "EMPTY_KEY",
			defaultValue: 40,
			want:         40,
			err:          nil,
		},
		{
			key:          "INVALID_KEY",
			defaultValue: 50,
			want:         50,
			err:          strconv.ErrSyntax,
		},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got, err := getIntEnvWithDefault(tt.key, tt.defaultValue)
			if err != nil && !errors.Is(err, tt.err) {
				t.Errorf("getIntEnvWithDefault(%q, %d) error = %v, want %v", tt.key, tt.defaultValue, err, tt.err)
			}
			if got != tt.want {
				t.Errorf("getIntEnvWithDefault(%q, %d) = %d, want %d", tt.key, tt.defaultValue, got, tt.want)
			}
		})
	}
}

func TestGetEnvWithDefault(t *testing.T) {
	os.Setenv("MY_KEY", "my_value")
	defer os.Unsetenv("MY_KEY")

	os.Setenv("EMPTY_KEY", "")
	defer os.Unsetenv("EMPTY_KEY")

	os.Setenv("OTHER_KEY", "other_value")
	defer os.Unsetenv("OTHER_KEY")

	tests := []struct {
		key          string
		defaultValue string
		want         string
	}{
		{
			key:          "MY_KEY",
			defaultValue: "default_value",
			want:         "my_value",
		},
		{
			key:          "NON_EXISTENT_KEY",
			defaultValue: "non_existent_default_value",
			want:         "non_existent_default_value",
		},
		{
			key:          "EMPTY_KEY",
			defaultValue: "empty_default_value",
			want:         "empty_default_value",
		},
		{
			key:          "EMPTY_KEY",
			defaultValue: os.Getenv("OTHER_KEY"),
			want:         "other_value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if got := getEnvWithDefault(tt.key, tt.defaultValue); got != tt.want {
				t.Errorf("getEnvWithDefault(%q, %q) = %q, want %q", tt.key, tt.defaultValue, got, tt.want)
			}
		})
	}
}
