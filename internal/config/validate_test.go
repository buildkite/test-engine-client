package config

import (
	"errors"
	"testing"
	"time"
)

func createConfig() Config {
	return Config{
		ServerBaseUrl:    "http://example.com",
		Parallelism:      10,
		NodeIndex:        0,
		Identifier:       "my_identifier",
		OrganizationSlug: "my_org",
		SuiteSlug:        "my_suite",
		AccessToken:      "my_token",
		MaxRetries:       3,
		ResultPath:       "tmp/result-*.json",
		errs:             InvalidConfigError{},
		TestRunner:       "rspec",
	}
}

func TestConfigValidate(t *testing.T) {
	c := createConfig()
	if err := c.validate(); err != nil {
		t.Errorf("config.validate() error = %v", err)
	}
}

func TestConfigValidate_Empty(t *testing.T) {
	c := Config{errs: InvalidConfigError{}}
	err := c.validate()

	if !errors.As(err, new(InvalidConfigError)) {
		t.Errorf("config.validate() error = %v, want InvalidConfigError", err)
	}
}

func TestConfigValidate_SetsDefaults(t *testing.T) {
	c := createConfig()

	c.ServerBaseUrl = ""

	err := c.validate()
	if err != nil {
		t.Errorf("config.validate() error = %v", err)
	}

	want := "https://api.buildkite.com"
	got := c.ServerBaseUrl

	if want != got {
		t.Errorf("c.Validate() -> c.ServerBaseUrl want %q got %q", want, got)
	}
}

func TestConfigValidate_Invalid(t *testing.T) {
	scenario := []struct {
		name  string
		field string
		value any
	}{
		// Base URL is bunk
		{
			name:  "BUILDKITE_TEST_ENGINE_BASE_URL",
			value: "foo",
		},
		// Organization slug is missing
		{
			name:  "BUILDKITE_ORGANIZATION_SLUG",
			value: "",
		},
		// Suite slug is missing
		{
			name:  "BUILDKITE_TEST_ENGINE_SUITE_SLUG",
			value: "",
		},
		// API access token is blank
		{
			name:  "BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN",
			value: "",
		},
		// Test runner is blank
		{
			name:  "BUILDKITE_TEST_ENGINE_TEST_RUNNER",
			value: "",
		},
	}

	for _, s := range scenario {
		t.Run(s.name, func(t *testing.T) {
			c := createConfig()
			switch s.name {
			case "BUILDKITE_TEST_ENGINE_BASE_URL":
				c.ServerBaseUrl = s.value.(string)
			case "BUILDKITE_PARALLEL_JOB":
				c.NodeIndex = s.value.(int)
			case "BUILDKITE_PARALLEL_JOB_COUNT":
				c.Parallelism = s.value.(int)
			case "BUILDKITE_ORGANIZATION_SLUG":
				c.OrganizationSlug = s.value.(string)
			case "BUILDKITE_TEST_ENGINE_SUITE_SLUG":
				c.SuiteSlug = s.value.(string)
			case "BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN":
				c.AccessToken = s.value.(string)
			case "BUILDKITE_TEST_ENGINE_TEST_RUNNER":
				c.TestRunner = s.value.(string)
			}

			err := c.validate()

			var invConfigError InvalidConfigError
			if !errors.As(err, &invConfigError) {
				t.Errorf("config.validate() error = %v, want InvalidConfigError", err)
			}

			if len(invConfigError) != 1 {
				t.Errorf("config.validate() error length = %d, want 1", len(invConfigError))
			}

			if len(invConfigError[s.name]) != 1 {
				t.Errorf("config.validate() error for %s length = %d, want 1", s.name, len(invConfigError[s.name]))
			}
		})
	}

	t.Run("MaxRetries is less than 0", func(t *testing.T) {
		c := createConfig()
		c.MaxRetries = -1
		err := c.validate()

		var invConfigError InvalidConfigError
		if !errors.As(err, &invConfigError) {
			t.Errorf("config.validate() error = %v, want InvalidConfigError", err)
			return
		}

		if len(invConfigError) != 1 {
			t.Errorf("config.validate() error length = %d, want 1", len(invConfigError))
		}
	})

	t.Run("BuildId, StepId and Identifier are empty", func(t *testing.T) {
		c := createConfig()
		c.BuildId = ""
		c.StepId = ""
		c.Identifier = ""
		err := c.validate()

		var invConfigError InvalidConfigError
		if !errors.As(err, &invConfigError) {
			t.Errorf("config.validate() error = %v, want InvalidConfigError", err)
			return
		}

		for _, field := range []string{"BUILDKITE_BUILD_ID", "BUILDKITE_STEP_ID"} {
			if len(invConfigError[field]) != 1 {
				t.Errorf("config.validate() error for %s length = %d, want 1", field, len(invConfigError[field]))
			}
		}

		if len(invConfigError) != 2 {
			t.Errorf("config.validate() error length = %d, want 2", len(invConfigError))
		}
	})
}

func TestConfigValidate_IdentifierPresentBuildIdStepIdMissing(t *testing.T) {
	c := createConfig()
	c.BuildId = ""
	c.StepId = ""

	if err := c.validate(); err != nil {
		t.Errorf("config.validate() error = %v", err)
	}
}

func TestConfigValidate_ResultPathOptionalWithCypress(t *testing.T) {
	c := createConfig()
	c.ResultPath = ""
	c.TestRunner = "cypress"

	err := c.validate()
	if err != nil {
		t.Errorf("config.validate() error = %v", err)
	}
}

func TestConfigValidate_ResultPathOptionalWithPytest(t *testing.T) {
	c := createConfig()
	c.ResultPath = ""
	c.TestRunner = "pytest"

	err := c.validate()
	if err != nil {
		t.Errorf("config.validate() error = %v", err)
	}
}

// Validation specific to `bktec run`
func TestConfigValidateForRun_NodeIndexLessThanZero(t *testing.T) {
	c := createConfig()
	c.NodeIndex = -1

	err := c.ValidateForRun()

	var invConfigError InvalidConfigError
	if !errors.As(err, &invConfigError) {
		t.Errorf("config.validate() error = %v, want InvalidConfigError", err)
		return
	}
}

func TestConfigValidateForRun_NodeIndexGreaterThanParallelism(t *testing.T) {
	c := createConfig()
	c.Parallelism = 1
	c.NodeIndex = 2

	err := c.ValidateForRun()

	var invConfigError InvalidConfigError
	if !errors.As(err, &invConfigError) {
		t.Errorf("config.validate() error = %v, want InvalidConfigError", err)
		return
	}
}

func TestConfigValidateForRun_ParallelismGreaterThanOneThousand(t *testing.T) {
	c := createConfig()
	c.Parallelism = 1001

	err := c.ValidateForRun()

	var invConfigError InvalidConfigError
	if !errors.As(err, &invConfigError) {
		t.Errorf("config.validate() error = %v, want InvalidConfigError", err)
		return
	}
}

func TestConfigValidateForRun_ParallelismIsLessThanOne(t *testing.T) {
	c := createConfig()
	c.Parallelism = 0
	err := c.ValidateForRun()

	var invConfigError InvalidConfigError
	if !errors.As(err, &invConfigError) {
		t.Errorf("config.validate() error = %v, want InvalidConfigError", err)
		return
	}

	// When parallelism less than 1, node index will always be invalid because it cannot be greater than parallelism and less than 0.
	// So, we expect 2 validation errors.
	if len(invConfigError) != 2 {
		t.Errorf("config.validate() error length = %d, want 2", len(invConfigError))
	}
}

// Validation specific to `bktec plan`
func TestTargetTimeInvalid(t *testing.T) {
	c := createConfig()
	c.TargetTime, _ = time.ParseDuration("-5s")
	c.MaxParallelism = 10
	err := c.ValidateForPlan()
	if err == nil {
		t.Errorf("config.validate() error = nil, want InvalidConfigError")
	}

	var invConfigError InvalidConfigError
	if !errors.As(err, &invConfigError) {
		t.Errorf("config.validate() error = %v, want InvalidConfigError", err)
	}

	if invConfigError["target-time"][0].Error() != "was -5s, must be greater than 0" {
		t.Errorf("config.validate() error for target-time = %v, want 'was -5s, must be greater than 0'", invConfigError["target-time"][0])
	}
}

func TestTargetTimeExceedsMax(t *testing.T) {
	c := createConfig()
	c.TargetTime, _ = time.ParseDuration("24h1s")
	c.MaxParallelism = 10
	err := c.ValidateForPlan()
	if err == nil {
		t.Errorf("config.validate() error = nil, want InvalidConfigError")
	}

	var invConfigError InvalidConfigError
	if !errors.As(err, &invConfigError) {
		t.Errorf("config.validate() error = %v, want InvalidConfigError", err)
	}

	if invConfigError["target-time"][0].Error() != "was 24h0m1s, must be less than or equal to 24 hours" {
		t.Errorf("config.validate() error for target-time = %v, want 'was 24h0m1s, must be less than or equal to 24 hours'", invConfigError["target-time"][0])
	}
}

func TestTargeTimeWithZeroParallelism(t *testing.T) {
	c := createConfig()
	c.TargetTime, _ = time.ParseDuration("5m")
	err := c.ValidateForPlan()
	if err == nil {
		t.Errorf("config.validate() error = nil, want InvalidConfigError")
	}

	var invConfigError InvalidConfigError
	if !errors.As(err, &invConfigError) {
		t.Errorf("config.validate() error = %v, want InvalidConfigError", err)
	}

	if invConfigError["max-parallelism"][0].Error() != "must be set when target-time is set" {
		t.Errorf("config.validate() error for max-parallelism = %v, want 'must be set when target-time is set'", invConfigError["max-parallelism"][0])
	}
}

func TestMaxParallelismOutOfRange(t *testing.T) {
	c := createConfig()
	c.MaxParallelism = 1500
	err := c.ValidateForPlan()
	if err == nil {
		t.Errorf("config.validate() error = nil, want InvalidConfigError")
	}

	var invConfigError InvalidConfigError
	if !errors.As(err, &invConfigError) {
		t.Errorf("config.validate() error = %v, want InvalidConfigError", err)
	}

	if invConfigError["max-parallelism"][0].Error() != "was 1500, must be between 0 and 1000" {
		t.Errorf("config.validate() error for max-parallelism = %v, want 'was 1500, must be between 0 and 1000'", invConfigError["max-parallelism"][0])
	}
}

func ValidateForPlan_SkipsParallelismAndNodeIndexValidation(t *testing.T) {
	c := createConfig()

	// These 2 fields are only required on ValidateForRun
	c.Parallelism = 0
	c.NodeIndex = 0
	err := c.ValidateForPlan()
	if err != nil {
		t.Errorf("config.validate() err = %v, want nil", err)
	}
}

func TestConfigValidate_SelectionParamsRequireStrategy(t *testing.T) {
	t.Run("strategy required when params provided", func(t *testing.T) {
		c := createConfig()
		c.SelectionParams = map[string]string{"top": "100"}

		err := c.validate()

		var invConfigError InvalidConfigError
		if !errors.As(err, &invConfigError) {
			t.Errorf("config.validate() error = %v, want InvalidConfigError", err)
			return
		}

		if len(invConfigError["selection-param"]) != 1 {
			t.Errorf("config.validate() error for selection-param length = %d, want 1", len(invConfigError["selection-param"]))
		}
	})

	t.Run("strategy without params is valid", func(t *testing.T) {
		c := createConfig()
		c.SelectionStrategy = "least-reliable"

		err := c.validate()
		if err != nil {
			t.Errorf("config.validate() error = %v, want nil", err)
		}
	})
}

func TestConfigValidate_TagFiltersOnlyWorksWithPytest(t *testing.T) {
	t.Run("TagFilters with pytest runner should be valid", func(t *testing.T) {
		c := createConfig()
		c.TestRunner = "pytest"
		c.TagFilters = "speed=slow"

		err := c.validate()
		if err != nil {
			t.Errorf("config.validate() error = %v, want nil", err)
		}
	})

	t.Run("TagFilters with non-pytest runner should fail", func(t *testing.T) {
		c := createConfig()
		c.TestRunner = "rspec"
		c.TagFilters = "speed=slow"

		err := c.validate()

		var invConfigError InvalidConfigError
		if !errors.As(err, &invConfigError) {
			t.Errorf("config.validate() error = %v, want InvalidConfigError", err)
			return
		}

		if len(invConfigError["BUILDKITE_TEST_ENGINE_TAG_FILTERS"]) != 1 {
			t.Errorf("config.validate() error for BUILDKITE_TEST_ENGINE_TAG_FILTERS length = %d, want 1", len(invConfigError["BUILDKITE_TEST_ENGINE_TAG_FILTERS"]))
		}

		expectedMsg := "tag filtering is only supported for the pytest test runner"
		if invConfigError["BUILDKITE_TEST_ENGINE_TAG_FILTERS"][0].Error() != expectedMsg {
			t.Errorf("config.validate() error message = %q, want %q", invConfigError["BUILDKITE_TEST_ENGINE_TAG_FILTERS"][0].Error(), expectedMsg)
		}
	})
}
