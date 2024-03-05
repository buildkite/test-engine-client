package config

import (
	"net/url"
)

// validate checks if the Config struct is valid and returns InvalidConfigError if it's invalid.
func (c *Config) validate() error {
	var errs InvalidConfigError

	if c.SuiteToken == "" {
		errs.appendFieldError("SuiteToken", "can not be blank")
	}

	if len(c.SuiteToken) > 1024 {
		errs.appendFieldError("SuiteToken", "can not be longer than %d characters", 1024)
	}

	if c.Identifier == "" {
		errs.appendFieldError("Identifier", "can not be blank")
	}

	if len(c.Identifier) > 1024 {
		errs.appendFieldError("Identifier", "can not be longer than %d characters", 1024)
	}

	if c.Parallelism < 1 {
		errs.appendFieldError("Parallelism", "must be greater than or equal to %d", 1)
	}

	if c.Parallelism > 1000 {
		errs.appendFieldError("Parallelism", "can not be greater than %d", 1000)
	}

	if c.NodeIndex < 0 {
		errs.appendFieldError("NodeIndex", "must be greater than or equal to %d", 0)
	}

	if c.NodeIndex > c.Parallelism-1 {
		errs.appendFieldError("NodeIndex", "can not be greater than %d", c.Parallelism-1)
	}

	if c.Mode != "static" {
		errs.appendFieldError("Mode", "%s is not a valid %s. Valid values are %v", c.Mode, "Mode", []string{"static"})
	}

	if c.ServerBaseUrl != "" {
		if _, err := url.ParseRequestURI(c.ServerBaseUrl); err != nil {
			errs.appendFieldError("ServerBaseUrl", "must be a valid URL")
		}
	}

	if len(errs) > 0 {
		return errs
	}

	return nil
}
