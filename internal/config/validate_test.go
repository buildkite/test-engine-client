package config

import (
	"errors"
	"strings"
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
		{
			name:  "ServerBaseUrl is not a valid url",
			field: "ServerBaseUrl",
			value: "foo",
		},
		{
			name:  "Identifier is missing",
			field: "Identifier",
			value: "",
		},
		{
			name:  "Identifier is greater 1024 characters",
			field: "Identifier",
			value: strings.Repeat("a", 1025),
		},
		{
			name:  "NodeIndex is less than 0",
			field: "NodeIndex",
			value: -1,
		},
		{
			name:  "NodeIndex is greater than Parallelism",
			field: "NodeIndex",
			value: 15,
		},
		{
			name:  "Parallelism is greater than 1000",
			field: "Parallelism",
			value: 1341,
		},
		{
			name:  "OrganizationSlug is missing",
			field: "OrganizationSlug",
			value: "",
		},
		{
			name:  "SuiteSlug is missing",
			field: "SuiteSlug",
			value: "",
		},
		{
			name:  "AccessToken is missing",
			field: "AccessToken",
			value: "",
		},
	}

	for _, s := range scenario {
		t.Run(s.name, func(t *testing.T) {
			c := createConfig()
			switch s.field {
			case "ServerBaseUrl":
				c.ServerBaseUrl = s.value.(string)
			case "Identifier":
				c.Identifier = s.value.(string)
			case "NodeIndex":
				c.NodeIndex = s.value.(int)
			case "Parallelism":
				c.Parallelism = s.value.(int)
			case "OrganizationSlug":
				c.OrganizationSlug = s.value.(string)
			case "SuiteSlug":
				c.SuiteSlug = s.value.(string)
			case "AccessToken":
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

			if invConfigError[0].name != s.field {
				t.Errorf("config.validate() error name = %s, want %s", invConfigError[0].name, s.field)
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
