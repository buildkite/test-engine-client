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

var opts = ValidationOpts{}

func TestConfigValidate(t *testing.T) {
	c := createConfig()
	if err := c.Validate(opts); err != nil {
		t.Errorf("config.validate() error = %v", err)
	}
}

func TestConfigValidate_Empty(t *testing.T) {
	c := Config{errs: InvalidConfigError{}}
	err := c.Validate(opts)

	if !errors.As(err, new(InvalidConfigError)) {
		t.Errorf("config.validate() error = %v, want InvalidConfigError", err)
	}
}

func TestConfigValidate_SetsDefaults(t *testing.T) {
	c := createConfig()

	c.ServerBaseUrl = ""

	err := c.Validate(opts)
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
		// Node index < 0
		{
			name:  "BUILDKITE_PARALLEL_JOB",
			value: -1,
		},
		// Node index > parallelism
		{
			name:  "BUILDKITE_PARALLEL_JOB",
			value: 15,
		},
		// Parallelism > 1000
		{
			name:  "BUILDKITE_PARALLEL_JOB_COUNT",
			value: 1341,
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

			err := c.Validate(opts)

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

	t.Run("Parallelism is less than 1", func(t *testing.T) {
		c := createConfig()
		c.Parallelism = 0
		err := c.Validate(opts)

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
	})

	t.Run("opts.SkipParallelism is true", func(t *testing.T) {
		c := createConfig()

		// Normally these 2 field values would fail validation. When
		// SkipParallelism is true we want them to pass.
		c.Parallelism = 0
		c.NodeIndex = 0
		err := c.Validate(ValidationOpts{SkipParallelism: true})
		if err != nil {
			t.Errorf("config.validate() err = %v, want nil", err)
		}
	})

	t.Run("MaxRetries is less than 0", func(t *testing.T) {
		c := createConfig()
		c.MaxRetries = -1
		err := c.Validate(opts)

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
		err := c.Validate(opts)

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

	if err := c.Validate(opts); err != nil {
		t.Errorf("config.validate() error = %v", err)
	}
}

func TestConfigValidate_ResultPathOptionalWithCypress(t *testing.T) {
	c := createConfig()
	c.ResultPath = ""
	c.TestRunner = "cypress"

	err := c.Validate(opts)
	if err != nil {
		t.Errorf("config.validate() error = %v", err)
	}
}

func TestConfigValidate_ResultPathOptionalWithPytest(t *testing.T) {
	c := createConfig()
	c.ResultPath = ""
	c.TestRunner = "pytest"

	err := c.Validate(opts)
	if err != nil {
		t.Errorf("config.validate() error = %v", err)
	}
}

func TestTargetTimeParsing(t *testing.T) {
	c := createConfig()
	c.TargetTime, _ = time.ParseDuration("1.5s")
	c.MaxParallelism = 10

	err := c.Validate(opts)
	if err != nil {
		t.Errorf("config.validate() error = %v", err)
	}
	expected := 1500 * time.Millisecond

	if c.TargetTime != expected {
		t.Errorf("c.TargetTime = %v, want %v", c.TargetTime, expected)
	}
}

func TestTargetTimeInvalid(t *testing.T) {
	c := createConfig()
	c.TargetTime, _ = time.ParseDuration("-5s")
	c.MaxParallelism = 10
	err := c.Validate(opts)
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
	err := c.Validate(opts)
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
	err := c.Validate(opts)
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
	err := c.Validate(opts)
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
