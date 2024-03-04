package config

// validate checks if the Config struct is valid and returns InvalidConfigError if it's invalid.
func (c *Config) validate() error {
	validator := validator{
		errs: &InvalidConfigError{},
	}

	validator.validateStringRequired("SuiteToken", c.SuiteToken)
	validator.validateStringMaxLen("SuiteToken", c.SuiteToken, 1024)

	validator.validateStringRequired("Identifier", c.Identifier)
	validator.validateStringMaxLen("Identifier", c.Identifier, 1024)

	validator.validateMin("Parallelism", c.Parallelism, 1)
	validator.validateMax("Parallelism", c.Parallelism, 1000)

	validator.validateMin("NodeIndex", c.NodeIndex, 0)
	validator.validateMax("NodeIndex", c.NodeIndex, c.Parallelism-1)

	validator.validateStringIn("Mode", c.Mode, []string{"static"})

	validator.validateStringUrl("ServerBaseUrl", c.ServerBaseUrl)

	if len(*validator.errs) > 0 {
		return *validator.errs
	}

	return nil
}
