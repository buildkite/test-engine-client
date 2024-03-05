package config

// Config is the internal representation of the complete test splitter client configuration.
type Config struct {
	// Parallelism is the number of parallel tasks to run.
	Parallelism int
	// ServerBaseUrl is the base URL of the test splitter server.
	ServerBaseUrl string
	// SuiteToken is the token of the test suite.
	SuiteToken string
	// Mode is the mode of the test splitter.
	Mode string
	// Identifier is the identifier of the build.
	Identifier string
	// Node index is index of the current node.
	NodeIndex int
}

// New wraps the readFromEnv and validate functions to create a new Config struct.
// It returns Config struct and an InvalidConfigError if there is an invalid configuration.
func New() (Config, error) {
	var errs InvalidConfigError

	c := &Config{}

	err := c.readFromEnv()
	if err != nil {
		errs = append(errs, err.(InvalidConfigError)...)
	}

	err = c.validate()
	if err != nil {
		errs = append(errs, err.(InvalidConfigError)...)
	}

	if len(errs) > 0 {
		return Config{}, errs
	}

	return *c, nil
}
