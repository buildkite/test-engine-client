package config

import (
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestNewConfig(t *testing.T) {
	t.Run("config is valid", func(t *testing.T) {
		os.Setenv("BUILDKITE_PARALLEL_JOB_COUNT", "60")
		os.Setenv("BUILDKITE_PARALLEL_JOB", "7")
		os.Setenv("BUILDKITE_SPLITTER_BASE_URL", "https://build.kite")
		os.Setenv("BUILDKITE_SPLITTER_MODE", "static")
		os.Setenv("BUILDKITE_BUILD_ID", "xyz")
		os.Setenv("BUILDKITE_SUITE_TOKEN", "my_token")
		defer os.Clearenv()

		c, err := New()
		if err != nil {
			t.Errorf("config.New() expected no error, got error %v", err)
		}

		want := Config{
			Parallelism:   60,
			NodeIndex:     7,
			ServerBaseUrl: "https://build.kite",
			Mode:          "static",
			Identifier:    "xyz",
			SuiteToken:    "my_token",
		}

		if diff := cmp.Diff(c, want); diff != "" {
			t.Errorf("config.New() diff (-got +want):\n%s", diff)
		}
	})

	t.Run("config is empty", func(t *testing.T) {
		os.Clearenv()
		_, err := New()
		if err == nil {
			t.Errorf("config.New() expected error, got nil")
		}
		validationErrors := err.(InvalidConfigError)
		if len(validationErrors) != 6 {
			t.Errorf("config.New() expected 6 validation errors, got %d", len(validationErrors))
		}
	})
}
