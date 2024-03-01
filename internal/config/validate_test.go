package config

import (
	"errors"
	"strings"
	"testing"
)

func createConfig() Config {
	return Config{
		ServerBaseUrl: "http://example.com",
		Mode:          "static",
		Parallelism:   10,
		NodeIndex:     0,
		SuiteToken:    "my_suite_token",
		Identifier:    "my_identifier",
	}
}

func TestConfigValidate(t *testing.T) {
	t.Run("config is valid", func(t *testing.T) {
		c := createConfig()
		if err := c.validate(); err != nil {
			t.Errorf("config.Validate() expected no error, got error %v", err)
		}
	})

	t.Run("config is empty", func(t *testing.T) {
		c := Config{}
		err := c.validate()
		if err == nil {
			t.Errorf("config.Validate() expected error, got nil")
		}

		if !errors.As(err, new(InvalidConfigError)) {
			t.Errorf("config.Validate() expected InvalidConfigError, got %v", err)
		}
	})

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
			name:  "Mode is not static",
			field: "Mode",
			value: "dynamic",
		},
		{
			name:  "SuiteToken is missing",
			field: "SuiteToken",
			value: "",
		},
		{
			name:  "SuiteToken is greater than 1024",
			field: "SuiteToken",
			value: strings.Repeat("a", 1025),
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
			name:  "Parallelism is greater than 500",
			field: "Parallelism",
			value: 501,
		},
	}

	for _, s := range scenario {
		t.Run(s.name, func(t *testing.T) {
			c := createConfig()
			switch s.field {
			case "ServerBaseUrl":
				c.ServerBaseUrl = s.value.(string)
			case "Mode":
				c.Mode = s.value.(string)
			case "SuiteToken":
				c.SuiteToken = s.value.(string)
			case "Identifier":
				c.Identifier = s.value.(string)
			case "NodeIndex":
				c.NodeIndex = s.value.(int)
			case "Parallelism":
				c.Parallelism = s.value.(int)
			}

			err := c.validate()
			if err == nil {
				t.Errorf("config.Validate() expected error, got nil")
			}

			if !errors.As(err, new(InvalidConfigError)) {
				t.Errorf("config.Validate() expected InvalidConfigError, got %v", err)
			}

			validationErrors := err.(InvalidConfigError)
			if len(validationErrors) != 1 {
				t.Errorf("config.Validate() expected 1 validation error, got %d", len(validationErrors))
			}

			if validationErrors[0].name != s.field {
				t.Errorf("config.Validate() expected error name %v, got %v", s.field, validationErrors[0].name)
			}
		})
	}

	t.Run("Parallelism is less than 1", func(t *testing.T) {
		c := createConfig()
		c.Parallelism = 0
		err := c.validate()
		if err == nil {
			t.Errorf("config.Validate() expected error, got nil")
		}

		if !errors.As(err, new(InvalidConfigError)) {
			t.Errorf("config.Validate() expected ValidationError, got %v", err)
		}

		validationErrors := err.(InvalidConfigError)

		// When parallelism less than 1, node index will always be invalid because it cannot be greater than parallelism and less than 0.
		// So, we expect 2 validation errors.
		if len(validationErrors) != 2 {
			t.Errorf("config.readFromEnv() expected 2 validation error, got %d", len(validationErrors))
		}
	})
}
