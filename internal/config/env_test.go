package config

import (
	"errors"
	"strconv"
	"testing"
)

func TestGetIntEnvWithDefault(t *testing.T) {
	env := map[string]string{
		"MY_KEY":      "10",
		"EMPTY_KEY":   "",
		"INVALID_KEY": "invalid_value",
	}

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
			got, err := getIntEnvWithDefault(env, tt.key, tt.defaultValue)
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
	env := map[string]string{
		"MY_KEY":    "my_value",
		"EMPTY_KEY": "",
		"OTHER_KEY": "other_value",
	}

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
			defaultValue: env["OTHER_KEY"],
			want:         "other_value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if got := getEnvWithDefault(env, tt.key, tt.defaultValue); got != tt.want {
				t.Errorf("getEnvWithDefault(%q, %q) = %q, want %q", tt.key, tt.defaultValue, got, tt.want)
			}
		})
	}
}
