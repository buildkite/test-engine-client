package config

import (
	"strings"
	"testing"
)

func createConfig() Config {
	return Config{
		ServerBaseUrl: "http://example.com",
		Mode:          "static",
		Parallelism:   10,
		NodeIndex:     5,
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

	t.Run("ServerBaseUrl is not a valid url", func(t *testing.T) {
		c := createConfig()
		c.ServerBaseUrl = "foo"
		if err := c.validate(); err == nil {
			t.Errorf("config.Validate expected error, got nil")
		}
	})

	t.Run("Mode is not static", func(t *testing.T) {
		c := createConfig()
		c.Mode = "dynamic"
		if err := c.validate(); err == nil {
			t.Errorf("config.Validate expected error, got nil")
		}
	})

	t.Run("SuiteToken is missing", func(t *testing.T) {
		c := createConfig()
		c.SuiteToken = ""
		if err := c.validate(); err == nil {
			t.Errorf("config.Validate expected error, got nil")
		}
	})

	t.Run("SuiteToken is greater than 1024", func(t *testing.T) {
		c := createConfig()
		c.SuiteToken = strings.Repeat("a", 1025)
		if err := c.validate(); err == nil {
			t.Errorf("config.Validate expected error, got nil")
		}
	})

	t.Run("Identifier is missing", func(t *testing.T) {
		c := createConfig()
		c.Identifier = ""
		if err := c.validate(); err == nil {
			t.Errorf("config.Validate expected error, got nil")
		}
	})

	t.Run("Identifier is greater 1024 characters", func(t *testing.T) {
		c := createConfig()
		c.Identifier = strings.Repeat("a", 1025)
		if err := c.validate(); err == nil {
			t.Errorf("config.Validate expected error, got nil")
		}
	})

	t.Run("NodeIndex is less than 0", func(t *testing.T) {
		c := createConfig()
		c.NodeIndex = -1
		if err := c.validate(); err == nil {
			t.Errorf("config.Validate expected error, got nil")
		}
	})

	t.Run("NodeIndex is greater than Parallelism", func(t *testing.T) {
		c := createConfig()
		c.NodeIndex = 15
		c.Parallelism = 10
		if err := c.validate(); err == nil {
			t.Errorf("config.Validate expected error, got nil")
		}
	})

	t.Run("Parallelism is less than 1", func(t *testing.T) {
		c := createConfig()
		c.Parallelism = 0
		if err := c.validate(); err == nil {
			t.Errorf("config.Validate expected error, got nil")
		}
	})
}
