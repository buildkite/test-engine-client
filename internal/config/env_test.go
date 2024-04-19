package config

import (
	"os"
	"testing"
)

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
