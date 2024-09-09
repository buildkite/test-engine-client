package config

import (
	"errors"
	"testing"
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
	}
}

func TestConfigValidate(t *testing.T) {
	c := createConfig()
	if err := c.validate(); err != nil {
		t.Errorf("config.validate() error = %v", err)
	}
}

func TestConfigValidate_Empty(t *testing.T) {
	c := Config{}
	err := c.validate()

	if !errors.As(err, new(InvalidConfigError)) {
		t.Errorf("config.validate() error = %v, want InvalidConfigError", err)
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
			name:  "BUILDKITE_SPLITTER_BASE_URL",
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
			name:  "BUILDKITE_SPLITTER_SUITE_SLUG",
			value: "",
		},
		// API access token is blank
		{
			name:  "BUILDKITE_SPLITTER_API_ACCESS_TOKEN",
			value: "",
		},
	}

	for _, s := range scenario {
		t.Run(s.name, func(t *testing.T) {
			c := createConfig()
			switch s.name {
			case "BUILDKITE_SPLITTER_BASE_URL":
				c.ServerBaseUrl = s.value.(string)
			case "BUILDKITE_PARALLEL_JOB":
				c.NodeIndex = s.value.(int)
			case "BUILDKITE_PARALLEL_JOB_COUNT":
				c.Parallelism = s.value.(int)
			case "BUILDKITE_ORGANIZATION_SLUG":
				c.OrganizationSlug = s.value.(string)
			case "BUILDKITE_SPLITTER_SUITE_SLUG":
				c.SuiteSlug = s.value.(string)
			case "BUILDKITE_SPLITTER_API_ACCESS_TOKEN":
				c.AccessToken = s.value.(string)
			}

			err := c.validate()

			var invConfigError InvalidConfigError
			if !errors.As(err, &invConfigError) {
				t.Errorf("config.validate() error = %v, want InvalidConfigError", err)
			}

			if len(invConfigError) != 1 {
				t.Errorf("config.validate() error length = %d, want 1", len(invConfigError))
			}

			if invConfigError[0].name != s.name {
				t.Errorf("config.validate() error name = %s, want %s", invConfigError[0].name, s.name)
			}
		})
	}

	t.Run("Parallelism is less than 1", func(t *testing.T) {
		c := createConfig()
		c.Parallelism = 0
		err := c.validate()

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
}
